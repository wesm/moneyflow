package monarch

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

const snapshotRetryDelay = 25 * time.Millisecond

var _ provider.Reader = (*Client)(nil)

// ProbeIdentity returns the household-scoped subscription identity for profile binding.
func (client *Client) ProbeIdentity(ctx context.Context) (provider.ProfileIdentity, error) {
	subscription, err := client.GetSubscriptionDetails(ctx)
	if err != nil {
		return provider.ProfileIdentity{}, err
	}
	if !validProviderText(subscription.ID) {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeDataInvalid)
	}
	return provider.ProfileIdentity{Kind: providerKind, RemoteID: subscription.ID}, nil
}

// FetchSnapshot reads and validates one complete posted-transaction snapshot.
func (client *Client) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	if !validCurrency(client.options.ImportCurrency) || client.options.ImportScale > 9 {
		return domain.ImportSnapshot{}, provider.NewError(provider.CodeDataInvalid)
	}
	for attempt := 1; attempt <= maxSnapshotAttempts; attempt++ {
		snapshot, err := client.fetchSnapshotAttempt(ctx, attempt, progress)
		if err == nil {
			return snapshot, nil
		}
		if !errors.Is(err, errSnapshotChanged) {
			return domain.ImportSnapshot{}, err
		}
		if attempt == maxSnapshotAttempts {
			break
		}
		if err := client.options.Sleep(ctx, snapshotRetryDelay*time.Duration(attempt)); err != nil {
			return domain.ImportSnapshot{}, err
		}
	}
	return domain.ImportSnapshot{}, provider.NewError(provider.CodeSnapshotUnstable)
}

func (client *Client) fetchSnapshotAttempt(
	ctx context.Context,
	attempt int,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	accounts, err := client.GetAccounts(ctx)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	merchants, err := client.GetMerchants(ctx)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	groups, err := client.GetCategoryGroups(ctx)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	categories, err := client.GetCategories(ctx)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	visible, err := client.fetchTransactionPartition(ctx, false, attempt, progress)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	hidden, err := client.fetchTransactionPartition(ctx, true, attempt, progress)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	if partitionsOverlap(visible.rows, hidden.rows) {
		return domain.ImportSnapshot{}, errSnapshotChanged
	}
	if err := client.verifyPartitionCount(ctx, false, visible.total); err != nil {
		return domain.ImportSnapshot{}, err
	}
	if err := client.verifyPartitionCount(ctx, true, hidden.total); err != nil {
		return domain.ImportSnapshot{}, err
	}
	allTransactions := append(
		append(make([]Transaction, 0, len(visible.rows)+len(hidden.rows)), visible.rows...),
		hidden.rows...,
	)
	snapshot, err := client.normalizeSnapshot(accounts, merchants, groups, categories, allTransactions)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	return snapshot, nil
}

func (client *Client) normalizeSnapshot(
	accounts []Account,
	merchants []Merchant,
	groups []CategoryGroup,
	categories []Category,
	transactions []Transaction,
) (domain.ImportSnapshot, error) {
	accountEntities, accountIDs, err := normalizeAccounts(accounts)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	merchantEntities, merchantIDs, err := normalizeMerchants(merchants)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	groupEntities, groupIDs, err := normalizeGroups(groups)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	categoryEntities, categoryIDs, err := normalizeCategories(categories, groupIDs)
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	importedTransactions := make([]domain.ImportTransaction, 0, len(transactions))
	for _, transaction := range transactions {
		imported, err := client.normalizeTransaction(transaction)
		if err != nil {
			return domain.ImportSnapshot{}, err
		}
		if _, ok := accountIDs[imported.AccountExternalID]; !ok {
			return domain.ImportSnapshot{}, errSnapshotChanged
		}
		if _, ok := merchantIDs[imported.MerchantExternalID]; !ok {
			return domain.ImportSnapshot{}, errSnapshotChanged
		}
		if _, ok := categoryIDs[imported.CategoryExternalID]; !ok {
			return domain.ImportSnapshot{}, errSnapshotChanged
		}
		// Pending rows participate in integrity and relationship validation, but never persist.
		if !imported.Pending {
			importedTransactions = append(importedTransactions, imported)
		}
	}
	snapshot := domain.ImportSnapshot{
		Accounts: accountEntities, Merchants: merchantEntities,
		Groups: groupEntities, Categories: categoryEntities,
		Transactions: importedTransactions, ObservedAt: client.options.Now().UTC(),
	}
	sortSnapshot(&snapshot)
	if err := snapshot.Validate(); err != nil {
		return domain.ImportSnapshot{}, provider.NewError(provider.CodeDataInvalid)
	}
	return snapshot, nil
}

func normalizeAccounts(accounts []Account) ([]domain.ImportEntity, map[string]struct{}, error) {
	entities := make([]domain.ImportEntity, 0, len(accounts))
	for _, account := range accounts {
		entities = append(entities, domain.ImportEntity{
			Kind: domain.EntityKindAccount, ExternalID: account.ID, Label: account.DisplayName,
		})
	}
	return validateEntities(entities)
}

func normalizeMerchants(merchants []Merchant) ([]domain.ImportEntity, map[string]struct{}, error) {
	entities := make([]domain.ImportEntity, 0, len(merchants))
	for _, merchant := range merchants {
		entities = append(entities, domain.ImportEntity{
			Kind: domain.EntityKindMerchant, ExternalID: merchant.ID, Label: merchant.Name,
		})
	}
	return validateEntities(entities)
}

func normalizeGroups(groups []CategoryGroup) ([]domain.ImportEntity, map[string]struct{}, error) {
	entities := make([]domain.ImportEntity, 0, len(groups))
	for _, group := range groups {
		entities = append(entities, domain.ImportEntity{
			Kind: domain.EntityKindGroup, ExternalID: group.ID, Label: group.Name,
		})
	}
	return validateEntities(entities)
}

func normalizeCategories(
	categories []Category,
	groupIDs map[string]struct{},
) ([]domain.ImportEntity, map[string]struct{}, error) {
	entities := make([]domain.ImportEntity, 0, len(categories))
	for _, category := range categories {
		if validProviderText(category.Group.ID) {
			if _, ok := groupIDs[category.Group.ID]; !ok {
				return nil, nil, errSnapshotChanged
			}
		}
		entities = append(entities, domain.ImportEntity{
			Kind: domain.EntityKindCategory, ExternalID: category.ID,
			Label: category.Name, ParentExternalID: category.Group.ID,
		})
	}
	return validateEntities(entities)
}

func validateEntities(
	entities []domain.ImportEntity,
) ([]domain.ImportEntity, map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		if !validProviderText(entity.ExternalID) || !validProviderText(entity.Label) ||
			(entity.Kind == domain.EntityKindCategory && !validProviderText(entity.ParentExternalID)) {
			return nil, nil, provider.NewError(provider.CodeDataInvalid)
		}
		if _, duplicate := ids[entity.ExternalID]; duplicate {
			return nil, nil, provider.NewError(provider.CodeDataInvalid)
		}
		ids[entity.ExternalID] = struct{}{}
	}
	return entities, ids, nil
}

func (client *Client) normalizeTransaction(
	transaction Transaction,
) (domain.ImportTransaction, error) {
	if !validProviderText(transaction.ID) || !validProviderText(transaction.Account.ID) ||
		!validProviderText(transaction.Merchant.ID) || !validProviderText(transaction.Category.ID) {
		return domain.ImportTransaction{}, provider.NewError(provider.CodeDataInvalid)
	}
	date, err := domain.ParseDate(transaction.Date)
	if err != nil {
		return domain.ImportTransaction{}, provider.NewError(provider.CodeDataInvalid)
	}
	amount, err := decodeMoney(
		transaction.Amount,
		client.options.ImportCurrency,
		client.options.ImportScale,
	)
	if err != nil {
		return domain.ImportTransaction{}, err
	}
	return domain.ImportTransaction{
		ExternalID: transaction.ID, AccountExternalID: transaction.Account.ID,
		MerchantExternalID: transaction.Merchant.ID, CategoryExternalID: transaction.Category.ID,
		Date: date, Amount: amount, Notes: transaction.Notes,
		Hidden: transaction.HideFromReports, Pending: transaction.Pending,
	}, nil
}

func partitionsOverlap(left []Transaction, right []Transaction) bool {
	ids := make(map[string]struct{}, len(left))
	for _, transaction := range left {
		ids[transaction.ID] = struct{}{}
	}
	for _, transaction := range right {
		if _, duplicate := ids[transaction.ID]; duplicate {
			return true
		}
	}
	return false
}

func sortSnapshot(snapshot *domain.ImportSnapshot) {
	sortEntities := func(entities []domain.ImportEntity) {
		sort.Slice(entities, func(left int, right int) bool {
			return entities[left].ExternalID < entities[right].ExternalID
		})
	}
	sortEntities(snapshot.Accounts)
	sortEntities(snapshot.Merchants)
	sortEntities(snapshot.Groups)
	sortEntities(snapshot.Categories)
	sort.Slice(snapshot.Transactions, func(left int, right int) bool {
		return snapshot.Transactions[left].ExternalID < snapshot.Transactions[right].ExternalID
	})
}

func validProviderText(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func validCurrency(currency domain.Currency) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}
