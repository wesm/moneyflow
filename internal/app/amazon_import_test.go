package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestAmazonImportFirstInstallBuildsOrdinaryCommittedProfile(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 20, 18, 0, 0, 0, time.UTC)
	row := amazonIncomingRow("order-a", "ASIN-A", "fingerprint-a", "orders.csv", 2)
	request := AmazonImportRequest{
		Candidate: amazon.Candidate{
			Rows: []amazon.Row{row}, ObservedOrderIDs: []string{"order-a"},
			FileCount: 1, LogicalRecordCount: 1, Digest: amazonDigest("a"),
		},
		Settings: amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: now,
	}
	plan, err := BuildAmazonImportPlan(store.AmazonImportState{}, amazonProposedIDs(), request)
	require.NoError(t, err)
	require.NoError(t, plan.Committed.Validate())
	require.NotNil(t, plan.Settings)
	assert.Equal(t, domain.Currency("USD"), plan.Settings.Currency)
	assert.Len(t, plan.Committed.Transactions, 1)
	assert.Equal(t, "amazon", plan.Committed.Transactions[0].Provider)
	assert.Equal(t, int64(-1000), plan.Committed.Transactions[0].Amount.Minor)
	assert.Len(t, plan.Items, 1)
	assert.True(t, plan.SemanticChange)
}

func TestAmazonImportInstallsPristineSQLiteProfileAndReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	profile, err := sqlite.Open(ctx, home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	now := time.Date(2026, time.August, 20, 18, 30, 0, 0, time.UTC)
	row := amazonIncomingRow("order-a", "ASIN-A", amazonDigest("b"), "orders.csv", 2)
	row.FullFingerprint = amazonDigest("c")
	request := AmazonImportRequest{
		Candidate: amazon.Candidate{
			Rows: []amazon.Row{row}, ObservedOrderIDs: []string{"order-a"},
			FileCount: 1, LogicalRecordCount: 1, Digest: amazonDigest("d"),
		},
		Settings: amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: now,
	}
	first, err := ImportAmazonProfile(ctx, profile, request)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), first.Revision)
	assert.Equal(t, 1, first.Inserted)
	service, err := NewProfileService(ctx, profile)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), service.Revision())
	capabilities := make(map[ActionID]Capability)
	for _, capability := range service.Capabilities() {
		capabilities[capability.Action] = capability
	}
	assert.True(t, capabilities[ActionRefreshProvider].Available)
	assert.True(t, capabilities[ActionManageCategories].Available)
	assert.True(t, capabilities[ActionManageGroups].Available)
	second, err := service.ImportAmazon(ctx, request)
	require.NoError(t, err)
	assert.True(t, second.NoOp)
	assert.Equal(t, first.Revision, second.Revision)
}

func TestAmazonImportUnchangedCandidateIsSemanticNoOp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 20, 18, 0, 0, 0, time.UTC)
	row := amazonIncomingRow("order-a", "ASIN-A", "fingerprint-a", "orders.csv", 2)
	request := AmazonImportRequest{
		Candidate: amazon.Candidate{Rows: []amazon.Row{row}, ObservedOrderIDs: []string{"order-a"}, Digest: amazonDigest("a")},
		Settings:  amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: now,
	}
	first, err := BuildAmazonImportPlan(store.AmazonImportState{}, amazonProposedIDs(), request)
	require.NoError(t, err)
	state := store.AmazonImportState{
		Snapshot: domain.ProfileSnapshot{Revision: 1, Committed: first.Committed, Journal: first.Journal, Cursor: first.Cursor, KnownDrills: first.KnownDrills},
		Settings: first.Settings, Items: first.Items, Allocations: first.Allocations,
	}
	second, err := BuildAmazonImportPlan(state, amazonProposedIDs(), request)
	require.NoError(t, err)
	assert.False(t, second.SemanticChange)
	assert.Equal(t, first.Committed, second.Committed)
	assert.Equal(t, 1, second.History.UnchangedCount)
}

func TestAmazonImportObservedShrinkRewritesJournalTargets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 20, 19, 0, 0, 0, time.UTC)
	firstRequest := AmazonImportRequest{
		Candidate: amazon.Candidate{
			Rows: []amazon.Row{
				amazonIncomingRow("order-a", "ASIN-A", amazonDigest("a"), "orders.csv", 2),
				amazonIncomingRow("order-a", "ASIN-B", amazonDigest("b"), "orders.csv", 3),
			},
			ObservedOrderIDs: []string{"order-a"}, Digest: amazonDigest("c"),
		},
		Settings: amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: now,
	}
	first, err := BuildAmazonImportPlan(store.AmazonImportState{}, amazonProposedIDs(), firstRequest)
	require.NoError(t, err)
	targets := []domain.EntityID{first.Items[0].LocalTransactionID, first.Items[1].LocalTransactionID}
	slices.Sort(targets)
	operation := domain.Operation{
		ID: "operation-amazon-hide", Sequence: 1, Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: 1, CreatedAt: now, Targets: targets,
		HideToggle: &domain.HideTogglePayload{},
	}
	state := store.AmazonImportState{
		Snapshot: domain.ProfileSnapshot{
			Revision: 1, Committed: first.Committed, Journal: []domain.Operation{operation}, Cursor: 1,
		},
		Settings: first.Settings, Items: first.Items, Allocations: first.Allocations,
	}
	secondRequest := firstRequest
	secondRequest.Candidate.Rows = secondRequest.Candidate.Rows[:1]
	second, err := BuildAmazonImportPlan(state, amazonProposedIDs(), secondRequest)
	require.NoError(t, err)
	require.Len(t, second.Journal, 1)
	assert.Len(t, second.Journal[0].Targets, 1)
	assert.Equal(t, 1, second.Cursor)
}

func amazonProposedIDs() store.ProposedAmazonIDs {
	return store.ProposedAmazonIDs{
		TransactionIDs:   []domain.EntityID{"transaction_new_a", "transaction_new_b"},
		AccountIDs:       []domain.EntityID{"account_new_a", "account_new_b"},
		MerchantIDs:      []domain.EntityID{"merchant_new_a", "merchant_new_b"},
		SourceIdentities: []string{"amazon_item_aaaaaaaaaaaaaaaaaaaaaaaaaa", "amazon_item_bbbbbbbbbbbbbbbbbbbbbbbbbb"},
		GroupIDs:         []domain.EntityID{"group_new_a"}, CategoryIDs: []domain.EntityID{"category_new_a"},
	}
}

func amazonDigest(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
