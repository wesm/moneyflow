package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAuditFailurePreventsClaimAndKeepsAttemptUnchanged(t *testing.T) {
	t.Parallel()
	profile, prepared, now := preparedWriteProfile(t)
	path := profileAuditPath(t, profile)
	require.NoError(t, os.Rename(path, path+".saved"))
	require.NoError(t, os.Mkdir(path, 0o700))
	_, err := profile.ClaimProviderWriteItems(t.Context(), store.ClaimProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite, ObservedAt: now, Limit: 1,
	})
	require.Error(t, err)
	state, err := profile.ProviderWriteState(t.Context())
	require.NoError(t, err)
	require.Len(t, state.Items, 1)
	assert.Zero(t, state.Items[0].AttemptCount)
}

func TestAuditIncompleteTailPreventsFurtherClaim(t *testing.T) {
	t.Parallel()
	profile, prepared, now := preparedWriteProfile(t)
	path := profileAuditPath(t, profile)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) // #nosec G304 -- Corrupt only this test's temporary audit log.
	require.NoError(t, err)
	_, err = file.WriteString(`{"event":`)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	_, err = profile.ClaimProviderWriteItems(t.Context(), store.ClaimProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite, ObservedAt: now, Limit: 1,
	})
	require.Error(t, err)
	contents, err := os.ReadFile(path) // #nosec G304 -- Inspect this test's temporary audit log after the rejected append.
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(contents), `{"event":`))
}

func TestLocalCommitAuditRetainsBeforeAndRequestedAfterJournalDeletion(t *testing.T) {
	t.Parallel()
	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	target := before.Committed.Transactions[0]
	revision, err := profile.Append(ctx, before.Revision,
		draftHideOperation("operation-local-audit", before.Revision, target.ID))
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded.Journal)
	contents, err := os.ReadFile(profileAuditPath(t, profile))
	require.NoError(t, err)
	var events []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(string(contents)), "\n") {
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}
	require.Len(t, events, 2)
	assert.Equal(t, "local_commit_intent", events[0]["event"])
	assert.Equal(t, string(target.ID), events[0]["transaction_id"])
	assert.Equal(t, []any{"operation-local-audit"}, events[0]["operation_ids"])
	assert.Equal(t, target.Hidden, events[0]["before"].(map[string]any)["hidden"])
	assert.Equal(t, !target.Hidden, events[0]["requested"].(map[string]any)["hidden"])
	assert.Equal(t, "local_committed", events[1]["event"])
}

func profileAuditPath(t *testing.T, profile *profile) string {
	t.Helper()
	var sequence int
	var name, path string
	require.NoError(t, profile.database.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path))
	return filepath.Join(filepath.Dir(path), "audit.jsonl")
}

func TestLocalTaxonomyAuditRetainsPreviousLabel(t *testing.T) {
	t.Parallel()
	profile := openSeededProfile(t, DefaultOptions)
	before, err := profile.Load(t.Context())
	require.NoError(t, err)
	var group domain.CategoryGroup
	for _, candidate := range before.Committed.Groups {
		if !candidate.Protected && !candidate.Retired {
			group = candidate
			break
		}
	}
	require.NotEmpty(t, group.ID)
	revision, err := profile.Append(t.Context(), before.Revision, domain.Operation{
		ID: "operation-taxonomy-audit", Type: domain.OperationGroupLabel, PayloadVersion: 1,
		CreatedRevision: before.Revision, CreatedAt: foldOperationTime(), Targets: []domain.EntityID{group.ID},
		Label: &domain.LabelPayload{EntityID: group.ID, Label: "Renamed Group", CollisionKey: "renamed group"},
	})
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
	require.NoError(t, err)
	contents, err := os.ReadFile(profileAuditPath(t, profile))
	require.NoError(t, err)
	var intent map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.Split(string(contents), "\n")[0]), &intent))
	assert.Equal(t, string(group.ID), intent["entity_id"])
	require.Contains(t, intent, "before_entity")
	assert.Equal(t, group.Label, intent["before_entity"].(map[string]any)["label"])
	assert.Equal(t, "Renamed Group", intent["requested_entity"].(map[string]any)["label"])
}
