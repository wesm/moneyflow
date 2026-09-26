package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
)

type amazonReconcileResult struct {
	Items                       []store.AmazonOrderItem
	Inserted, Updated, Restored int
	Retired, Unchanged          int
}

type amazonPairing struct {
	existing int
	incoming int
	restored bool
}

func reconcileAmazonRows(
	existing []store.AmazonOrderItem,
	rows []amazon.Row,
	observedOrderIDs []string,
	proposed store.ProposedAmazonIDs,
) (amazonReconcileResult, error) {
	observed := make(map[string]struct{}, len(observedOrderIDs))
	for _, orderID := range observedOrderIDs {
		observed[orderID] = struct{}{}
	}
	existingByOrder := make(map[string][]int)
	for index := range existing {
		existingByOrder[existing[index].OrderID] = append(existingByOrder[existing[index].OrderID], index)
	}
	incomingByOrder := make(map[string][]int)
	for index := range rows {
		incomingByOrder[rows[index].OrderID] = append(incomingByOrder[rows[index].OrderID], index)
	}

	result := amazonReconcileResult{Items: append([]store.AmazonOrderItem(nil), existing...)}
	transactionIndex, sourceIndex := 0, 0
	orders := append([]string(nil), observedOrderIDs...)
	slices.Sort(orders)
	orders = slices.Compact(orders)
	for _, orderID := range orders {
		pairs, remainingExisting, remainingIncoming := pairAmazonOrder(
			existing, rows, existingByOrder[orderID], incomingByOrder[orderID],
		)
		for _, pair := range pairs {
			previous := result.Items[pair.existing]
			updated := amazonItemFromRow(previous, rows[pair.incoming])
			updated.Retired = false
			result.Items[pair.existing] = updated
			switch {
			case pair.restored:
				result.Restored++
			case previous.FullFingerprint != updated.FullFingerprint:
				result.Updated++
			default:
				result.Unchanged++
			}
		}
		for _, index := range remainingExisting {
			if result.Items[index].Retired {
				continue
			}
			result.Items[index].Retired = true
			result.Retired++
		}
		for _, index := range remainingIncoming {
			if transactionIndex >= len(proposed.TransactionIDs) || sourceIndex >= len(proposed.SourceIdentities) {
				return amazonReconcileResult{}, errors.New("reconcile Amazon rows: proposed identities exhausted")
			}
			item := amazonItemFromRow(store.AmazonOrderItem{
				LocalTransactionID: proposed.TransactionIDs[transactionIndex],
				SourceIdentity:     proposed.SourceIdentities[sourceIndex],
			}, rows[index])
			result.Items = append(result.Items, item)
			transactionIndex++
			sourceIndex++
			result.Inserted++
		}
	}
	for _, item := range result.Items {
		if _, wasObserved := observed[item.OrderID]; !wasObserved && !item.Retired {
			result.Unchanged++
		}
	}
	slices.SortFunc(result.Items, func(left, right store.AmazonOrderItem) int {
		return strings.Compare(left.SourceIdentity, right.SourceIdentity)
	})
	return result, nil
}

func pairAmazonOrder(
	existing []store.AmazonOrderItem,
	rows []amazon.Row,
	existingIndexes, incomingIndexes []int,
) ([]amazonPairing, []int, []int) {
	active := make(map[int]struct{})
	retired := make(map[int]struct{})
	incoming := make(map[int]struct{})
	for _, index := range existingIndexes {
		if existing[index].Retired {
			retired[index] = struct{}{}
		} else {
			active[index] = struct{}{}
		}
	}
	for _, index := range incomingIndexes {
		incoming[index] = struct{}{}
	}
	pairs := make([]amazonPairing, 0)
	pairExactAmazon(&pairs, active, incoming, existing, rows, false)
	pairExactAmazon(&pairs, retired, incoming, existing, rows, true)
	pairAmazonASINSingletons(&pairs, active, incoming, existing, rows)
	pairAmazonASINLessSingleton(&pairs, active, incoming, existing, rows)
	return pairs, sortedAmazonIndexes(active), sortedAmazonIncoming(incoming, rows)
}

func pairExactAmazon(
	pairs *[]amazonPairing,
	existingSet, incomingSet map[int]struct{},
	existing []store.AmazonOrderItem,
	rows []amazon.Row,
	restored bool,
) {
	exactExisting := make(map[string][]int)
	exactIncoming := make(map[string][]int)
	for index := range existingSet {
		exactExisting[existing[index].IdentityFingerprint] = append(exactExisting[existing[index].IdentityFingerprint], index)
	}
	for index := range incomingSet {
		exactIncoming[rows[index].IdentityFingerprint] = append(exactIncoming[rows[index].IdentityFingerprint], index)
	}
	keys := make([]string, 0, len(exactIncoming))
	for key := range exactIncoming {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		left, right := exactExisting[key], exactIncoming[key]
		slices.SortFunc(left, func(a, b int) int {
			return strings.Compare(string(existing[a].LocalTransactionID), string(existing[b].LocalTransactionID))
		})
		slices.SortFunc(right, func(a, b int) int { return compareAmazonIncoming(rows[a], rows[b]) })
		count := min(len(left), len(right))
		for index := 0; index < count; index++ {
			*pairs = append(*pairs, amazonPairing{existing: left[index], incoming: right[index], restored: restored})
			delete(existingSet, left[index])
			delete(incomingSet, right[index])
		}
	}
}

func pairAmazonASINSingletons(
	pairs *[]amazonPairing,
	active, incoming map[int]struct{},
	existing []store.AmazonOrderItem,
	rows []amazon.Row,
) {
	existingByASIN := make(map[string][]int)
	incomingByASIN := make(map[string][]int)
	for index := range active {
		if existing[index].ASIN != "" {
			existingByASIN[existing[index].ASIN] = append(existingByASIN[existing[index].ASIN], index)
		}
	}
	for index := range incoming {
		if rows[index].ASIN != "" {
			incomingByASIN[rows[index].ASIN] = append(incomingByASIN[rows[index].ASIN], index)
		}
	}
	keys := make([]string, 0, len(incomingByASIN))
	for key := range incomingByASIN {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		left, right := existingByASIN[key], incomingByASIN[key]
		if len(left) == 1 && len(right) == 1 {
			*pairs = append(*pairs, amazonPairing{existing: left[0], incoming: right[0]})
			delete(active, left[0])
			delete(incoming, right[0])
		}
	}
}

func pairAmazonASINLessSingleton(
	pairs *[]amazonPairing,
	active, incoming map[int]struct{},
	existing []store.AmazonOrderItem,
	rows []amazon.Row,
) {
	left := make([]int, 0)
	right := make([]int, 0)
	for index := range active {
		if existing[index].ASIN == "" {
			left = append(left, index)
		}
	}
	for index := range incoming {
		if rows[index].ASIN == "" {
			right = append(right, index)
		}
	}
	if len(left) == 1 && len(right) == 1 {
		*pairs = append(*pairs, amazonPairing{existing: left[0], incoming: right[0]})
		delete(active, left[0])
		delete(incoming, right[0])
	}
}

func amazonItemFromRow(item store.AmazonOrderItem, row amazon.Row) store.AmazonOrderItem {
	item.OrderID = row.OrderID
	item.ASIN = row.ASIN
	item.ASINLessKey = row.ASINLessKey
	item.ProductName = row.ProductName
	item.OrderDate = row.OrderDate
	item.Quantity = row.Quantity
	item.AmountMinor = row.AmountMinor
	item.UnitPriceMinor = row.UnitPriceMinor
	item.Currency = row.Currency
	item.Scale = row.Scale
	item.OrderStatus = row.OrderStatus
	item.ShipmentStatus = row.ShipmentStatus
	item.IdentityFingerprint = row.IdentityFingerprint
	item.FullFingerprint = row.FullFingerprint
	item.Retired = false
	return item
}

func sortedAmazonIndexes(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func sortedAmazonIncoming(values map[int]struct{}, rows []amazon.Row) []int {
	result := sortedAmazonIndexes(values)
	slices.SortFunc(result, func(left, right int) int { return compareAmazonIncoming(rows[left], rows[right]) })
	return result
}

func compareAmazonIncoming(left, right amazon.Row) int {
	if comparison := strings.Compare(left.RelativeFilename, right.RelativeFilename); comparison != 0 {
		return comparison
	}
	return left.Record - right.Record
}
