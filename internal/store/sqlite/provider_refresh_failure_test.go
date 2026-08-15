package sqlite

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderRefreshLateWriteFailuresRollBackEveryLogicalTable(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name  string
		event string
		table string
	}{
		{"committed delete", "DELETE", "transactions"},
		{"committed insert", "INSERT", "accounts"},
		{"journal rewrite", "DELETE", "journal_operations"},
		{"allocation rewrite", "DELETE", "provider_label_allocations"},
		{"external identity rewrite", "DELETE", "external_identities"},
		{"known drill extension", "INSERT", "known_drills"},
		{"profile revision", "UPDATE", "profile_state"},
		{"refresh generation", "UPDATE", "provider_refresh_state"},
		{"lease release", "DELETE", "provider_refresh_lease"},
	}
	for _, failure := range failures {
		failure := failure
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			profile := openSeededProfile(t, DefaultOptions)
			now := time.Date(2026, time.August, 15, 22, 0, 0, 0, time.UTC)
			bindProviderForRefreshTest(t, profile, now)
			acquireProviderRefreshLease(t, profile, "failure-owner", now)
			beforeAppend, err := profile.Load(ctx)
			require.NoError(t, err)
			_, err = profile.Append(ctx, beforeAppend.Revision, draftHideOperation(
				"operation_refresh_failure",
				beforeAppend.Revision,
				beforeAppend.Committed.Transactions[0].ID,
			))
			require.NoError(t, err)
			_, err = profile.database.ExecContext(ctx, `
				INSERT INTO provider_label_allocations(
					entity_type, namespace, external_id, base_collision_key,
					display_label, suffix_token, unsuffixed
				) VALUES (
					'merchant', 'monarch/merchant', 'merchant-example',
					'example merchant', 'Example Merchant', '', 1
				)`)
			require.NoError(t, err)
			_, err = profile.database.ExecContext(ctx, `
				INSERT INTO external_identities(entity_type, entity_id, namespace, external_id)
				VALUES ('transaction', ?, 'refresh-test/transaction', 'transaction-example')`,
				beforeAppend.Committed.Transactions[0].ID)
			require.NoError(t, err)
			before, err := profile.Load(ctx)
			require.NoError(t, err)
			providerBefore, err := profile.ProviderState(ctx)
			require.NoError(t, err)
			installRefreshFailureTrigger(t, profile, failure.event, failure.table)

			_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
				ExpectedGeneration: 0, LeaseOwnerID: "failure-owner",
				Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
			}, failureRefreshPlanner(failure.table))
			assertStoreCode(t, err, store.CodeStoreError)
			assertSafeStorageFailure(t, err)
			after, loadErr := profile.Load(ctx)
			require.NoError(t, loadErr)
			providerAfter, providerErr := profile.ProviderState(ctx)
			require.NoError(t, providerErr)
			assert.Equal(t, before, after)
			assert.Equal(t, providerBefore, providerAfter)
		})
	}
}

func TestProviderRefreshInitialBindingFailureRollsBackImportedRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	now := time.Date(2026, time.August, 15, 22, 15, 0, 0, time.UTC)
	acquireProviderRefreshLease(t, profile, "binding-failure-owner", now)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	providerBefore, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	installRefreshFailureTrigger(t, profile, "INSERT", "provider_binding")
	committed := fixtureProfile(t)
	knownDrills, err := seededKnownDrills(committed)
	require.NoError(t, err)
	binding := store.ProviderBinding{
		Kind: "monarch", Namespace: "monarch", RemoteProfileID: "subscription-example",
		BoundAt: now,
	}

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "binding-failure-owner", Binding: &binding,
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		return store.RefreshPlan{
			Committed: committed, Effective: committed.Clone(), KnownDrills: knownDrills,
			Summary: refreshCandidateSummary(inputs.Candidate),
		}, nil
	})
	assertStoreCode(t, err, store.CodeStoreError)
	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	providerAfter, providerErr := profile.ProviderState(ctx)
	require.NoError(t, providerErr)
	assert.Equal(t, before, after)
	assert.Equal(t, providerBefore, providerAfter)
}

func failureRefreshPlanner(table string) store.RefreshPlanner {
	return func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plan, err := passthroughRefreshPlanner(inputs)
		if err != nil {
			return store.RefreshPlan{}, err
		}
		switch table {
		case "transactions":
			plan.Committed.Transactions = plan.Committed.Transactions[:len(plan.Committed.Transactions)-1]
		case "accounts":
			plan.Committed.Accounts = append(plan.Committed.Accounts, domain.Account{
				ID: "account_refresh_new", Label: "New Account", CollisionKey: "new account",
			})
		case "journal_operations":
			plan.Journal = nil
			plan.Cursor = 0
		case "provider_label_allocations":
			plan.Allocations = nil
		case "external_identities":
			plan.Committed.ExternalIdentities = slices.DeleteFunc(
				plan.Committed.ExternalIdentities,
				func(identity domain.ExternalIdentity) bool {
					return identity.Namespace == "refresh-test/transaction"
				},
			)
		case "known_drills":
			plan.KnownDrills = append(plan.KnownDrills, domain.DrillIdentity{
				Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2,
				Key: "merchant_refresh_known",
			})
			slices.SortFunc(plan.KnownDrills, func(a, b domain.DrillIdentity) int {
				left, _ := a.CanonicalKey()
				right, _ := b.CanonicalKey()
				return strings.Compare(left, right)
			})
		}
		if table == "transactions" || table == "accounts" || table == "journal_operations" ||
			table == "external_identities" {
			replayed, replayErr := profilereplay.Replay(domain.ProfileSnapshot{
				Committed: plan.Committed, Journal: plan.Journal,
				Cursor: plan.Cursor, KnownDrills: plan.KnownDrills,
			})
			if replayErr != nil {
				return store.RefreshPlan{}, replayErr
			}
			plan.Effective = replayed.Effective
		}
		return plan, nil
	}
}

func TestProviderRefreshStaleGenerationPreservesLeaseAndState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 22, 30, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "first-owner", now)
	_, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "first-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, passthroughRefreshPlanner)
	require.NoError(t, err)
	secondNow := now.Add(time.Minute)
	acquireProviderRefreshLease(t, profile, "stale-owner", secondNow)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	providerBefore, err := profile.ProviderState(ctx)
	require.NoError(t, err)

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "stale-owner",
		Candidate: providerRefreshCandidate(t, secondNow), ObservedAt: secondNow,
	}, passthroughRefreshPlanner)
	assertStoreCode(t, err, store.CodeRevisionConflict)
	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	providerAfter, providerErr := profile.ProviderState(ctx)
	require.NoError(t, providerErr)
	assert.Equal(t, before, after)
	assert.Equal(t, providerBefore, providerAfter)
}

func installRefreshFailureTrigger(
	t *testing.T,
	profile *profile,
	event, table string,
) {
	t.Helper()
	allowed := map[string]map[string]bool{
		"DELETE": {
			"transactions": true, "journal_operations": true,
			"provider_label_allocations": true, "provider_refresh_lease": true,
			"external_identities": true,
		},
		"INSERT": {"accounts": true, "known_drills": true, "provider_binding": true},
		"UPDATE": {"profile_state": true, "provider_refresh_state": true},
	}
	require.True(t, allowed[event][table])
	// The event and table are checked against fixed test values above.
	//nolint:gosec
	query := fmt.Sprintf(`
		CREATE TRIGGER fail_provider_refresh BEFORE %s ON %s
		BEGIN
			SELECT RAISE(ABORT, 'synthetic provider refresh failure');
		END`, event, table)
	_, err := profile.database.ExecContext(context.Background(), query)
	require.NoError(t, err)
}
