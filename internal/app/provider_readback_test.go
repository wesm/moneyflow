package app_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestExplicitResumeReadsUncertainTransactionBeforeRetry(t *testing.T) {
	for _, observed := range []string{"before", "requested", "other", "read failure", "released reservation", "changed before lease"} {
		t.Run(observed, func(t *testing.T) {
			ctx := context.Background()
			paths, err := home.ResolveRoot(t.TempDir(), nil, "")
			require.NoError(t, err)
			profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, profile.Close()) })
			guarded := &readbackLeaseProfile{Profile: profile}
			service, err := app.NewProfileService(ctx, guarded)
			require.NoError(t, err)
			now := providerWriteTime()
			reader := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"},
				snapshot: providerSnapshot(t, now, 2), fingerprint: "synthetic-session"}
			uncertainAttempts, readCalls := 0, 0
			writer := &scriptedProviderWriter{identity: reader.identity,
				update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
					if update.TransactionExternalID == transactionExternalID(1) {
						uncertainAttempts++
						if uncertainAttempts == 1 {
							return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
						}
					}
					return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID,
						MerchantExternalID: provider.Some("merchant-created"), MerchantLabel: provider.Some("Restored Merchant")}, nil
				},
				readback: func(_ context.Context, id string, date domain.Date) (provider.TransactionUpdateResult, error) {
					readCalls++
					assert.Equal(t, transactionExternalID(1), id)
					assert.Equal(t, "2026-08-15", date.String())
					if observed == "read failure" {
						return provider.TransactionUpdateResult{}, provider.NewError(provider.CodeUnavailable)
					}
					idValue, label := "merchant-example", "Example Merchant"
					switch observed {
					case "requested":
						idValue, label = "merchant-created", "Restored Merchant"
					case "other":
						idValue, label = "merchant-unrelated", "Unrelated Merchant"
					}
					return provider.TransactionUpdateResult{TransactionExternalID: id,
						MerchantExternalID: provider.Some(idValue), MerchantLabel: provider.Some(label),
						CategoryExternalID: provider.Some("category-example"), Hidden: provider.Some(false)}, nil
				},
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, WriteSource: source,
				Provider: "monarch", Currency: "USD", Scale: 2, Renderer: "mcp", InstanceID: "readback-test",
				Now: func() time.Time { return now }}))
			_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			merchantID := loaded.Committed.Transactions[0].MerchantID
			revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
				ID: "readback-rename", Type: domain.OperationMerchantLabel, PayloadVersion: 1,
				CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{merchantID},
				Label: &domain.LabelPayload{EntityID: merchantID, Label: "Restored Merchant", CollisionKey: "restored merchant"},
			})
			require.NoError(t, err)
			_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
			require.NoError(t, err)
			parked, err := service.RunProviderWrite(ctx)
			require.Error(t, err)
			require.Equal(t, 1, parked.Completed)
			require.Equal(t, store.WriteAttentionOutcomeUnknown, parked.AttentionReason)
			assert.True(t, parked.CanCheckOutcome)
			current, statusErr := service.ProviderWriteStatus(ctx)
			require.NoError(t, statusErr)
			assert.True(t, current.CanCheckOutcome)
			require.Equal(t, 2, writer.callCount())
			_, err = service.RunProviderWrite(ctx)
			require.Error(t, err, "automatic work must not read or retry the unknown item")
			assert.Zero(t, readCalls)
			if observed == "changed before lease" {
				guarded.beforeAcquire = func(ctx context.Context) error {
					batch, resumeErr := profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
						BatchID: parked.BatchID, ExpectedVersion: parked.Version, ObservedAt: now,
						Lease: store.ProviderOperationLease{OwnerID: "other-instance", Renderer: "mcp",
							Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(time.Minute)},
					})
					if resumeErr != nil {
						return resumeErr
					}
					_, parkErr := profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
						BatchID: batch.ID, ExpectedVersion: batch.Version, LeaseOwnerID: "other-instance",
						LeaseKind: store.ProviderOperationWrite, Phase: store.WritePhaseAttentionRequired,
						AttentionClass: store.WriteAttentionReconcileOnly, AttentionReason: store.WriteAttentionOutcomeUnknown,
						ObservedAt: now,
					})
					return parkErr
				}
			}

			status, execution, err := service.ReserveProviderWriteExecution(ctx, parked.Version)
			if observed == "changed before lease" {
				require.Error(t, err)
				assert.Nil(t, execution)
				assert.Zero(t, readCalls, "a stale version must not authorize a provider read")
				assert.Equal(t, 2, writer.callCount())
				return
			}
			if observed == "other" || observed == "read failure" {
				require.Error(t, err)
				assert.Nil(t, execution)
				assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
				assert.Equal(t, 2, writer.callCount())
				assert.Equal(t, 1, readCalls)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, execution)
			assert.Equal(t, 2, writer.callCount(), "reservation only reads provider state")
			if observed == "released reservation" {
				execution.Release()
				parked, next, reserveErr := service.ReserveProviderWriteExecution(ctx, status.Version)
				require.Error(t, reserveErr)
				assert.Nil(t, next)
				assert.Equal(t, 1, readCalls, "an unconsumed reservation cannot silently grant retry")
				status, execution, err = service.ReserveProviderWriteExecution(ctx, parked.Version)
				require.NoError(t, err)
				require.NotNil(t, execution)
				assert.Equal(t, 2, readCalls)
			}
			_, err = execution.Run(ctx)
			require.NoError(t, err)
			wantCalls := 3
			if observed == "requested" {
				wantCalls = 2
			}
			assert.Equal(t, wantCalls, writer.callCount())
			assert.Equal(t, 1, reader.fetchCalls(), "resume must not fetch a snapshot")
			var sawReadEvidence bool
			for _, event := range readWriteAudit(t, filepath.Join(paths.Root, "audit.jsonl")) {
				if observed == "requested" && event["event"] == "provider_read_confirmed" {
					sawReadEvidence = true
				}
				if observed != "requested" && event["event"] == "provider_read_retry_authorized" {
					sawReadEvidence = true
				}
			}
			assert.True(t, sawReadEvidence)
			persisted, err := profile.Load(ctx)
			require.NoError(t, err)
			assert.Empty(t, persisted.Journal)
			assert.Equal(t, "Restored Merchant", merchantLabelByID(t, persisted.Committed, merchantID))
		})
	}
}

type readbackLeaseProfile struct {
	store.Profile
	beforeAcquire func(context.Context) error
}

func (profile *readbackLeaseProfile) AcquireProviderOperationLease(ctx context.Context, lease store.ProviderOperationLease, now time.Time) (store.ProviderOperationLease, bool, error) {
	if before := profile.beforeAcquire; before != nil {
		profile.beforeAcquire = nil
		if err := before(ctx); err != nil {
			return store.ProviderOperationLease{}, false, err
		}
	}
	return profile.Profile.AcquireProviderOperationLease(ctx, lease, now)
}
