package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// MaxToolRows is the largest collection window exposed to renderer adapters.
const MaxToolRows = 1_000

// TransactionFilter selects one detached effective-transaction projection.
type TransactionFilter struct {
	StartDate         *domain.Date
	EndDate           *domain.Date
	LiteralQuery      string
	CategoryID        domain.EntityID
	CategoryLabel     string
	MerchantSubstring string
	MinAmount         *domain.Money
	MaxAmount         *domain.Money
	IncludeHidden     bool
}

// TransactionWindowRequest requests one bounded effective-transaction window.
type TransactionWindowRequest struct {
	ExpectedRevision uint64
	Filter           TransactionFilter
	Offset           int
	Limit            int
}

// TransactionWindow is an independently owned transaction result.
type TransactionWindow struct {
	Revision uint64
	Total    int
	Offset   int
	Limit    int
	Rows     []domain.Transaction
	Pending  PendingSummary
}

// CollectionWindowRequest requests one independently bounded catalog collection.
type CollectionWindowRequest struct {
	Offset int
	Limit  int
}

// CatalogWindowRequest independently windows every catalog collection.
type CatalogWindowRequest struct {
	ExpectedRevision uint64
	Groups           CollectionWindowRequest
	Categories       CollectionWindowRequest
	Merchants        CollectionWindowRequest
}

// GroupProjection is one active group plus its effective transaction count.
type GroupProjection struct {
	Group            domain.CategoryGroup
	TransactionCount int
}

// CategoryProjection is one active category plus its effective transaction count.
type CategoryProjection struct {
	Category         domain.Category
	TransactionCount int
}

// MerchantProjection is one active merchant plus exact partitioned totals.
type MerchantProjection struct {
	Merchant         domain.Merchant
	TransactionCount int
	Totals           []domain.Money
}

// CatalogProjection contains detached active taxonomy and merchant windows.
type CatalogProjection struct {
	Revision       uint64
	GroupTotal     int
	GroupOffset    int
	Groups         []GroupProjection
	CategoryTotal  int
	CategoryOffset int
	Categories     []CategoryProjection
	MerchantTotal  int
	MerchantOffset int
	Merchants      []MerchantProjection
}

// MoneyPartition summarizes one active exact-money partition.
type MoneyPartition struct {
	Currency         domain.Currency
	Scale            uint8
	TransactionCount int
}

// AccountProjection is the credential-blind profile summary shared by adapters.
type AccountProjection struct {
	Revision         uint64
	ProfileKind      string
	MoneyPartitions  []MoneyPartition
	TransactionCount int
	DateRange        *domain.DateRange
	CategoryCount    int
	Pending          PendingSummary
	Capabilities     []Capability
	Provider         ProviderStatus
	Write            ProviderWriteStatus
}

// SpendingGroup is one exact-money expense aggregate.
type SpendingGroup struct {
	ID               string
	Label            string
	Currency         domain.Currency
	Scale            uint8
	TransactionCount int
	Total            domain.Money
}

// SpendingSummaryRequest requests a bounded effective expense aggregation.
type SpendingSummaryRequest struct {
	ExpectedRevision uint64
	StartDate        *domain.Date
	EndDate          *domain.Date
	GroupBy          domain.Dimension
	Offset           int
	Limit            int
}

// SpendingSummary is one independently owned expense aggregation window.
type SpendingSummary struct {
	Revision uint64
	Total    int
	Offset   int
	Limit    int
	Groups   []SpendingGroup
}

type toolProjectionState struct {
	revision      uint64
	transactions  []domain.Transaction
	snapshot      *EffectiveSnapshot
	profileKind   string
	providerState ProviderStatus
	writeState    ProviderWriteStatus
	capabilities  []Capability
	pending       PendingSummary
}

// TransactionWindow returns a current effective snapshot filtered with literal MCP semantics.
func (service *Service) TransactionWindow(
	ctx context.Context,
	request TransactionWindowRequest,
) (TransactionWindow, error) {
	state, err := service.toolProjectionState(ctx, request.ExpectedRevision)
	if err != nil {
		return TransactionWindow{}, err
	}
	offset, limit, err := normalizeToolWindow(request.Offset, request.Limit)
	if err != nil {
		return TransactionWindow{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	filter := request.Filter
	if err = validateTransactionFilter(filter); err != nil {
		return TransactionWindow{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	categoryID, err := resolveToolCategory(filter, state.snapshot)
	if err != nil {
		return TransactionWindow{}, newAppError(AppInvalidTarget, state.revision, err)
	}
	rows := make([]domain.Transaction, 0, len(state.transactions))
	query := strings.ToLower(filter.LiteralQuery)
	merchant := strings.ToLower(filter.MerchantSubstring)
	for _, transaction := range state.transactions {
		if !matchesToolTransaction(transaction, filter, categoryID, query, merchant) {
			continue
		}
		rows = append(rows, transaction.Clone())
	}
	sortToolTransactions(rows)
	total := len(rows)
	start := min(offset, total)
	end := min(start+limit, total)
	return TransactionWindow{
		Revision: state.revision, Total: total, Offset: start, Limit: limit,
		Rows: append([]domain.Transaction(nil), rows[start:end]...), Pending: state.pending,
	}, nil
}

// CatalogProjection returns independently bounded active catalog collections.
func (service *Service) CatalogProjection(
	ctx context.Context,
	request CatalogWindowRequest,
) (CatalogProjection, error) {
	state, err := service.toolProjectionState(ctx, request.ExpectedRevision)
	if err != nil {
		return CatalogProjection{}, err
	}
	if state.snapshot == nil {
		return catalogFromTransactions(state, request)
	}
	groupOffset, groupLimit, err := normalizeToolWindow(request.Groups.Offset, request.Groups.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	categoryOffset, categoryLimit, err := normalizeToolWindow(request.Categories.Offset, request.Categories.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	merchantOffset, merchantLimit, err := normalizeToolWindow(request.Merchants.Offset, request.Merchants.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	groups, categories, merchants := buildCatalogEntries(*state.snapshot, state.transactions)
	result := CatalogProjection{Revision: state.revision}
	result.GroupTotal, result.GroupOffset, result.Groups = len(groups), min(groupOffset, len(groups)), windowGroups(groups, groupOffset, groupLimit)
	result.CategoryTotal, result.CategoryOffset, result.Categories = len(categories), min(categoryOffset, len(categories)), windowCategories(categories, categoryOffset, categoryLimit)
	result.MerchantTotal, result.MerchantOffset, result.Merchants = len(merchants), min(merchantOffset, len(merchants)), windowMerchants(merchants, merchantOffset, merchantLimit)
	return result, nil
}

// AccountProjection returns a current credential-blind profile summary.
func (service *Service) AccountProjection(ctx context.Context, expected uint64) (AccountProjection, error) {
	state, err := service.toolProjectionState(ctx, expected)
	if err != nil {
		return AccountProjection{}, err
	}
	partitions := make(map[string]MoneyPartition)
	var first, last domain.Date
	for index, transaction := range state.transactions {
		key := string(transaction.Amount.Currency) + "\x00" + string(rune(transaction.Amount.Scale))
		partition := partitions[key]
		partition.Currency, partition.Scale = transaction.Amount.Currency, transaction.Amount.Scale
		partition.TransactionCount++
		partitions[key] = partition
		if index == 0 || transaction.Date.Compare(first) < 0 {
			first = transaction.Date
		}
		if index == 0 || transaction.Date.Compare(last) > 0 {
			last = transaction.Date
		}
	}
	moneyPartitions := make([]MoneyPartition, 0, len(partitions))
	for _, partition := range partitions {
		moneyPartitions = append(moneyPartitions, partition)
	}
	sort.Slice(moneyPartitions, func(left, right int) bool {
		if moneyPartitions[left].Currency != moneyPartitions[right].Currency {
			return moneyPartitions[left].Currency < moneyPartitions[right].Currency
		}
		return moneyPartitions[left].Scale < moneyPartitions[right].Scale
	})
	var dateRange *domain.DateRange
	if len(state.transactions) > 0 {
		dateRange = &domain.DateRange{Start: first, End: last}
	}
	categoryCount := 0
	if state.snapshot != nil {
		for _, category := range state.snapshot.Effective.Categories {
			if !category.Retired {
				categoryCount++
			}
		}
	} else {
		seen := make(map[string]struct{})
		for _, transaction := range state.transactions {
			seen[transaction.Category.ID] = struct{}{}
		}
		categoryCount = len(seen)
	}
	return AccountProjection{
		Revision: state.revision, ProfileKind: state.profileKind, MoneyPartitions: moneyPartitions,
		TransactionCount: len(state.transactions), DateRange: dateRange, CategoryCount: categoryCount,
		Pending: state.pending, Capabilities: append([]Capability(nil), state.capabilities...),
		Provider: state.providerState, Write: state.writeState,
	}, nil
}

// SpendingSummary returns expense-only groups from the effective snapshot.
func (service *Service) SpendingSummary(
	ctx context.Context,
	request SpendingSummaryRequest,
) (SpendingSummary, error) {
	if request.GroupBy != domain.DimensionCategory && request.GroupBy != domain.DimensionMerchant {
		return SpendingSummary{}, newAppError(AppInvalidOperation, service.Revision(), errors.New("spending group is invalid"))
	}
	state, err := service.toolProjectionState(ctx, request.ExpectedRevision)
	if err != nil {
		return SpendingSummary{}, err
	}
	if err = validateTransactionFilter(TransactionFilter{StartDate: request.StartDate, EndDate: request.EndDate}); err != nil {
		return SpendingSummary{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	offset, limit, err := normalizeToolWindow(request.Offset, request.Limit)
	if err != nil {
		return SpendingSummary{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	type key struct {
		id, currency string
		scale        uint8
	}
	groups := make(map[key]SpendingGroup)
	for _, transaction := range state.transactions {
		if request.StartDate != nil && transaction.Date.Compare(*request.StartDate) < 0 ||
			request.EndDate != nil && transaction.Date.Compare(*request.EndDate) > 0 {
			continue
		}
		if transaction.Hidden || transaction.Amount.Minor >= 0 {
			continue
		}
		id, label := transaction.Category.ID, transaction.Category.Name
		if request.GroupBy == domain.DimensionMerchant {
			id, label = transaction.Merchant.ID, transaction.Merchant.Name
		}
		groupKey := key{id: id, currency: string(transaction.Amount.Currency), scale: transaction.Amount.Scale}
		group := groups[groupKey]
		group.ID, group.Label = id, label
		group.Currency, group.Scale = transaction.Amount.Currency, transaction.Amount.Scale
		group.TransactionCount++
		if group.Total.Currency == "" {
			group.Total = domain.Money{Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale}
		}
		group.Total, err = group.Total.Add(transaction.Amount)
		if err != nil {
			return SpendingSummary{}, newAppError(AppStoreCorrupt, state.revision, err)
		}
		groups[groupKey] = group
	}
	values := make([]SpendingGroup, 0, len(groups))
	for _, group := range groups {
		values = append(values, group)
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].Total.Minor != values[right].Total.Minor {
			return values[left].Total.Minor < values[right].Total.Minor
		}
		if lowerLeft, lowerRight := strings.ToLower(values[left].Label), strings.ToLower(values[right].Label); lowerLeft != lowerRight {
			return lowerLeft < lowerRight
		}
		if values[left].ID != values[right].ID {
			return values[left].ID < values[right].ID
		}
		if values[left].Currency != values[right].Currency {
			return values[left].Currency < values[right].Currency
		}
		return values[left].Scale < values[right].Scale
	})
	start := min(offset, len(values))
	end := min(start+limit, len(values))
	return SpendingSummary{Revision: state.revision, Total: len(values), Offset: start, Limit: limit, Groups: append([]SpendingGroup(nil), values[start:end]...)}, nil
}

func (service *Service) toolProjectionState(ctx context.Context, expected uint64) (toolProjectionState, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return toolProjectionState{}, err
	}
	service.mu.RLock()
	state := toolProjectionState{
		revision: service.Revision(), profileKind: service.profileKind,
		transactions: make([]domain.Transaction, len(service.transactions)),
	}
	for index := range service.transactions {
		state.transactions[index] = service.transactions[index].Clone()
	}
	if service.snapshot != nil {
		state.snapshot = cloneEffectiveSnapshot(*service.snapshot)
		state.pending = pendingSummary(*state.snapshot)
		state.providerState = providerStatusFromState(service.providerState)
		state.writeState = providerWriteStatusFromState(service.providerState)
	}
	service.mu.RUnlock()
	if expected != 0 && expected != state.revision {
		return toolProjectionState{}, newAppError(AppRevisionConflict, state.revision, errors.New("tool projection revision is stale"))
	}
	if state.snapshot != nil {
		state.capabilities = service.capabilitiesForStateSnapshot(*state.snapshot, DefaultViewState())
	}
	return state, nil
}

func validateTransactionFilter(filter TransactionFilter) error {
	if filter.StartDate != nil && filter.EndDate != nil && filter.StartDate.Compare(*filter.EndDate) > 0 {
		return errors.New("transaction date range is reversed")
	}
	if filter.CategoryID != "" && filter.CategoryLabel != "" {
		return errors.New("category ID and label are mutually exclusive")
	}
	if filter.MinAmount != nil && filter.MaxAmount != nil {
		if filter.MinAmount.Currency != filter.MaxAmount.Currency || filter.MinAmount.Scale != filter.MaxAmount.Scale {
			return errors.New("amount bounds use different money partitions")
		}
		if filter.MinAmount.Minor > filter.MaxAmount.Minor {
			return errors.New("amount range is reversed")
		}
	}
	return nil
}

func resolveToolCategory(filter TransactionFilter, snapshot *EffectiveSnapshot) (domain.EntityID, error) {
	if filter.CategoryID != "" {
		if snapshot != nil {
			for _, category := range snapshot.Effective.Categories {
				if category.ID == filter.CategoryID && !category.Retired {
					return filter.CategoryID, nil
				}
			}
			return "", errors.New("category is absent")
		}
		return filter.CategoryID, nil
	}
	if filter.CategoryLabel == "" {
		return "", nil
	}
	if snapshot == nil {
		return "", errors.New("category labels require a durable profile")
	}
	key, err := domain.CollisionKey(filter.CategoryLabel)
	if err != nil {
		return "", err
	}
	var result domain.EntityID
	for _, category := range snapshot.Effective.Categories {
		if category.Retired || category.CollisionKey != key {
			continue
		}
		if result != "" {
			return "", errors.New("category label is ambiguous")
		}
		result = category.ID
	}
	if result == "" {
		return "", errors.New("category is absent")
	}
	return result, nil
}

func matchesToolTransaction(
	transaction domain.Transaction,
	filter TransactionFilter,
	categoryID domain.EntityID,
	query, merchant string,
) bool {
	if filter.StartDate != nil && transaction.Date.Compare(*filter.StartDate) < 0 ||
		filter.EndDate != nil && transaction.Date.Compare(*filter.EndDate) > 0 ||
		!filter.IncludeHidden && transaction.Hidden ||
		categoryID != "" && transaction.Category.ID != string(categoryID) {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(transaction.Merchant.Name), query) &&
		!strings.Contains(strings.ToLower(transaction.Category.Name), query) &&
		!strings.Contains(strings.ToLower(transaction.Notes), query) {
		return false
	}
	if merchant != "" && !strings.Contains(strings.ToLower(transaction.Merchant.Name), merchant) {
		return false
	}
	if filter.MinAmount != nil {
		if transaction.Amount.Currency != filter.MinAmount.Currency || transaction.Amount.Scale != filter.MinAmount.Scale || transaction.Amount.Minor < filter.MinAmount.Minor {
			return false
		}
	}
	if filter.MaxAmount != nil {
		if transaction.Amount.Currency != filter.MaxAmount.Currency || transaction.Amount.Scale != filter.MaxAmount.Scale || transaction.Amount.Minor > filter.MaxAmount.Minor {
			return false
		}
	}
	return true
}

func sortToolTransactions(rows []domain.Transaction) {
	sort.Slice(rows, func(left, right int) bool {
		comparison := rows[left].Date.Compare(rows[right].Date)
		if comparison != 0 {
			return comparison > 0
		}
		return rows[left].ID < rows[right].ID
	})
}

func normalizeToolWindow(offset, limit int) (int, int, error) {
	if offset < 0 || limit <= 0 || limit > MaxToolRows {
		return 0, 0, errors.New("tool collection window is invalid")
	}
	return offset, limit, nil
}

func buildCatalogEntries(snapshot EffectiveSnapshot, transactions []domain.Transaction) ([]GroupProjection, []CategoryProjection, []MerchantProjection) {
	groupCounts := make(map[domain.EntityID]int)
	categoryCounts := make(map[domain.EntityID]int)
	merchantCounts := make(map[domain.EntityID]int)
	merchantTotals := make(map[domain.EntityID]map[string]domain.Money)
	for _, transaction := range transactions {
		groupCounts[domain.EntityID(transaction.Category.GroupID)]++
		categoryCounts[domain.EntityID(transaction.Category.ID)]++
		merchantID := domain.EntityID(transaction.Merchant.ID)
		merchantCounts[merchantID]++
		if merchantTotals[merchantID] == nil {
			merchantTotals[merchantID] = make(map[string]domain.Money)
		}
		key := string(transaction.Amount.Currency) + "\x00" + string(rune(transaction.Amount.Scale))
		total := merchantTotals[merchantID][key]
		if total.Currency == "" {
			total = domain.Money{Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale}
		}
		total, _ = total.Add(transaction.Amount)
		merchantTotals[merchantID][key] = total
	}
	groups := make([]GroupProjection, 0)
	for _, group := range snapshot.Effective.Groups {
		if !group.Retired {
			groups = append(groups, GroupProjection{Group: group, TransactionCount: groupCounts[group.ID]})
		}
	}
	categories := make([]CategoryProjection, 0)
	for _, category := range snapshot.Effective.Categories {
		if !category.Retired {
			categories = append(categories, CategoryProjection{Category: category, TransactionCount: categoryCounts[category.ID]})
		}
	}
	merchants := make([]MerchantProjection, 0)
	for _, merchant := range snapshot.Effective.Merchants {
		if merchant.Retired {
			continue
		}
		entry := MerchantProjection{Merchant: merchant, TransactionCount: merchantCounts[merchant.ID]}
		for _, total := range merchantTotals[merchant.ID] {
			entry.Totals = append(entry.Totals, total)
		}
		sort.Slice(entry.Totals, func(left, right int) bool {
			if entry.Totals[left].Currency != entry.Totals[right].Currency {
				return entry.Totals[left].Currency < entry.Totals[right].Currency
			}
			return entry.Totals[left].Scale < entry.Totals[right].Scale
		})
		merchants = append(merchants, entry)
	}
	sort.Slice(groups, func(left, right int) bool {
		return entityLess(groups[left].Group.Label, string(groups[left].Group.ID), groups[right].Group.Label, string(groups[right].Group.ID))
	})
	sort.Slice(categories, func(left, right int) bool {
		return entityLess(categories[left].Category.Label, string(categories[left].Category.ID), categories[right].Category.Label, string(categories[right].Category.ID))
	})
	sort.Slice(merchants, func(left, right int) bool {
		if merchants[left].TransactionCount != merchants[right].TransactionCount {
			return merchants[left].TransactionCount > merchants[right].TransactionCount
		}
		return entityLess(merchants[left].Merchant.Label, string(merchants[left].Merchant.ID), merchants[right].Merchant.Label, string(merchants[right].Merchant.ID))
	})
	return groups, categories, merchants
}

func catalogFromTransactions(state toolProjectionState, request CatalogWindowRequest) (CatalogProjection, error) {
	profile := domain.CommittedProfile{}
	groups := make(map[domain.EntityID]domain.CategoryGroup)
	categories := make(map[domain.EntityID]domain.Category)
	merchants := make(map[domain.EntityID]domain.Merchant)
	for _, transaction := range state.transactions {
		groupID := domain.EntityID(transaction.Category.GroupID)
		categoryID := domain.EntityID(transaction.Category.ID)
		merchantID := domain.EntityID(transaction.Merchant.ID)
		groupKey, _ := domain.CollisionKey(transaction.Category.Group)
		categoryKey, _ := domain.CollisionKey(transaction.Category.Name)
		merchantKey, _ := domain.CollisionKey(transaction.Merchant.Name)
		groups[groupID] = domain.CategoryGroup{ID: groupID, Label: transaction.Category.Group, CollisionKey: groupKey}
		categories[categoryID] = domain.Category{ID: categoryID, GroupID: groupID, Label: transaction.Category.Name, CollisionKey: categoryKey}
		merchants[merchantID] = domain.Merchant{ID: merchantID, Label: transaction.Merchant.Name, CollisionKey: merchantKey}
	}
	for _, value := range groups {
		profile.Groups = append(profile.Groups, value)
	}
	for _, value := range categories {
		profile.Categories = append(profile.Categories, value)
	}
	for _, value := range merchants {
		profile.Merchants = append(profile.Merchants, value)
	}
	snapshot := EffectiveSnapshot{Effective: profile}
	groupOffset, groupLimit, err := normalizeToolWindow(request.Groups.Offset, request.Groups.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	categoryOffset, categoryLimit, err := normalizeToolWindow(request.Categories.Offset, request.Categories.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	merchantOffset, merchantLimit, err := normalizeToolWindow(request.Merchants.Offset, request.Merchants.Limit)
	if err != nil {
		return CatalogProjection{}, newAppError(AppInvalidOperation, state.revision, err)
	}
	groupEntries, categoryEntries, merchantEntries := buildCatalogEntries(snapshot, state.transactions)
	return CatalogProjection{
		Revision:   state.revision,
		GroupTotal: len(groupEntries), GroupOffset: min(groupOffset, len(groupEntries)), Groups: windowGroups(groupEntries, groupOffset, groupLimit),
		CategoryTotal: len(categoryEntries), CategoryOffset: min(categoryOffset, len(categoryEntries)), Categories: windowCategories(categoryEntries, categoryOffset, categoryLimit),
		MerchantTotal: len(merchantEntries), MerchantOffset: min(merchantOffset, len(merchantEntries)), Merchants: windowMerchants(merchantEntries, merchantOffset, merchantLimit),
	}, nil
}

func entityLess(leftLabel, leftID, rightLabel, rightID string) bool {
	left, right := strings.ToLower(leftLabel), strings.ToLower(rightLabel)
	if left != right {
		return left < right
	}
	return leftID < rightID
}

func windowGroups(values []GroupProjection, offset, limit int) []GroupProjection {
	start := min(offset, len(values))
	return append([]GroupProjection(nil), values[start:min(start+limit, len(values))]...)
}

func windowCategories(values []CategoryProjection, offset, limit int) []CategoryProjection {
	start := min(offset, len(values))
	return append([]CategoryProjection(nil), values[start:min(start+limit, len(values))]...)
}

func windowMerchants(values []MerchantProjection, offset, limit int) []MerchantProjection {
	start := min(offset, len(values))
	result := append([]MerchantProjection(nil), values[start:min(start+limit, len(values))]...)
	for index := range result {
		result[index].Totals = append([]domain.Money(nil), result[index].Totals...)
	}
	return result
}
