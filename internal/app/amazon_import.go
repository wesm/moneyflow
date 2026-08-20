package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

const amazonProvider = "amazon"

// TaxonomyClone is one committed, point-in-time taxonomy captured from another profile.
type TaxonomyClone struct {
	SourceProfileID string
	Committed       domain.CommittedProfile
}

// AmazonImportRequest contains one parsed candidate and renderer-independent settings.
type AmazonImportRequest struct {
	Candidate     amazon.Candidate
	Settings      amazon.Settings
	TaxonomyClone *TaxonomyClone
	StartedAt     time.Time
	ImportedAt    time.Time
}

// AmazonImportResult is the counts-only semantic result of one import.
type AmazonImportResult struct {
	Revision                             uint64
	Inserted, Updated, Restored, Retired int
	Unchanged                            int
	RemovedJournalTargets                int
	RemovedJournalOperations             int
	NoOp                                 bool
}

// ImportAmazon installs a parsed candidate against the freshest durable profile state.
func (service *Service) ImportAmazon(ctx context.Context, request AmazonImportRequest) (AmazonImportResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if service.profile == nil {
		return AmazonImportResult{}, newAppError(AppInvalidOperation, service.Revision(), errors.New("amazon import requires a durable profile"))
	}
	result, err := ImportAmazonProfile(ctx, service.profile, request)
	if err != nil {
		return AmazonImportResult{}, err
	}
	commitRevision := result.Revision
	if err = service.reloadExpected(ctx, commitRevision); err != nil {
		return AmazonImportResult{}, err
	}
	return result, nil
}

// ImportAmazonProfile imports into a durable profile before a first application service can load it.
func ImportAmazonProfile(ctx context.Context, profile store.Profile, request AmazonImportRequest) (AmazonImportResult, error) {
	if profile == nil {
		return AmazonImportResult{}, newAppError(AppInvalidOperation, 0, errors.New("amazon import requires a durable profile"))
	}
	if request.StartedAt.IsZero() {
		request.StartedAt = request.ImportedAt
	}
	if request.ImportedAt.IsZero() || request.Candidate.Digest == "" {
		return AmazonImportResult{}, newAppError(AppInvalidOperation, 0, errors.New("amazon import request is incomplete"))
	}
	importID, err := domain.NewOperationID(rand.Reader)
	if err != nil {
		return AmazonImportResult{}, newAppError(AppStoreError, 0, err)
	}
	commit, err := profile.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: importID, StartedAt: request.StartedAt, ImportedAt: request.ImportedAt,
		CandidateDigest: request.Candidate.Digest, ProposedCounts: amazonProposedCounts(request),
	}, func(state store.AmazonImportState, proposed store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		return BuildAmazonImportPlan(state, proposed, request)
	})
	if err != nil {
		return AmazonImportResult{}, mapAppError(err, 0)
	}
	history := commit.History
	return AmazonImportResult{
		Revision: commit.Revision, Inserted: history.InsertedCount, Updated: history.UpdatedCount,
		Restored: history.RestoredCount, Retired: history.RetiredCount,
		Unchanged: history.UnchangedCount, NoOp: !commit.SemanticChange,
	}, nil
}

func amazonProposedCounts(request AmazonImportRequest) store.AmazonIDCounts {
	counts := store.AmazonIDCounts{
		Transactions: len(request.Candidate.Rows), Sources: len(request.Candidate.Rows),
		Accounts: len(request.Candidate.ObservedOrderIDs), Merchants: len(request.Candidate.Rows),
	}
	if request.TaxonomyClone != nil {
		for _, group := range request.TaxonomyClone.Committed.Groups {
			if !group.Protected && !group.Retired {
				counts.Groups++
			}
		}
		for _, category := range request.TaxonomyClone.Committed.Categories {
			if !category.Protected && !category.Retired {
				counts.Categories++
			}
		}
	}
	return counts
}

// BuildAmazonImportPlan reconciles one candidate without consulting clocks, randomness, SQL, or I/O.
func BuildAmazonImportPlan(
	state store.AmazonImportState,
	proposed store.ProposedAmazonIDs,
	request AmazonImportRequest,
) (store.AmazonImportPlan, error) {
	if err := validateAmazonSettings(state.Settings, request); err != nil {
		return store.AmazonImportPlan{}, err
	}
	reconciled, err := reconcileAmazonRows(state.Items, request.Candidate.Rows, request.Candidate.ObservedOrderIDs, proposed)
	if err != nil {
		return store.AmazonImportPlan{}, err
	}
	committed := state.Snapshot.Committed.Clone()
	if state.Settings == nil {
		committed, err = initializeAmazonTaxonomy(committed, request.TaxonomyClone, proposed)
		if err != nil {
			return store.AmazonImportPlan{}, err
		}
	}
	allocations := append([]store.LabelAllocation(nil), state.Allocations...)
	committed, allocations, err = materializeAmazonCommitted(committed, state.Items, reconciled.Items, allocations, proposed)
	if err != nil {
		return store.AmazonImportPlan{}, err
	}
	rebased := profilereplay.ProviderRebaseResult{}
	if state.Settings == nil && len(state.Snapshot.Committed.Transactions) == 0 &&
		len(state.Snapshot.Journal) == 0 {
		rebased.Journal = []domain.Operation{}
	} else {
		rebased, err = profilereplay.RebaseProviderJournal(
			state.Snapshot.Committed, committed, state.Snapshot.Journal, state.Snapshot.Cursor,
		)
		if err != nil {
			return store.AmazonImportPlan{}, err
		}
	}
	settings := state.Settings
	if settings == nil {
		taxonomySource := ""
		if request.TaxonomyClone != nil {
			taxonomySource = request.TaxonomyClone.SourceProfileID
		}
		settings = &store.AmazonSettings{
			Currency: request.Settings.Currency, Scale: request.Settings.Scale,
			TaxonomySourceProfileID: taxonomySource, CreatedAt: request.ImportedAt.UTC(),
		}
	}
	history := store.AmazonImportHistory{
		FileCount: request.Candidate.FileCount, LogicalRecordCount: request.Candidate.LogicalRecordCount,
		BlankRecordCount:     request.Candidate.BlankRecordCount,
		CancelledRecordCount: request.Candidate.CancelledRecordCount,
		InsertedCount:        reconciled.Inserted, UpdatedCount: reconciled.Updated,
		RestoredCount: reconciled.Restored, RetiredCount: reconciled.Retired,
		UnchangedCount: reconciled.Unchanged,
	}
	semantic := !reflect.DeepEqual(state.Snapshot.Committed, committed) ||
		!equivalentAppSlice(state.Snapshot.Journal, rebased.Journal) || state.Snapshot.Cursor != rebased.Cursor ||
		!reflect.DeepEqual(state.Settings, settings) || !equivalentAppSlice(state.Items, reconciled.Items) ||
		!equivalentAppSlice(state.Allocations, allocations)
	return store.AmazonImportPlan{
		Committed: committed, Journal: rebased.Journal, Cursor: rebased.Cursor,
		KnownDrills: append([]domain.DrillIdentity(nil), state.Snapshot.KnownDrills...),
		Settings:    settings, Items: reconciled.Items, Allocations: allocations,
		History: history, SemanticChange: semantic,
	}, nil
}

func validateAmazonSettings(current *store.AmazonSettings, request AmazonImportRequest) error {
	if len(request.Settings.Currency) != 3 || request.Settings.Scale > 9 {
		return errors.New("amazon import settings are invalid")
	}
	if current == nil {
		return nil
	}
	if current.Currency != request.Settings.Currency || current.Scale != request.Settings.Scale {
		return errors.New("amazon import settings do not match the profile")
	}
	if request.TaxonomyClone != nil {
		return errors.New("amazon taxonomy can only be cloned during first import")
	}
	return nil
}

func initializeAmazonTaxonomy(
	committed domain.CommittedProfile,
	clone *TaxonomyClone,
	proposed store.ProposedAmazonIDs,
) (domain.CommittedProfile, error) {
	committed.Groups = []domain.CategoryGroup{{
		ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
		CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
	}}
	committed.Categories = []domain.Category{{
		ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
		Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
	}}
	if clone == nil {
		return committed, nil
	}
	groupMap := map[domain.EntityID]domain.EntityID{domain.UncategorizedGroupID: domain.UncategorizedGroupID}
	groups := make([]domain.CategoryGroup, 0)
	for _, group := range clone.Committed.Groups {
		if !group.Protected && !group.Retired {
			groups = append(groups, group)
		}
	}
	slices.SortFunc(groups, func(left, right domain.CategoryGroup) int { return strings.Compare(string(left.ID), string(right.ID)) })
	if len(groups) > len(proposed.GroupIDs) {
		return domain.CommittedProfile{}, errors.New("amazon taxonomy group IDs exhausted")
	}
	for index, group := range groups {
		groupMap[group.ID] = proposed.GroupIDs[index]
		committed.Groups = append(committed.Groups, domain.CategoryGroup{
			ID: proposed.GroupIDs[index], Label: group.Label, CollisionKey: group.CollisionKey,
		})
	}
	categories := make([]domain.Category, 0)
	for _, category := range clone.Committed.Categories {
		if !category.Protected && !category.Retired {
			categories = append(categories, category)
		}
	}
	slices.SortFunc(categories, func(left, right domain.Category) int { return strings.Compare(string(left.ID), string(right.ID)) })
	if len(categories) > len(proposed.CategoryIDs) {
		return domain.CommittedProfile{}, errors.New("amazon taxonomy category IDs exhausted")
	}
	for index, category := range categories {
		groupID, exists := groupMap[category.GroupID]
		if !exists {
			return domain.CommittedProfile{}, errors.New("amazon taxonomy category has no active group")
		}
		committed.Categories = append(committed.Categories, domain.Category{
			ID: proposed.CategoryIDs[index], GroupID: groupID, Label: category.Label,
			CollisionKey: category.CollisionKey,
		})
	}
	return committed, nil
}

func materializeAmazonCommitted(
	committed domain.CommittedProfile,
	beforeItems, items []store.AmazonOrderItem,
	allocations []store.LabelAllocation,
	proposed store.ProposedAmazonIDs,
) (domain.CommittedProfile, []store.LabelAllocation, error) {
	identities := make(map[string]domain.ExternalIdentity)
	for _, identity := range committed.ExternalIdentities {
		identities[identity.Namespace+"\x00"+identity.ExternalID] = identity
	}
	accounts := make(map[domain.EntityID]domain.Account)
	for _, value := range committed.Accounts {
		accounts[value.ID] = value
	}
	merchants := make(map[domain.EntityID]domain.Merchant)
	for _, value := range committed.Merchants {
		merchants[value.ID] = value
	}
	transactions := make(map[domain.EntityID]domain.TransactionRecord)
	for _, value := range committed.Transactions {
		transactions[value.ID] = value
	}
	allocationMap := make(map[string]store.LabelAllocation)
	for _, value := range allocations {
		allocationMap[value.Namespace+"\x00"+value.ExternalID] = value
	}
	accountCursor, merchantCursor := 0, 0
	accountForOrder := make(map[string]domain.EntityID)
	merchantForProduct := make(map[string]domain.EntityID)
	for _, item := range items {
		if item.Retired {
			continue
		}
		accountID, ok := resolveAmazonIdentity(identities, domain.EntityKindAccount, "amazon/order", item.OrderID)
		if !ok {
			if accountCursor >= len(proposed.AccountIDs) {
				return domain.CommittedProfile{}, nil, errors.New("amazon account IDs exhausted")
			}
			accountID = proposed.AccountIDs[accountCursor]
			accountCursor++
			label, key, allocation, allocationErr := allocateAmazonLabel(domain.EntityKindAccount, "amazon/order", item.OrderID, item.OrderID, accountID, accounts, merchants, allocationMap)
			if allocationErr != nil {
				return domain.CommittedProfile{}, nil, allocationErr
			}
			accounts[accountID] = domain.Account{ID: accountID, Label: label, CollisionKey: key}
			allocationMap[allocation.Namespace+"\x00"+allocation.ExternalID] = allocation
			identities["amazon/order\x00"+item.OrderID] = domain.ExternalIdentity{EntityType: domain.EntityKindAccount, EntityID: accountID, Namespace: "amazon/order", ExternalID: item.OrderID}
		}
		accountForOrder[item.OrderID] = accountID
		productKey := amazonProductKey(item)
		merchantID, ok := resolveAmazonIdentity(identities, domain.EntityKindMerchant, "amazon/product", productKey)
		if !ok {
			if merchantCursor >= len(proposed.MerchantIDs) {
				return domain.CommittedProfile{}, nil, errors.New("amazon merchant IDs exhausted")
			}
			merchantID = proposed.MerchantIDs[merchantCursor]
			merchantCursor++
			label, key, allocation, allocationErr := allocateAmazonLabel(domain.EntityKindMerchant, "amazon/product", productKey, item.ProductName, merchantID, accounts, merchants, allocationMap)
			if allocationErr != nil {
				return domain.CommittedProfile{}, nil, allocationErr
			}
			merchants[merchantID] = domain.Merchant{ID: merchantID, Label: label, CollisionKey: key}
			allocationMap[allocation.Namespace+"\x00"+allocation.ExternalID] = allocation
			identities["amazon/product\x00"+productKey] = domain.ExternalIdentity{EntityType: domain.EntityKindMerchant, EntityID: merchantID, Namespace: "amazon/product", ExternalID: productKey}
		}
		merchantForProduct[productKey] = merchantID
	}
	previousActive := make(map[domain.EntityID]struct{})
	for _, item := range beforeItems {
		if !item.Retired {
			previousActive[item.LocalTransactionID] = struct{}{}
		}
	}
	activeNow := make(map[domain.EntityID]struct{})
	for _, item := range items {
		if item.Retired {
			continue
		}
		activeNow[item.LocalTransactionID] = struct{}{}
		previous, existed := transactions[item.LocalTransactionID]
		categoryID := domain.UncategorizedCategoryID
		merchantID := merchantForProduct[amazonProductKey(item)]
		accountID := accountForOrder[item.OrderID]
		notes := ""
		hidden := false
		if existed {
			categoryID, merchantID, accountID = previous.CategoryID, previous.MerchantID, previous.AccountID
			notes, hidden = previous.Notes, previous.Hidden
		}
		transactions[item.LocalTransactionID] = domain.TransactionRecord{
			ID: item.LocalTransactionID, Provider: amazonProvider, ProviderID: item.SourceIdentity,
			AccountID: accountID, MerchantID: merchantID, CategoryID: categoryID,
			Date: item.OrderDate, Amount: domain.Money{Minor: item.AmountMinor, Currency: item.Currency, Scale: item.Scale},
			Notes: notes, Hidden: hidden, Metadata: amazonItemMetadata(item),
		}
		key := "amazon/order-item\x00" + item.SourceIdentity
		if _, exists := identities[key]; !exists {
			identities[key] = domain.ExternalIdentity{EntityType: domain.EntityKindTransaction, EntityID: item.LocalTransactionID, Namespace: "amazon/order-item", ExternalID: item.SourceIdentity}
		}
	}
	for id := range previousActive {
		if _, retained := activeNow[id]; !retained {
			delete(transactions, id)
		}
	}
	committed.Accounts = amazonAccountValues(accounts)
	committed.Merchants = amazonMerchantValues(merchants)
	committed.Transactions = amazonTransactionValues(transactions)
	committed.ExternalIdentities = amazonIdentityValues(identities)
	allocations = amazonAllocationValues(allocationMap)
	return committed, allocations, committed.Validate()
}

func resolveAmazonIdentity(values map[string]domain.ExternalIdentity, kind domain.EntityKind, namespace, externalID string) (domain.EntityID, bool) {
	value, ok := values[namespace+"\x00"+externalID]
	return value.EntityID, ok && value.EntityType == kind
}

func amazonProductKey(item store.AmazonOrderItem) string {
	if item.ASIN != "" {
		return "asin:" + item.ASIN
	}
	return "asinless:" + item.ASINLessKey
}

func allocateAmazonLabel(
	kind domain.EntityKind, namespace, externalID, providerLabel string, localID domain.EntityID,
	accounts map[domain.EntityID]domain.Account, merchants map[domain.EntityID]domain.Merchant,
	allocations map[string]store.LabelAllocation,
) (string, string, store.LabelAllocation, error) {
	baseKey, err := domain.CollisionKey(providerLabel)
	if err != nil {
		return "", "", store.LabelAllocation{}, err
	}
	reserved := make(map[string]struct{}, len(accounts)+len(merchants))
	for _, value := range accounts {
		if !value.Retired {
			reserved[value.CollisionKey] = struct{}{}
		}
	}
	for _, value := range merchants {
		if !value.Retired {
			reserved[value.CollisionKey] = struct{}{}
		}
	}
	label, collisionKey, suffix, unsuffixed := providerLabel, baseKey, "", true
	if _, collision := reserved[baseKey]; collision {
		digest := sha256.Sum256([]byte(namespace + "\x00" + externalID + "\x00" + string(localID)))
		material := hex.EncodeToString(digest[:])
		for length := 4; length <= len(material); length += 2 {
			suffix = material[:length]
			label = providerLabel + providerSuffixSeparator + suffix
			collisionKey, err = domain.CollisionKey(label)
			if err != nil {
				return "", "", store.LabelAllocation{}, err
			}
			if _, exists := reserved[collisionKey]; !exists {
				break
			}
		}
		unsuffixed = false
	}
	allocation := store.LabelAllocation{
		Kind: kind, Namespace: namespace, ExternalID: externalID, BaseCollisionKey: baseKey,
		DisplayLabel: label, ProviderLabel: providerLabel, SuffixToken: suffix, Unsuffixed: unsuffixed,
	}
	allocations[namespace+"\x00"+externalID] = allocation
	return label, collisionKey, allocation, nil
}

func amazonItemMetadata(item store.AmazonOrderItem) map[string]string {
	metadata := map[string]string{
		"amazon_order_id": item.OrderID, "amazon_product_name": item.ProductName,
		"amazon_quantity":     strconv.FormatInt(item.Quantity, 10),
		"amazon_order_status": item.OrderStatus, "amazon_shipment_status": item.ShipmentStatus,
	}
	if item.ASIN != "" {
		metadata["amazon_asin"] = item.ASIN
	}
	if item.UnitPriceMinor != nil {
		metadata["amazon_unit_price_minor"] = strconv.FormatInt(*item.UnitPriceMinor, 10)
	}
	return metadata
}

func amazonAccountValues(values map[domain.EntityID]domain.Account) []domain.Account {
	result := make([]domain.Account, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right domain.Account) int { return strings.Compare(string(left.ID), string(right.ID)) })
	return result
}
func amazonMerchantValues(values map[domain.EntityID]domain.Merchant) []domain.Merchant {
	result := make([]domain.Merchant, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right domain.Merchant) int { return strings.Compare(string(left.ID), string(right.ID)) })
	return result
}
func amazonTransactionValues(values map[domain.EntityID]domain.TransactionRecord) []domain.TransactionRecord {
	result := make([]domain.TransactionRecord, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right domain.TransactionRecord) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	return result
}
func amazonIdentityValues(values map[string]domain.ExternalIdentity) []domain.ExternalIdentity {
	result := make([]domain.ExternalIdentity, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right domain.ExternalIdentity) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	return result
}
func amazonAllocationValues(values map[string]store.LabelAllocation) []store.LabelAllocation {
	result := make([]store.LabelAllocation, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right store.LabelAllocation) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	return result
}
func equivalentAppSlice[T any](left, right []T) bool {
	return (len(left) == 0 && len(right) == 0) || reflect.DeepEqual(left, right)
}
