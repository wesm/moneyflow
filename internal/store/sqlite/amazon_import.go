package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

var amazonDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ApplyAmazonImport computes and installs one Amazon import under the authoritative write lock.
func (profile *profile) ApplyAmazonImport(
	ctx context.Context,
	request store.AtomicAmazonImportRequest,
	planner store.AmazonImportPlanner,
) (store.AmazonImportCommit, error) {
	if err := validateAmazonImportRequest(request, planner); err != nil {
		return store.AmazonImportCommit{}, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.AmazonImportCommit{}, err
	}
	defer func() { _ = finish(false) }()

	state, err := loadAmazonState(ctx, connection)
	if err != nil {
		return store.AmazonImportCommit{}, err
	}
	proposed, err := proposeAmazonIDs(request.ProposedCounts)
	if err != nil {
		return store.AmazonImportCommit{}, store.NewError(store.CodeStoreError, err)
	}
	plan, err := planner(cloneAmazonImportState(state), proposed)
	if err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = validateAmazonImportPlan(state, plan); err != nil {
		return store.AmazonImportCommit{}, store.NewError(store.CodeInvalidOperation, err)
	}

	previousRevision := state.Snapshot.Revision
	resultingRevision := previousRevision
	if plan.SemanticChange {
		resultingRevision, err = incrementRevision(previousRevision)
		if err != nil {
			return store.AmazonImportCommit{}, err
		}
	}
	if err = applyProviderCommitted(
		ctx, connection, state.Snapshot.Committed, plan.Committed,
		state.Snapshot.KnownDrills, plan.KnownDrills,
	); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = replaceRefreshJournal(ctx, connection, state.Snapshot.Journal, plan.Journal); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = replaceLabelAllocations(ctx, connection, state.Allocations, plan.Allocations); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = replaceAmazonSettings(ctx, connection, state.Settings, plan.Settings); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = replaceAmazonItems(ctx, connection, state.Items, plan.Items); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = updateJournalState(ctx, connection, previousRevision, resultingRevision, plan.Cursor); err != nil {
		return store.AmazonImportCommit{}, err
	}
	history := plan.History
	history.ImportID = request.ImportID
	history.StartedAt = request.StartedAt.UTC()
	history.CompletedAt = request.ImportedAt.UTC()
	history.SourceRevision = previousRevision
	history.ResultingRevision = resultingRevision
	history.CandidateDigest = request.CandidateDigest
	if err = insertAmazonHistory(ctx, connection, history); err != nil {
		return store.AmazonImportCommit{}, err
	}
	if err = finish(true); err != nil {
		return store.AmazonImportCommit{}, err
	}
	return store.AmazonImportCommit{
		PreviousRevision: previousRevision, Revision: resultingRevision,
		SemanticChange: plan.SemanticChange, History: history,
	}, nil
}

func validateAmazonImportRequest(request store.AtomicAmazonImportRequest, planner store.AmazonImportPlanner) error {
	if planner == nil {
		return errors.New("amazon import planner is required")
	}
	if request.ImportID == "" || !amazonDigestPattern.MatchString(request.CandidateDigest) {
		return errors.New("amazon import identity or candidate digest is invalid")
	}
	if request.StartedAt.IsZero() || request.ImportedAt.Before(request.StartedAt) {
		return errors.New("amazon import timestamps are invalid")
	}
	counts := request.ProposedCounts
	values := []int{counts.Transactions, counts.Accounts, counts.Merchants, counts.Sources, counts.Groups, counts.Categories}
	total := 0
	for _, value := range values {
		if value < 0 || value > 1_000_000 {
			return errors.New("amazon proposed ID count is outside its bound")
		}
		total += value
	}
	if total > 3_000_000 {
		return errors.New("amazon proposed ID total is outside its bound")
	}
	return nil
}

func proposeAmazonIDs(counts store.AmazonIDCounts) (store.ProposedAmazonIDs, error) {
	var result store.ProposedAmazonIDs
	var err error
	if result.TransactionIDs, err = proposeEntityIDs(domain.EntityKindTransaction, counts.Transactions); err != nil {
		return result, err
	}
	if result.AccountIDs, err = proposeEntityIDs(domain.EntityKindAccount, counts.Accounts); err != nil {
		return result, err
	}
	if result.MerchantIDs, err = proposeEntityIDs(domain.EntityKindMerchant, counts.Merchants); err != nil {
		return result, err
	}
	if result.GroupIDs, err = proposeEntityIDs(domain.EntityKindGroup, counts.Groups); err != nil {
		return result, err
	}
	if result.CategoryIDs, err = proposeEntityIDs(domain.EntityKindCategory, counts.Categories); err != nil {
		return result, err
	}
	result.SourceIdentities = make([]string, counts.Sources)
	for index := range result.SourceIdentities {
		result.SourceIdentities[index], err = domain.NewAmazonSourceIdentity(rand.Reader)
		if err != nil {
			return result, fmt.Errorf("generate Amazon source identity: %w", err)
		}
	}
	return result, nil
}

func proposeEntityIDs(kind domain.EntityKind, count int) ([]domain.EntityID, error) {
	result := make([]domain.EntityID, count)
	for index := range result {
		id, err := domain.NewEntityID(kind, rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate %s identity: %w", kind, err)
		}
		result[index] = id
	}
	return result, nil
}

func validateAmazonImportPlan(state store.AmazonImportState, plan store.AmazonImportPlan) error {
	planned := domain.ProfileSnapshot{
		Revision: state.Snapshot.Revision, Committed: plan.Committed, Journal: plan.Journal,
		Cursor: plan.Cursor, KnownDrills: plan.KnownDrills,
	}
	if err := planned.Validate(); err != nil {
		return fmt.Errorf("amazon import plan: %w", err)
	}
	if _, err := profilereplay.Replay(planned); err != nil {
		return fmt.Errorf("amazon import plan replay: %w", err)
	}
	if state.Settings != nil && !reflect.DeepEqual(state.Settings, plan.Settings) {
		return errors.New("amazon import plan changes immutable settings")
	}
	if state.Settings == nil {
		if plan.Settings == nil || !domain.IsValidCurrency(plan.Settings.Currency) ||
			plan.Settings.Scale > 9 || plan.Settings.CreatedAt.IsZero() {
			return errors.New("amazon import plan does not install canonical settings")
		}
	}
	if err := validateAmazonLedger(plan.Committed, plan.Settings, plan.Items); err != nil {
		return fmt.Errorf("amazon import plan ledger: %w", err)
	}
	changedParts := make([]string, 0, 7)
	if !reflect.DeepEqual(state.Snapshot.Committed, plan.Committed) {
		changedParts = append(changedParts, "committed")
	}
	if !equivalentAmazonSlice(state.Snapshot.Journal, plan.Journal) || state.Snapshot.Cursor != plan.Cursor {
		changedParts = append(changedParts, "journal")
	}
	if !equivalentAmazonSlice(state.Snapshot.KnownDrills, plan.KnownDrills) {
		changedParts = append(changedParts, "known drills")
	}
	if !reflect.DeepEqual(state.Settings, plan.Settings) {
		changedParts = append(changedParts, "settings")
	}
	if !equivalentAmazonSlice(state.Items, plan.Items) {
		changedParts = append(changedParts, "items")
	}
	if !equivalentAmazonSlice(state.Allocations, plan.Allocations) {
		changedParts = append(changedParts, "allocations")
	}
	changed := len(changedParts) > 0
	if changed != plan.SemanticChange {
		return fmt.Errorf("amazon import semantic-change flag disagrees with the plan: %v", changedParts)
	}
	return nil
}

func validateAmazonLedger(
	committed domain.CommittedProfile,
	settings *store.AmazonSettings,
	items []store.AmazonOrderItem,
) error {
	if settings == nil {
		if len(items) == 0 {
			return nil
		}
		return errors.New("ledger exists without settings")
	}
	transactions := make(map[domain.EntityID]domain.TransactionRecord, len(committed.Transactions))
	for _, transaction := range committed.Transactions {
		transactions[transaction.ID] = transaction
	}
	identities := make(map[string]domain.ExternalIdentity, len(committed.ExternalIdentities))
	for _, identity := range committed.ExternalIdentities {
		identities[identity.Namespace+"\x00"+identity.ExternalID] = identity
	}
	active := make(map[domain.EntityID]struct{}, len(items))
	seen := make(map[domain.EntityID]struct{}, len(items))
	for index, item := range items {
		if _, duplicate := seen[item.LocalTransactionID]; duplicate {
			return fmt.Errorf("item[%d] duplicates local transaction", index)
		}
		seen[item.LocalTransactionID] = struct{}{}
		if item.LocalTransactionID == "" || item.SourceIdentity == "" ||
			item.LocalAccountID == "" || item.LocalMerchantID == "" || item.LocalCategoryID == "" {
			return fmt.Errorf("item[%d] has incomplete local identity", index)
		}
		if item.Currency != settings.Currency || item.Scale != settings.Scale ||
			!amazonDigestPattern.MatchString(item.IdentityFingerprint) ||
			!amazonDigestPattern.MatchString(item.FullFingerprint) {
			return fmt.Errorf("item[%d] has invalid source facts", index)
		}
		identity, exists := identities["amazon/order-item\x00"+item.SourceIdentity]
		if !exists || identity.EntityType != domain.EntityKindTransaction ||
			identity.EntityID != item.LocalTransactionID {
			return fmt.Errorf("item[%d] has no matching external identity", index)
		}
		transaction, exists := transactions[item.LocalTransactionID]
		if item.Retired {
			if exists {
				return fmt.Errorf("item[%d] is retired but transaction remains active", index)
			}
			continue
		}
		if !exists || transaction.Provider != "amazon" || transaction.ProviderID != item.SourceIdentity {
			return fmt.Errorf("item[%d] has no matching Amazon transaction", index)
		}
		if transaction.Date != item.OrderDate || transaction.Amount.Minor != item.AmountMinor ||
			transaction.Amount.Currency != item.Currency || transaction.Amount.Scale != item.Scale {
			return fmt.Errorf("item[%d] money or date differs from its transaction", index)
		}
		if transaction.AccountID != item.LocalAccountID || transaction.MerchantID != item.LocalMerchantID ||
			transaction.CategoryID != item.LocalCategoryID || transaction.Notes != item.LocalNotes ||
			transaction.Hidden != item.LocalHidden {
			return fmt.Errorf("item[%d] local state differs from its transaction", index)
		}
		active[item.LocalTransactionID] = struct{}{}
	}
	for _, transaction := range committed.Transactions {
		if transaction.Provider != "amazon" {
			continue
		}
		if _, exists := active[transaction.ID]; !exists {
			return fmt.Errorf("amazon transaction %q has no active ledger item", transaction.ID)
		}
	}
	return nil
}

func equivalentAmazonSlice[T any](left, right []T) bool {
	return (len(left) == 0 && len(right) == 0) || reflect.DeepEqual(left, right)
}

func cloneAmazonImportState(state store.AmazonImportState) store.AmazonImportState {
	state.Snapshot = state.Snapshot.Clone()
	if state.Settings != nil {
		settings := *state.Settings
		state.Settings = &settings
	}
	state.Items = append([]store.AmazonOrderItem(nil), state.Items...)
	for index := range state.Items {
		if state.Items[index].UnitPriceMinor != nil {
			value := *state.Items[index].UnitPriceMinor
			state.Items[index].UnitPriceMinor = &value
		}
	}
	state.Allocations = append([]store.LabelAllocation(nil), state.Allocations...)
	return state
}

func replaceAmazonSettings(ctx context.Context, connection *sql.Conn, before, after *store.AmazonSettings) error {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	if _, err := connection.ExecContext(ctx, "DELETE FROM amazon_profile_settings"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if after == nil {
		return nil
	}
	var taxonomy any
	if after.TaxonomySourceProfileID != "" {
		taxonomy = after.TaxonomySourceProfileID
	}
	_, err := connection.ExecContext(ctx, `
		INSERT INTO amazon_profile_settings(singleton, currency, scale, taxonomy_source_profile_id, created_at_unix_ms)
		VALUES (1, ?, ?, ?, ?)`, after.Currency, after.Scale, taxonomy, after.CreatedAt.UnixMilli())
	return mapDriverError(err, store.CodeStoreError)
}

func replaceAmazonItems(ctx context.Context, connection *sql.Conn, before, after []store.AmazonOrderItem) error {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	if _, err := connection.ExecContext(ctx, "DELETE FROM amazon_order_items"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	statement, err := connection.PrepareContext(ctx, `
		INSERT INTO amazon_order_items(
			local_transaction_id, source_identity, order_id, asin, asinless_key, product_name,
			order_date, quantity, amount_minor, unit_price_minor, currency, scale, order_status,
			shipment_status, identity_fingerprint, full_fingerprint, retired,
			local_account_id, local_merchant_id, local_category_id, local_notes, local_hidden
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = statement.Close() }()
	for _, item := range after {
		var asin any
		if item.ASIN != "" {
			asin = item.ASIN
		}
		var unit any
		if item.UnitPriceMinor != nil {
			unit = *item.UnitPriceMinor
		}
		if _, err = statement.ExecContext(ctx,
			item.LocalTransactionID, item.SourceIdentity, item.OrderID, asin, item.ASINLessKey,
			item.ProductName, item.OrderDate.String(), item.Quantity, item.AmountMinor, unit,
			item.Currency, item.Scale, item.OrderStatus, item.ShipmentStatus,
			item.IdentityFingerprint, item.FullFingerprint, booleanInteger(item.Retired),
			item.LocalAccountID, item.LocalMerchantID, item.LocalCategoryID,
			item.LocalNotes, booleanInteger(item.LocalHidden),
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func insertAmazonHistory(ctx context.Context, connection *sql.Conn, history store.AmazonImportHistory) error {
	_, err := connection.ExecContext(ctx, `
		INSERT INTO amazon_import_history(
			import_id, started_at_unix_ms, completed_at_unix_ms, source_revision,
			resulting_revision, candidate_digest, file_count, logical_record_count,
			blank_record_count, cancelled_record_count, inserted_count, updated_count,
			restored_count, retired_count, unchanged_count, removed_journal_targets,
			removed_journal_operations
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		history.ImportID, history.StartedAt.UnixMilli(), history.CompletedAt.UnixMilli(),
		history.SourceRevision, history.ResultingRevision, history.CandidateDigest,
		history.FileCount, history.LogicalRecordCount, history.BlankRecordCount,
		history.CancelledRecordCount, history.InsertedCount, history.UpdatedCount,
		history.RestoredCount, history.RetiredCount, history.UnchangedCount,
		history.RemovedJournalTargets, history.RemovedJournalOperations)
	return mapDriverError(err, store.CodeStoreError)
}
