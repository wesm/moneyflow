package analytics

import (
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// DuplicateGroup is one deterministic group of likely duplicate transactions.
type DuplicateGroup struct {
	Date           domain.Date
	Amount         domain.Money
	MatchingLabel  string
	AccountLabel   string
	TransactionIDs []domain.EntityID
	matchingKey    string
}

type duplicateKey struct {
	date          string
	currency      domain.Currency
	scale         uint8
	minor         int64
	matchingLabel string
	accountLabel  string
}

type duplicateAccumulator struct {
	date           domain.Date
	amount         domain.Money
	matchingLabel  string
	transactionIDs []domain.EntityID
}

// FindDuplicates groups transactions using the exact matching behavior exposed by Python v1.
// It deliberately uses Unicode lowercasing without trimming, normalization, case folding, fuzzy
// matching, or date tolerance.
func FindDuplicates(
	transactions []domain.Transaction,
	matchingMerchantLabels map[domain.EntityID]string,
) []DuplicateGroup {
	lower := cases.Lower(language.Und)
	accumulators := make(map[duplicateKey]*duplicateAccumulator, len(transactions)/2)
	for _, transaction := range transactions {
		label := transaction.Merchant.Name
		if providerLabel, ok := matchingMerchantLabels[domain.EntityID(transaction.Merchant.ID)]; ok {
			label = providerLabel
		}
		key := duplicateKey{
			date:          transaction.Date.String(),
			currency:      transaction.Amount.Currency,
			scale:         transaction.Amount.Scale,
			minor:         transaction.Amount.Minor,
			matchingLabel: lower.String(label),
			accountLabel:  transaction.Account.Name,
		}
		accumulator, ok := accumulators[key]
		if !ok {
			accumulator = &duplicateAccumulator{
				date: transaction.Date, amount: transaction.Amount, matchingLabel: label,
			}
			accumulators[key] = accumulator
		} else if label < accumulator.matchingLabel {
			accumulator.matchingLabel = label
		}
		accumulator.transactionIDs = append(accumulator.transactionIDs, domain.EntityID(transaction.ID))
	}

	groups := make([]DuplicateGroup, 0, len(accumulators))
	for key, accumulator := range accumulators {
		if len(accumulator.transactionIDs) < 2 {
			continue
		}
		slices.Sort(accumulator.transactionIDs)
		groups = append(groups, DuplicateGroup{
			Date:           accumulator.date,
			Amount:         accumulator.amount,
			MatchingLabel:  accumulator.matchingLabel,
			AccountLabel:   key.accountLabel,
			TransactionIDs: accumulator.transactionIDs,
			matchingKey:    key.matchingLabel,
		})
	}
	slices.SortFunc(groups, compareDuplicateGroups)
	return groups
}

func compareDuplicateGroups(left, right DuplicateGroup) int {
	if comparison := right.Date.Compare(left.Date); comparison != 0 {
		return comparison
	}
	if comparison := strings.Compare(string(left.Amount.Currency), string(right.Amount.Currency)); comparison != 0 {
		return comparison
	}
	if left.Amount.Scale != right.Amount.Scale {
		return compareOrdered(left.Amount.Scale, right.Amount.Scale)
	}
	if left.Amount.Minor != right.Amount.Minor {
		return compareOrdered(left.Amount.Minor, right.Amount.Minor)
	}
	if comparison := strings.Compare(left.matchingKey, right.matchingKey); comparison != 0 {
		return comparison
	}
	if comparison := strings.Compare(left.AccountLabel, right.AccountLabel); comparison != 0 {
		return comparison
	}
	return strings.Compare(string(left.TransactionIDs[0]), string(right.TransactionIDs[0]))
}

func compareOrdered[T ~int64 | ~uint8](left, right T) int {
	if left < right {
		return -1
	}
	return 1
}
