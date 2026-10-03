package app_test

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProviderRecoveryReclaimsRetiredMerchantForExactSubset(t *testing.T) {
	for _, providerKind := range []string{"monarch", "ynab"} {
		t.Run(providerKind, func(t *testing.T) {
			for _, removeIdentity := range []bool{false, true} {
				name := "retired identity mapped"
				if removeIdentity {
					name = "former identity unmapped"
				}
				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					paths, err := home.ResolveRoot(t.TempDir(), nil, "")
					require.NoError(t, err)
					profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
					require.NoError(t, err)
					originalProfile := profile
					t.Cleanup(func() { require.NoError(t, originalProfile.Close()) })
					service, err := app.NewProfileService(ctx, profile)
					require.NoError(t, err)
					now := providerWriteTime()
					snapshot := providerSnapshot(t, now, 5)
					snapshot.Merchants = append(snapshot.Merchants,
						domain.ImportEntity{Kind: domain.EntityKindMerchant, ExternalID: "merchant-destination", Label: "Destination Merchant"},
						domain.ImportEntity{Kind: domain.EntityKindMerchant, ExternalID: "merchant-third", Label: "Third Merchant"})
					snapshot.Transactions[3].MerchantExternalID = "merchant-destination"
					snapshot.Transactions[4].MerchantExternalID = "merchant-third"
					for index := range 2 {
						snapshot.Transactions[index].Date, err = domain.ParseDate("2025-12-15")
						require.NoError(t, err)
					}
					reader := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: providerKind, RemoteID: "synthetic-plan"}, snapshot: snapshot, fingerprint: "synthetic-session"}
					recovery := false
					writer := &scriptedProviderWriter{identity: reader.identity, update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
						if !recovery {
							if providerKind == "ynab" {
								assert.Equal(t, provider.Some("merchant-destination"), update.MerchantExternalID)
							} else {
								assert.Equal(t, provider.Some("Destination Merchant"), update.MerchantName)
								assert.False(t, update.MerchantExternalID.Present)
							}
							return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID,
								MerchantExternalID: provider.Some("merchant-destination"), MerchantLabel: provider.Some("Destination Merchant")}, nil
						}
						assert.Contains(t, []string{transactionExternalID(0), transactionExternalID(1)}, update.TransactionExternalID)
						assert.False(t, update.CategoryExternalID.Present)
						assert.False(t, update.ClearCategory)
						assert.False(t, update.Hidden.Present)
						if providerKind == "monarch" {
							assert.Equal(t, provider.Some("Example Merchant"), update.MerchantName)
							assert.False(t, update.MerchantExternalID.Present)
						} else if update.MerchantName.Present {
							assert.Equal(t, "Example Merchant", update.MerchantName.Value)
						} else {
							assert.Equal(t, provider.Some("merchant-example"), update.MerchantExternalID)
						}
						return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID,
							MerchantExternalID: provider.Some("merchant-example"), MerchantLabel: provider.Some("Example Merchant")}, nil
					}}
					source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
					runtime := app.ProviderRuntime{ReadSource: source, WriteSource: source,
						Provider: providerKind, Currency: "USD", Scale: 2, Renderer: "mcp", InstanceID: "synthetic-recovery",
						Now: func() time.Time { return now }}
					require.NoError(t, service.ConfigureProvider(runtime))
					_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
					require.NoError(t, err)
					loaded, err := profile.Load(ctx)
					require.NoError(t, err)
					sourceID := recoveryEntityID(t, providerKind, loaded.Committed, domain.EntityKindMerchant, "merchant-example")
					destinationID := recoveryEntityID(t, providerKind, loaded.Committed, domain.EntityKindMerchant, "merchant-destination")
					thirdID := recoveryEntityID(t, providerKind, loaded.Committed, domain.EntityKindMerchant, "merchant-third")
					revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
						ID: "synthetic-original-merge", Type: domain.OperationMerchantMerge, PayloadVersion: 1,
						CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{sourceID},
						Merge: &domain.MergePayload{SourceID: sourceID, DestinationID: destinationID},
					})
					require.NoError(t, err)
					_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
					require.NoError(t, err)
					_, err = service.RunProviderWrite(ctx)
					require.NoError(t, err)
					beforeRecovery, err := profile.Load(ctx)
					require.NoError(t, err)
					assert.True(t, merchantByID(t, beforeRecovery.Committed, sourceID).Retired)
					require.Equal(t, 3, writer.callCount())

					if removeIdentity {
						transfer, transferErr := profile.LoadProfileTransfer(ctx)
						require.NoError(t, transferErr)
						identities := transfer.Snapshot.Committed.ExternalIdentities[:0]
						for _, identity := range transfer.Snapshot.Committed.ExternalIdentities {
							if identity.EntityType != domain.EntityKindMerchant || identity.EntityID != sourceID {
								identities = append(identities, identity)
							}
						}
						transfer.Snapshot.Committed.ExternalIdentities = identities
						importedPaths, resolveErr := home.ResolveRoot(t.TempDir(), nil, "")
						require.NoError(t, resolveErr)
						imported, openErr := sqlite.Open(ctx, importedPaths, sqlite.DefaultOptions)
						require.NoError(t, openErr)
						t.Cleanup(func() { require.NoError(t, imported.Close()) })
						require.NoError(t, imported.InstallProfileTransfer(ctx, transfer))
						require.NoError(t, profile.Close())
						profile, paths = imported, importedPaths
						service, err = app.NewProfileService(ctx, profile)
						require.NoError(t, err)
						require.NoError(t, service.ConfigureProvider(runtime))
						beforeRecovery, err = profile.Load(ctx)
						require.NoError(t, err)
					}

					// Undo the one unrelated pending merge before appending the recovery edit.
					revision, err = profile.Append(ctx, beforeRecovery.Revision, domain.Operation{
						ID: "synthetic-wrong-pending-merge", Type: domain.OperationMerchantMerge, PayloadVersion: 1,
						CreatedRevision: beforeRecovery.Revision, CreatedAt: now, Targets: []domain.EntityID{thirdID},
						Merge: &domain.MergePayload{SourceID: thirdID, DestinationID: destinationID},
					})
					require.NoError(t, err)
					_, err = service.Undo(ctx, revision)
					require.NoError(t, err)
					undone, err := profile.Load(ctx)
					require.NoError(t, err)
					require.Zero(t, undone.Cursor)
					require.Equal(t, beforeRecovery.Committed, undone.Committed)
					var targets []domain.EntityID
					for _, transaction := range beforeRecovery.Committed.Transactions {
						if transaction.Date.String() < "2026-01-01" {
							targets = append(targets, transaction.ID)
						}
					}
					require.Len(t, targets, 2)
					selection, err := app.NewExplicitTransactionSelection(targets[:1], undone.Revision)
					require.NoError(t, err)
					state := app.DefaultViewState()
					state.Current.Mode = domain.ResultModeDetail
					mutated, err := service.Mutate(ctx, app.MutationRequest{
						Action: app.ActionEditMerchant, ExpectedRevision: undone.Revision, OmitProjection: true,
						State: state, Selection: selection,
						Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant-restored-synthetic", Label: "Example Merchant"},
					})
					require.NoError(t, err, "recreate the retired label for the exact historical subset")
					selection, err = app.NewExplicitTransactionSelection(targets[1:], mutated.Revision)
					require.NoError(t, err)
					mutated, err = service.Mutate(ctx, app.MutationRequest{
						Action: app.ActionEditMerchant, ExpectedRevision: mutated.Revision, OmitProjection: true,
						State: state, Selection: selection,
						Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant-restored-synthetic"},
					})
					require.NoError(t, err, "stage the next exact chunk against the same recreated merchant")
					staged, err := profile.Load(ctx)
					require.NoError(t, err)
					require.Len(t, staged.Journal, 2)
					assert.Equal(t, domain.OperationMerchantReassign, staged.Journal[0].Type)
					require.NotNil(t, staged.Journal[0].Reassign.CreatedMerchant)
					require.ElementsMatch(t, targets[:1], staged.Journal[0].Targets)
					require.ElementsMatch(t, targets[1:], staged.Journal[1].Targets)
					assert.Nil(t, staged.Journal[1].Reassign.CreatedMerchant)
					assert.Equal(t, staged.Journal[0].Reassign.DestinationID, staged.Journal[1].Reassign.DestinationID)
					recovery = true
					prepared, err := service.Commit(ctx, app.CommitRequest{ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision})
					require.NoError(t, err)
					require.NotNil(t, prepared.ProviderWrite)
					batchID := prepared.ProviderWrite.BatchID
					final, err := service.RunProviderWrite(ctx)
					require.NoError(t, err)
					assert.Empty(t, final.Phase)
					assert.Equal(t, 5, writer.callCount())
					assert.Equal(t, 1, reader.fetchCalls(), "recovery must not fetch the provider snapshot")
					require.NoError(t, profile.Close())
					profile, err = sqlite.Open(ctx, paths, sqlite.DefaultOptions)
					require.NoError(t, err)
					reopenedProfile := profile
					t.Cleanup(func() { require.NoError(t, reopenedProfile.Close()) })
					after, err := profile.Load(ctx)
					require.NoError(t, err)
					require.Empty(t, after.Journal)
					expected := beforeRecovery.Committed.Clone()
					targetSet := make(map[domain.EntityID]bool)
					for _, id := range targets {
						targetSet[id] = true
					}
					for index := range expected.Transactions {
						if targetSet[expected.Transactions[index].ID] {
							expected.Transactions[index].MerchantID = "merchant-restored-synthetic"
						}
					}
					assert.Equal(t, expected.Transactions, after.Committed.Transactions, "only selected merchant assignments may change")
					assert.Equal(t, domain.EntityID("merchant-restored-synthetic"), recoveryEntityID(t, providerKind, after.Committed, domain.EntityKindMerchant, "merchant-example"))
					var auditedTargets []string
					acknowledged, finalized := 0, false
					for _, event := range readWriteAudit(t, filepath.Join(paths.Root, "audit.jsonl")) {
						if event["batch_id"] != batchID {
							continue
						}
						switch event["event"] {
						case "provider_planned":
							auditedTargets = append(auditedTargets, event["transaction_external_id"].(string))
							assert.Equal(t, "Destination Merchant", event["before"].(map[string]any)["merchant_name"])
							assert.Equal(t, "Example Merchant", event["requested"].(map[string]any)["merchant_name"])
						case "provider_acknowledged":
							acknowledged++
							assert.Equal(t, "merchant-example", event["acknowledged"].(map[string]any)["merchant_external_id"])
						case "provider_finalized":
							finalized = true
						}
					}
					sort.Strings(auditedTargets)
					assert.Equal(t, []string{transactionExternalID(0), transactionExternalID(1)}, auditedTargets)
					assert.Equal(t, 2, acknowledged)
					assert.True(t, finalized)
					writeState, err := profile.ProviderWriteState(ctx)
					require.NoError(t, err)
					assert.Nil(t, writeState.Batch)
					t.Logf("restored exactly %d synthetic pre-2026 transactions; preserved 3 other rows and all nonmerchant fields; 1 initial fetch, no recovery fetch; original retired provider ID reassigned; audit retained for batch %s", len(targets), batchID)
				})
			}
		})
	}
}

func recoveryEntityID(t *testing.T, providerKind string, profile domain.CommittedProfile, kind domain.EntityKind, externalID string) domain.EntityID {
	t.Helper()
	for _, identity := range profile.ExternalIdentities {
		if identity.EntityType == kind && identity.Namespace == providerKind+"/"+string(kind) && identity.ExternalID == externalID {
			return identity.EntityID
		}
	}
	t.Fatalf("synthetic provider identity %s missing", externalID)
	return ""
}
