package sqlite

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProfileTransferRoundTrip(t *testing.T) {
	for _, kind := range []string{"ynab", "amazon"} {
		t.Run(kind, func(t *testing.T) {
			state := transferTestState(t, kind)
			first, err := Open(t.Context(), temporaryPaths(t), DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, first.Close()) })
			require.NoError(t, first.InstallProfileTransfer(t.Context(), state))
			before, err := first.LoadProfileTransfer(t.Context())
			require.NoError(t, err)
			require.ElementsMatch(t, state.Snapshot.Committed.Accounts, before.Snapshot.Committed.Accounts)
			require.ElementsMatch(t, state.Snapshot.Committed.Merchants, before.Snapshot.Committed.Merchants)
			require.ElementsMatch(t, state.Snapshot.Committed.Groups, before.Snapshot.Committed.Groups)
			require.ElementsMatch(t, state.Snapshot.Committed.Categories, before.Snapshot.Committed.Categories)
			require.ElementsMatch(t, state.Snapshot.Committed.Transactions, before.Snapshot.Committed.Transactions)
			require.ElementsMatch(t, state.Snapshot.Committed.ExternalIdentities, before.Snapshot.Committed.ExternalIdentities)
			require.Equal(t, state.Snapshot.KnownDrills, before.Snapshot.KnownDrills)
			require.Equal(t, state.Provider.Binding, before.Provider.Binding)
			require.ElementsMatch(t, state.Provider.Allocations, before.Provider.Allocations)
			require.ElementsMatch(t, state.Provider.Lineage, before.Provider.Lineage)
			require.ElementsMatch(t, state.Provider.WriteRestrictions, before.Provider.WriteRestrictions)
			require.ElementsMatch(t, state.YNABSplits, before.YNABSplits)
			require.Equal(t, state.AmazonSettings, before.AmazonSettings)
			require.ElementsMatch(t, state.AmazonItems, before.AmazonItems)
			second, err := Open(t.Context(), temporaryPaths(t), DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, second.Close()) })
			require.NoError(t, second.InstallProfileTransfer(t.Context(), before))
			after, err := second.LoadProfileTransfer(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Empty(t, after.Snapshot.Journal)
			require.Nil(t, after.Provider.Lease)
			require.Nil(t, after.Provider.Write)
			require.Zero(t, after.Provider.Refresh)
			require.NoError(t, app.ValidateProfileTransfer(after, kind))
			_, err = app.NewProfileService(t.Context(), second)
			require.NoError(t, err)
			require.Error(t, second.InstallProfileTransfer(t.Context(), state))
		})
	}
}

func TestProfileTransferConstraintFailureRollsBack(t *testing.T) {
	state := transferTestState(t, "ynab")
	state.YNABSplits[0].ParentTransactionID = "transaction_missing"
	target, err := Open(t.Context(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, target.Close()) })
	require.Error(t, target.InstallProfileTransfer(t.Context(), state))
	loaded, err := target.LoadProfileTransfer(t.Context())
	require.NoError(t, err)
	require.Empty(t, loaded.Snapshot.Committed.Transactions)
	require.Zero(t, loaded.Snapshot.Revision)
	require.Nil(t, loaded.Provider.Binding)
}

func TestProfileTransferEligibility(t *testing.T) {
	state := transferTestState(t, "ynab")
	state.Snapshot.Journal = []domain.Operation{{ID: "excluded_redo"}}
	now := time.Now().UTC()
	clean, excluded, err := app.PrepareProfileTransfer(state, now)
	require.NoError(t, err)
	require.Equal(t, 1, excluded)
	require.Empty(t, clean.Snapshot.Journal)
	require.Len(t, state.Snapshot.Journal, 1)
	state.Snapshot.Cursor = 1
	_, _, err = app.PrepareProfileTransfer(state, now)
	require.ErrorContains(t, err, "edits")
	state.Snapshot.Cursor = 0
	state.Provider.Write = &store.WriteBatchStatus{}
	_, _, err = app.PrepareProfileTransfer(state, now)
	require.ErrorContains(t, err, "write")
	state.Provider.Write = nil
	state.Provider.Lease = &store.ProviderOperationLease{ExpiresAt: now.Add(time.Minute)}
	_, _, err = app.PrepareProfileTransfer(state, now)
	require.ErrorContains(t, err, "operation")
	state.Provider.Lease.ExpiresAt = now.Add(-time.Second)
	_, _, err = app.PrepareProfileTransfer(state, now)
	require.NoError(t, err)
}

func TestTransferSourceNeverInstallsOrChangesDatabase(t *testing.T) {
	for _, mode := range []string{"current", "older", "corrupt", "missing"} {
		t.Run(mode, func(t *testing.T) {
			paths := temporaryPaths(t)
			if mode != "missing" {
				opened, err := Open(t.Context(), paths, DefaultOptions)
				require.NoError(t, err)
				if mode == "older" {
					_, err = opened.(*profile).database.Exec("UPDATE schema_metadata SET schema_version = 1")
					require.NoError(t, err)
				}
				require.NoError(t, opened.Close())
				if mode == "corrupt" {
					require.NoError(t, os.WriteFile(paths.Database, []byte("broken"), 0600))
				}
			}
			before, _ := os.ReadFile(paths.Database) //nolint:gosec // Synthetic database in t.TempDir.
			opened, err := OpenTransferSource(t.Context(), paths, DefaultOptions)
			if mode == "current" {
				require.NoError(t, err)
				_, err = opened.LoadProfileTransfer(t.Context())
				require.NoError(t, err)
				require.NoError(t, opened.Close())
			} else {
				require.Error(t, err)
			}
			after, _ := os.ReadFile(paths.Database) //nolint:gosec // Synthetic database in t.TempDir.
			require.Equal(t, before, after)
			if mode == "missing" {
				require.NoFileExists(t, paths.Database)
			}
		})
	}
}

func transferTestState(t *testing.T, kind string) store.ProfileTransfer {
	t.Helper()
	if kind == "amazon" {
		amazon := validAmazonStoreState(t)
		amazon.Settings.CreatedAt = amazon.Settings.CreatedAt.Truncate(time.Millisecond)
		amazon.Items[0].LocalNotes = "Saved local overlay"
		amazon.Items[0].LocalHidden = true
		return store.ProfileTransfer{Snapshot: amazon.Snapshot, AmazonSettings: amazon.Settings, AmazonItems: amazon.Items}
	}
	committed := fixtureProfile(t)
	destination := committed.Merchants[0].ID
	committed.Merchants = append(committed.Merchants,
		domain.Merchant{ID: "merchant_retired", Label: "Retired", CollisionKey: "retired", Retired: true, MergeDestination: &destination},
		domain.Merchant{ID: "merchant_empty", Label: "Empty", CollisionKey: "empty"})
	committed.ExternalIdentities = append(committed.ExternalIdentities, domain.ExternalIdentity{
		EntityType: domain.EntityKindTransaction, EntityID: "transaction_deleted", Namespace: "ynab/transaction", ExternalID: "deleted"})
	for i := range committed.Transactions {
		committed.Transactions[i].Provider = "ynab"
		committed.Transactions[i].Amount.Currency = "USD"
		committed.Transactions[i].Amount.Scale = 2
	}
	known, err := seededKnownDrills(committed)
	require.NoError(t, err)
	known = append(known, domain.DrillIdentity{Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_empty"})
	slices.SortFunc(known, compareDrillIdentities)
	transaction := committed.Transactions[0]
	return store.ProfileTransfer{
		Snapshot: domain.ProfileSnapshot{Committed: committed, KnownDrills: known},
		Provider: store.ProviderState{
			Binding:           &store.ProviderBinding{Kind: "ynab", Namespace: "ynab", RemoteProfileID: "example-plan", Currency: "USD", Scale: 2, BoundAt: time.Unix(100, 0).UTC()},
			Allocations:       []store.LabelAllocation{{Kind: domain.EntityKindMerchant, Namespace: "ynab/merchant", ExternalID: "example", BaseCollisionKey: "example", DisplayLabel: "Example", ProviderLabel: "Example", Unsuffixed: true}},
			Lineage:           []store.ProviderIdentityLineage{{Kind: domain.EntityKindMerchant, Namespace: "ynab/merchant", ExternalID: "old", PriorLocalID: "merchant_retired", CurrentLocalID: destination, ProviderLabel: "Retired", Disposition: "alias", BatchVersion: 3}},
			WriteRestrictions: []store.ProviderWriteRestriction{{Kind: domain.EntityKindTransaction, EntityID: transaction.ID, Reason: "transfer"}},
		},
		YNABSplits: []store.YNABTransactionSplit{{ParentTransactionID: transaction.ID, ExternalID: "split-example", AmountMinor: transaction.Amount.Minor, AmountMilliunits: transaction.Amount.Minor * 10}},
	}
}
