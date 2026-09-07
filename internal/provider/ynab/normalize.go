package ynab

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

const (
	unknownPayeeID    = "moneyflow:ynab:unknown-payee"
	unknownPayeeLabel = "YNAB: Unknown Payee"
)

// Normalize converts one complete YNAB plan into the provider-neutral snapshot contract.
func Normalize(plan PlanDocument, observedAt time.Time) (domain.ImportSnapshot, error) {
	currency := domain.Currency(plan.CurrencyFormat.ISOCode)
	if !domain.IsValidCurrency(currency) || plan.CurrencyFormat.DecimalDigits < 0 ||
		plan.CurrencyFormat.DecimalDigits > 9 {
		return domain.ImportSnapshot{}, provider.NewError(provider.CodeMoneyMismatch)
	}
	scale := uint8(plan.CurrencyFormat.DecimalDigits)
	snapshot := domain.ImportSnapshot{ObservedAt: observedAt}

	accounts := make(map[string]Account)
	for _, account := range plan.Accounts {
		if err := validateAccount(account); err != nil {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if *account.Deleted {
			continue
		}
		if _, duplicate := accounts[account.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		accounts[account.ID] = account
		snapshot.Accounts = append(snapshot.Accounts, domain.ImportEntity{
			Kind: domain.EntityKindAccount, ExternalID: account.ID, Label: account.Name,
		})
	}

	payees := make(map[string]Payee)
	for _, payee := range plan.Payees {
		if validateID(payee.ID) != nil || validateLabel(payee.Name) != nil || payee.Deleted == nil {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if *payee.Deleted {
			continue
		}
		if _, duplicate := payees[payee.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		payees[payee.ID] = payee
		snapshot.Merchants = append(snapshot.Merchants, domain.ImportEntity{
			Kind: domain.EntityKindMerchant, ExternalID: payee.ID, Label: payee.Name,
		})
	}
	snapshot.Merchants = append(snapshot.Merchants, domain.ImportEntity{
		Kind: domain.EntityKindMerchant, ExternalID: unknownPayeeID, Label: unknownPayeeLabel,
	})

	groups := make(map[string]CategoryGroup)
	for _, group := range plan.CategoryGroups {
		if validateID(group.ID) != nil || validateLabel(group.Name) != nil ||
			group.Hidden == nil || group.Deleted == nil {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if *group.Deleted {
			continue
		}
		if _, duplicate := groups[group.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		groups[group.ID] = group
		snapshot.Groups = append(snapshot.Groups, domain.ImportEntity{
			Kind: domain.EntityKindGroup, ExternalID: group.ID, Label: group.Name,
		})
	}
	categories := make(map[string]Category)
	for _, category := range plan.Categories {
		if validateID(category.ID) != nil || validateID(category.CategoryGroupID) != nil ||
			validateLabel(category.Name) != nil || category.Hidden == nil || category.Deleted == nil {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if *category.Deleted {
			continue
		}
		if _, ok := groups[category.CategoryGroupID]; !ok {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if _, duplicate := categories[category.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		categories[category.ID] = category
		snapshot.Categories = append(snapshot.Categories, domain.ImportEntity{
			Kind: domain.EntityKindCategory, ExternalID: category.ID,
			ParentExternalID: category.CategoryGroupID, Label: category.Name,
		})
	}
	// Python retains category identities and names from transaction details even
	// when YNAB omits them from taxonomy. The group is unknown, not the category.
	retainCategory := func(id, label string) error {
		if id == "" {
			return nil
		}
		if category, exists := categories[id]; exists {
			if category.CategoryGroupID == "" && category.Name != label {
				return provider.NewDataInvalidError(provider.DataInvalidCategoryReference)
			}
			return nil
		}
		if validateID(id) != nil || validateLabel(label) != nil {
			return provider.NewDataInvalidError(provider.DataInvalidCategoryReference)
		}
		categories[id] = Category{ID: id, Name: label}
		snapshot.Categories = append(snapshot.Categories, domain.ImportEntity{
			Kind: domain.EntityKindCategory, ExternalID: id, Label: label,
		})
		return nil
	}
	for _, transaction := range plan.Transactions {
		if err := retainCategory(transaction.CategoryID, transaction.CategoryName); err != nil {
			return domain.ImportSnapshot{}, err
		}
	}
	for _, split := range plan.Subtransactions {
		if err := retainCategory(split.CategoryID, split.CategoryName); err != nil {
			return domain.ImportSnapshot{}, err
		}
	}

	transactions := make(map[string]Transaction)
	for _, transaction := range plan.Transactions {
		if err := validateTransactionWire(transaction); err != nil || *transaction.Deleted {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if _, duplicate := transactions[transaction.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if _, ok := accounts[transaction.AccountID]; !ok {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		transactions[transaction.ID] = transaction
	}

	splitsByParent := make(map[string][]Subtransaction)
	seenSplits := make(map[string]struct{})
	for _, split := range plan.Subtransactions {
		if validateID(split.ID) != nil || validateID(split.TransactionID) != nil ||
			validateMemo(split.Memo) != nil || split.Deleted == nil || *split.Deleted {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if _, ok := transactions[split.TransactionID]; !ok {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		if _, duplicate := seenSplits[split.ID]; duplicate {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		seenSplits[split.ID] = struct{}{}
		if err := validateOptionalReferences(
			split.PayeeID, split.CategoryID, split.TransferAccountID,
			payees, categories, accounts,
		); err != nil {
			return domain.ImportSnapshot{}, err
		}
		splitsByParent[split.TransactionID] = append(splitsByParent[split.TransactionID], split)
	}

	for _, transaction := range plan.Transactions {
		money, err := milliunitsToMoney(transaction.Amount, currency, scale)
		if err != nil {
			return domain.ImportSnapshot{}, err
		}
		date, err := domain.ParseDate(transaction.Date)
		if err != nil {
			return domain.ImportSnapshot{}, provider.NewDataInvalidError(provider.DataInvalidTransactionDate)
		}
		merchantID := transaction.PayeeID
		if merchantID == "" {
			merchantID = unknownPayeeID
		} else if _, ok := payees[merchantID]; !ok {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		categoryID := transaction.CategoryID
		if categoryID != "" {
			if _, ok := categories[categoryID]; !ok {
				return domain.ImportSnapshot{}, provider.NewDataInvalidError(provider.DataInvalidCategoryReference)
			}
		}
		if err = validateOptionalReferences(
			transaction.PayeeID, transaction.CategoryID, transaction.TransferAccountID,
			payees, categories, accounts,
		); err != nil {
			return domain.ImportSnapshot{}, err
		}
		cleared := transaction.Cleared
		if cleared != "cleared" && cleared != "uncleared" && cleared != "reconciled" {
			return domain.ImportSnapshot{}, invalidSnapshot()
		}
		imported := domain.ImportTransaction{
			ExternalID: transaction.ID, AccountExternalID: transaction.AccountID,
			MerchantExternalID: merchantID, CategoryExternalID: categoryID,
			Date: date, Amount: money, Notes: transaction.Memo,
			Pending: cleared == "uncleared",
			Hidden:  transaction.TransferAccountID != "" || !*accounts[transaction.AccountID].OnBudget,
		}
		children := splitsByParent[transaction.ID]
		if len(children) > 0 {
			imported.CategoryExternalID = ""
			imported.SystemCategoryID = domain.SplitCategoryID
			sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
			var total int64
			for position, split := range children {
				if (split.Amount > 0 && total > math.MaxInt64-split.Amount) ||
					(split.Amount < 0 && total < math.MinInt64-split.Amount) {
					return domain.ImportSnapshot{}, invalidSnapshot()
				}
				total += split.Amount
				splitMoney, moneyErr := milliunitsToMoney(split.Amount, currency, scale)
				if moneyErr != nil {
					return domain.ImportSnapshot{}, moneyErr
				}
				payeeLabel, categoryLabel := "", ""
				if split.PayeeID != "" {
					payeeLabel = payees[split.PayeeID].Name
				}
				if split.CategoryID != "" {
					categoryLabel = categories[split.CategoryID].Name
				}
				snapshot.Splits = append(snapshot.Splits, domain.ImportTransactionSplit{
					ExternalID: split.ID, ParentTransactionExternalID: transaction.ID,
					Position: position, SourceAmount: split.Amount, SourceScale: 3,
					Amount: splitMoney, Memo: split.Memo,
					PayeeExternalID: split.PayeeID, PayeeLabel: payeeLabel,
					CategoryExternalID: split.CategoryID, CategoryLabel: categoryLabel,
					TransferAccountExternalID:     split.TransferAccountID,
					TransferTransactionExternalID: split.TransferTransactionID,
				})
			}
			if total != transaction.Amount {
				return domain.ImportSnapshot{}, invalidSnapshot()
			}
		}
		snapshot.Transactions = append(snapshot.Transactions, imported)
	}
	sortImportSnapshot(&snapshot)
	if err := snapshot.Validate(); err != nil {
		return domain.ImportSnapshot{}, invalidSnapshot()
	}
	return snapshot, nil
}

func sortImportSnapshot(snapshot *domain.ImportSnapshot) {
	sort.Slice(snapshot.Accounts, func(i, j int) bool { return snapshot.Accounts[i].ExternalID < snapshot.Accounts[j].ExternalID })
	sort.Slice(snapshot.Merchants, func(i, j int) bool { return snapshot.Merchants[i].ExternalID < snapshot.Merchants[j].ExternalID })
	sort.Slice(snapshot.Groups, func(i, j int) bool { return snapshot.Groups[i].ExternalID < snapshot.Groups[j].ExternalID })
	sort.Slice(snapshot.Categories, func(i, j int) bool { return snapshot.Categories[i].ExternalID < snapshot.Categories[j].ExternalID })
	sort.Slice(snapshot.Transactions, func(i, j int) bool { return snapshot.Transactions[i].ExternalID < snapshot.Transactions[j].ExternalID })
	sort.Slice(snapshot.Splits, func(i, j int) bool {
		if snapshot.Splits[i].ParentTransactionExternalID != snapshot.Splits[j].ParentTransactionExternalID {
			return snapshot.Splits[i].ParentTransactionExternalID < snapshot.Splits[j].ParentTransactionExternalID
		}
		return snapshot.Splits[i].Position < snapshot.Splits[j].Position
	})
}

func validateAccount(account Account) error {
	if validateID(account.ID) != nil || validateLabel(account.Name) != nil || account.Type == "" ||
		account.OnBudget == nil || account.Closed == nil || account.Deleted == nil {
		return errors.New("invalid account")
	}
	return nil
}

func validateTransactionWire(transaction Transaction) error {
	if validateID(transaction.ID) != nil || validateID(transaction.AccountID) != nil ||
		validateMemo(transaction.Memo) != nil || transaction.Cleared == "" ||
		transaction.Approved == nil || transaction.Deleted == nil {
		return errors.New("invalid transaction")
	}
	return nil
}

func validateOptionalReferences(
	payeeID, categoryID, transferAccountID string,
	payees map[string]Payee,
	categories map[string]Category,
	accounts map[string]Account,
) error {
	if payeeID != "" {
		if validateID(payeeID) != nil {
			return invalidSnapshot()
		}
		if _, ok := payees[payeeID]; !ok {
			return invalidSnapshot()
		}
	}
	if categoryID != "" {
		if validateID(categoryID) != nil {
			return invalidSnapshot()
		}
		if _, ok := categories[categoryID]; !ok {
			return provider.NewDataInvalidError(provider.DataInvalidCategoryReference)
		}
	}
	if transferAccountID != "" {
		if validateID(transferAccountID) != nil {
			return invalidSnapshot()
		}
		if _, ok := accounts[transferAccountID]; !ok {
			return invalidSnapshot()
		}
	}
	return nil
}

func validateID(value string) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > 256 {
		return errors.New("invalid identity")
	}
	return nil
}

func validateLabel(value string) error {
	if !utf8.ValidString(value) || len(value) > 1024 {
		return errors.New("invalid label")
	}
	normalized, err := domain.NormalizeDisplayLabel(value)
	if err != nil || normalized != value {
		return errors.New("invalid label")
	}
	return nil
}

func validateMemo(value string) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 500 {
		return errors.New("invalid memo")
	}
	return nil
}

func invalidSnapshot() error { return provider.NewDataInvalidError(provider.DataInvalidSnapshot) }
