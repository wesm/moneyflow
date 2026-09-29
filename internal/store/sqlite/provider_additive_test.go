package sqlite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestSimpleFINAttemptRequiresCurrentOwner(t *testing.T) {
	profile, err := Open(t.Context(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	before, err := profile.ProviderState(t.Context())
	require.NoError(t, err)
	require.Error(t, profile.RecordRefreshAttempt(t.Context(), "missing", now, now.Add(time.Hour)))
	_, acquired, err := profile.AcquireRefreshLease(t.Context(), store.RefreshLease{OwnerID: "owner", Renderer: "tui", ExpiresAt: now.Add(time.Minute)}, now)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Error(t, profile.RecordRefreshAttempt(t.Context(), "other", now, now.Add(time.Hour)))
	require.Error(t, profile.RecordRefreshAttempt(t.Context(), "owner", now.Add(time.Minute), now.Add(time.Hour)))
	require.NoError(t, profile.RecordRefreshAttempt(t.Context(), "owner", now, now.Add(time.Hour)))
	after, err := profile.ProviderState(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Refresh.Generation, after.Refresh.Generation)
	require.Equal(t, before.Refresh.LastSuccess, after.Refresh.LastSuccess)
	require.Equal(t, before.Refresh.StatusCode, after.Refresh.StatusCode)
	require.Equal(t, now, after.Refresh.LastAttempt)
	require.Equal(t, now.Add(time.Hour), after.Refresh.NextEligible)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Zero(t, loaded.Revision)
}

func TestSimpleFINStorageRejectsChangedLocalTruth(t *testing.T) {
	for _, mutation := range []string{"none", "old transaction", "dimension", "identity", "new ID", "journal", "count", "replay"} {
		t.Run(mutation, func(t *testing.T) {
			profile, err := Open(t.Context(), temporaryPaths(t), DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, profile.Close()) })
			now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
			candidate := providerRefreshCandidate(t, now)
			candidate.Groups, candidate.Categories = nil, nil
			for i := range candidate.Transactions {
				candidate.Transactions[i].CategoryExternalID = ""
			}
			proposals := make(map[string]domain.EntityID)
			for _, entity := range append(append([]domain.ImportEntity{}, candidate.Accounts...), candidate.Merchants...) {
				proposals[app.ProviderIdentityKey("simplefin", entity.Kind, entity.ExternalID)] = domain.EntityID("id-" + entity.ExternalID)
			}
			for _, row := range candidate.Transactions {
				proposals[app.ProviderIdentityKey("simplefin", domain.EntityKindTransaction, row.ExternalID)] = domain.EntityID("id-" + row.ExternalID)
			}
			request := store.AtomicRefreshRequest{LeaseOwnerID: "owner", Binding: &store.ProviderBinding{Kind: "simplefin", Namespace: "simplefin", RemoteProfileID: "credential-digest", Currency: "USD", Scale: 2, BoundAt: now}, Candidate: candidate, ObservedAt: now, ProposedIDs: proposals}
			_, acquired, err := profile.AcquireRefreshLease(t.Context(), store.RefreshLease{OwnerID: "owner", Renderer: "tui", ExpiresAt: now.Add(time.Minute)}, now)
			require.NoError(t, err)
			require.True(t, acquired)
			_, err = profile.ApplyProviderRefresh(t.Context(), request, app.BuildProviderRefreshPlanReference)
			require.NoError(t, err)
			request.ExpectedGeneration = 1
			next := candidate.Transactions[0]
			next.ExternalID = "new-transaction"
			request.Candidate.Transactions = append(request.Candidate.Transactions, next)
			proposals[app.ProviderIdentityKey("simplefin", domain.EntityKindTransaction, next.ExternalID)] = "new-id"
			_, acquired, err = profile.AcquireRefreshLease(t.Context(), store.RefreshLease{OwnerID: "owner", Renderer: "tui", ExpiresAt: now.Add(time.Minute)}, now)
			require.NoError(t, err)
			require.True(t, acquired)
			before, err := profile.Load(t.Context())
			require.NoError(t, err)
			state, err := profile.ProviderState(t.Context())
			require.NoError(t, err)
			_, err = profile.ApplyProviderRefresh(t.Context(), request, func(input store.RefreshInputs) (store.RefreshPlan, error) {
				plan, planErr := app.BuildProviderRefreshPlanReference(input)
				if planErr != nil {
					return plan, planErr
				}
				switch mutation {
				case "old transaction":
					plan.Committed.Transactions[0].Notes = "Lost local edit"
				case "dimension":
					plan.Committed.Accounts[0].Label = "Changed"
				case "identity":
					plan.Committed.ExternalIdentities = plan.Committed.ExternalIdentities[1:]
				case "new ID":
					delete(input.ProposedIDs, app.ProviderIdentityKey("simplefin", domain.EntityKindTransaction, next.ExternalID))
					plan.Committed.Transactions[len(plan.Committed.Transactions)-1].ID = "unproposed"
				case "journal":
					plan.Cursor = 1
				case "count":
					plan.Summary.ImportedTransactions++
				case "replay":
					plan.Effective.Transactions[0].Notes = "Incorrect replay"
				}
				if mutation != "replay" && mutation != "journal" {
					replayed, replayErr := app.Replay(domain.ProfileSnapshot{Committed: plan.Committed, Journal: plan.Journal, Cursor: plan.Cursor, KnownDrills: plan.KnownDrills})
					if replayErr == nil {
						plan.Effective = replayed.Effective
					}
				}
				return plan, nil
			})
			if mutation == "none" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			after, loadErr := profile.Load(t.Context())
			require.NoError(t, loadErr)
			require.Equal(t, before, after)
			afterState, loadErr := profile.ProviderState(t.Context())
			require.NoError(t, loadErr)
			require.Equal(t, state, afterState)
		})
	}
}
