package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store"
)

func TestLocalCommitCompletionAuditFailureKeepsSavedProjection(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"audit unavailable", "canceled request", "intent unavailable"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			profile := openSeededProfile(t, DefaultOptions)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			before, err := profile.Load(ctx)
			require.NoError(t, err)
			target := before.Committed.Transactions[0]
			revision, err := profile.Append(ctx, before.Revision,
				draftHideOperation("operation-completion-audit", before.Revision, target.ID))
			require.NoError(t, err)
			service, err := app.NewProfileService(ctx, profile)
			require.NoError(t, err)
			auditPath := profileAuditPath(t, profile)
			if mode == "intent unavailable" {
				require.NoError(t, os.Mkdir(auditPath, 0o700))
			}
			calls := 0
			profile.now = func() time.Time {
				calls++
				if calls == 2 {
					// The completion timestamp is requested after SQLite commits,
					// before the independent completion audit append.
					durable, loadErr := profile.Load(context.Background())
					require.NoError(t, loadErr)
					require.Equal(t, revision+1, durable.Revision)
					switch mode {
					case "audit unavailable":
						require.NoError(t, os.Rename(auditPath, auditPath+".saved"))
						require.NoError(t, os.Mkdir(auditPath, 0o700))
					case "canceled request":
						cancel()
					}
				}
				return time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
			}
			result, commitErr := service.Commit(ctx, app.CommitRequest{
				ExpectedRevision: revision, ReviewedRevision: revision,
			})
			durable, err := profile.Load(t.Context())
			require.NoError(t, err)
			if mode == "intent unavailable" {
				require.Error(t, commitErr)
				assert.Equal(t, revision, durable.Revision)
				assert.Len(t, durable.Journal, 1)
				assert.Equal(t, target.Hidden, transactionRecord(t, durable.Committed, target.ID).Hidden)
				assert.Equal(t, 1, service.Pending().ActiveOperations)
				return
			}
			require.NoError(t, commitErr)
			assert.Equal(t, revision+1, result.Revision)
			assert.Equal(t, revision+1, durable.Revision)
			assert.Empty(t, durable.Journal)
			assert.Equal(t, !target.Hidden, transactionRecord(t, durable.Committed, target.ID).Hidden)
			assert.Equal(t, revision+1, service.Revision())
			assert.Zero(t, result.Pending.ActiveOperations)
			assert.NotEmpty(t, result.Projection.AuditWarning)
			projection, err := service.ProjectView(app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{})
			require.NoError(t, err)
			assert.Equal(t, revision+1, projection.Revision)
			assert.Zero(t, projection.Pending.ActiveOperations)
			assert.NotEmpty(t, projection.AuditWarning)

			if mode == "audit unavailable" {
				require.NoError(t, os.Remove(auditPath))
				require.NoError(t, os.Rename(auditPath+".saved", auditPath))
			}
			profile.now = time.Now
			revision, err = profile.Append(t.Context(), durable.Revision,
				draftHideOperation("operation-after-audit-recovery", durable.Revision, target.ID))
			require.NoError(t, err)
			result, err = service.Commit(t.Context(), app.CommitRequest{
				ExpectedRevision: revision, ReviewedRevision: revision,
			})
			require.NoError(t, err)
			assert.Empty(t, result.Projection.AuditWarning)
		})
	}
}

func TestFoldCompletionAuditFailureReturnsCommittedRevision(t *testing.T) {
	t.Parallel()
	profile := openSeededProfile(t, DefaultOptions)
	before, err := profile.Load(t.Context())
	require.NoError(t, err)
	target := before.Committed.Transactions[0]
	revision, err := profile.Append(t.Context(), before.Revision,
		draftHideOperation("operation-fold-completion", before.Revision, target.ID))
	require.NoError(t, err)
	pending, err := profile.Load(t.Context())
	require.NoError(t, err)
	replayed, err := app.Replay(pending)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(replayed, revision)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	profile.now = func() time.Time {
		calls++
		if calls == 2 {
			cancel()
		}
		return time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	}
	next, err := profile.Fold(ctx, revision, plan)
	require.Error(t, err)
	var completion *store.AuditCompletionError
	require.ErrorAs(t, err, &completion)
	assert.ErrorIs(t, completion, context.Canceled)
	assert.Equal(t, revision+1, next)
	durable, loadErr := profile.Load(t.Context())
	require.NoError(t, loadErr)
	assert.Equal(t, revision+1, durable.Revision)
	assert.Empty(t, durable.Journal)
	assert.Equal(t, !target.Hidden, transactionRecord(t, durable.Committed, target.ID).Hidden)
}
