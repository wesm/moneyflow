package analytics

import (
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

const amazonMatchWindowDays = 7

// AmazonMatchClass identifies the first global pass that produced results.
type AmazonMatchClass string

// Amazon matching passes in precedence order.
const (
	AmazonMatchExactOrder AmazonMatchClass = "exact_order"
	AmazonMatchFuzzyOrder AmazonMatchClass = "fuzzy_order"
	AmazonMatchExactItem  AmazonMatchClass = "exact_item"
)

// AmazonMatchConfidence is the renderer-neutral Python-compatible confidence label.
type AmazonMatchConfidence string

// Amazon match confidence values.
const (
	AmazonConfidenceHigh   AmazonMatchConfidence = "high"
	AmazonConfidenceMedium AmazonMatchConfidence = "medium"
	AmazonConfidenceLikely AmazonMatchConfidence = "likely"
)

// AmazonMatchItem is one committed Amazon source item used by the pure matcher.
type AmazonMatchItem struct {
	LocalTransactionID domain.EntityID
	OrderID            string
	ProductName        string
	Date               domain.Date
	AmountMinor        int64
	ASIN               string
	Quantity           int64
	OrderStatus        string
	ShipmentStatus     string
	UnitPriceMinor     *int64
}

// AmazonMatchSource is one immutable compatible profile snapshot.
type AmazonMatchSource struct {
	ProfileID string
	Revision  uint64
	Currency  domain.Currency
	Scale     uint8
	Items     []AmazonMatchItem
}

// AmazonMatch is one deterministic order-level result.
type AmazonMatch struct {
	Class                 AmazonMatchClass
	Confidence            AmazonMatchConfidence
	ProfileID             string
	SourceRevision        uint64
	OrderID               string
	OrderDate             domain.Date
	OrderTotal            domain.Money
	DateDistanceDays      int
	AmountDifferenceMinor int64
	FirstProduct          string
	Items                 []AmazonMatchItem
}

// AmazonMatchResult is one bounded window plus its authoritative total.
type AmazonMatchResult struct {
	Matches []AmazonMatch
	Total   int
}

type amazonOrderCandidate struct {
	profileID string
	revision  uint64
	currency  domain.Currency
	scale     uint8
	orderID   string
	date      domain.Date
	total     int64
	items     []AmazonMatchItem
}

// MatchAmazonOrders runs the three global Python-compatible passes over immutable sources.
func MatchAmazonOrders(
	transaction domain.Transaction,
	sources []AmazonMatchSource,
	limit int,
) (AmazonMatchResult, error) {
	if limit < 1 {
		return AmazonMatchResult{}, errors.New("match Amazon orders: limit must be positive")
	}
	if transaction.Amount.Minor >= 0 {
		return AmazonMatchResult{}, nil
	}
	orders, err := buildAmazonOrderCandidates(transaction.Amount, sources)
	if err != nil {
		return AmazonMatchResult{}, err
	}
	passes := []AmazonMatchClass{AmazonMatchExactOrder, AmazonMatchFuzzyOrder, AmazonMatchExactItem}
	for _, class := range passes {
		matches := make([]AmazonMatch, 0)
		for _, order := range orders {
			if class == AmazonMatchExactItem {
				itemMatches, itemErr := matchAmazonItems(transaction, order)
				if itemErr != nil {
					return AmazonMatchResult{}, itemErr
				}
				matches = append(matches, itemMatches...)
				continue
			}
			match, ok, matchErr := matchAmazonOrder(transaction, order, class)
			if matchErr != nil {
				return AmazonMatchResult{}, matchErr
			}
			if ok {
				matches = append(matches, match)
			}
		}
		if len(matches) == 0 {
			continue
		}
		slices.SortFunc(matches, compareAmazonMatches)
		result := AmazonMatchResult{Total: len(matches)}
		result.Matches = append([]AmazonMatch(nil), matches[:min(limit, len(matches))]...)
		return result, nil
	}
	return AmazonMatchResult{}, nil
}

func buildAmazonOrderCandidates(
	money domain.Money,
	sources []AmazonMatchSource,
) ([]amazonOrderCandidate, error) {
	orders := make([]amazonOrderCandidate, 0)
	for _, source := range sources {
		if source.Currency != money.Currency || source.Scale != money.Scale {
			continue
		}
		byOrder := make(map[string][]AmazonMatchItem)
		for _, item := range source.Items {
			if item.OrderID != "" {
				byOrder[item.OrderID] = append(byOrder[item.OrderID], item)
			}
		}
		orderIDs := make([]string, 0, len(byOrder))
		for orderID := range byOrder {
			orderIDs = append(orderIDs, orderID)
		}
		slices.Sort(orderIDs)
		for _, orderID := range orderIDs {
			items := append([]AmazonMatchItem(nil), byOrder[orderID]...)
			slices.SortFunc(items, func(left, right AmazonMatchItem) int {
				return strings.Compare(string(left.LocalTransactionID), string(right.LocalTransactionID))
			})
			date := items[0].Date
			var total int64
			for _, item := range items {
				if (item.AmountMinor > 0 && total > math.MaxInt64-item.AmountMinor) ||
					(item.AmountMinor < 0 && total < math.MinInt64-item.AmountMinor) {
					return nil, errors.New("match Amazon orders: order total overflow")
				}
				total += item.AmountMinor
				if item.Date.Compare(date) < 0 {
					date = item.Date
				}
			}
			orders = append(orders, amazonOrderCandidate{
				profileID: source.ProfileID, revision: source.Revision,
				currency: source.Currency, scale: source.Scale, orderID: orderID,
				date: date, total: total, items: items,
			})
		}
	}
	return orders, nil
}

func matchAmazonOrder(
	transaction domain.Transaction,
	order amazonOrderCandidate,
	class AmazonMatchClass,
) (AmazonMatch, bool, error) {
	days := amazonDateDistance(transaction.Date, order.date)
	if days > amazonMatchWindowDays {
		return AmazonMatch{}, false, nil
	}
	orderDifference, err := absoluteDifference(transaction.Amount.Minor, order.total)
	if err != nil {
		return AmazonMatch{}, false, err
	}
	tolerance, err := amazonExactTolerance(order.scale)
	if err != nil {
		return AmazonMatch{}, false, err
	}
	amountDifference := orderDifference
	matched := false
	switch class {
	case AmazonMatchExactOrder:
		matched = orderDifference <= tolerance
	case AmazonMatchFuzzyOrder:
		matched, err = amazonFuzzyMatch(transaction.Amount.Minor, order.total, order.scale)
	default:
		return AmazonMatch{}, false, errors.New("match Amazon orders: unknown pass")
	}
	if err != nil || !matched {
		return AmazonMatch{}, false, err
	}
	confidence := AmazonConfidenceLikely
	switch class {
	case AmazonMatchExactOrder:
		confidence = AmazonConfidenceMedium
		cent, centErr := amazonCentThreshold(order.scale)
		if centErr != nil {
			return AmazonMatch{}, false, centErr
		}
		if days <= 2 && amountDifference < cent {
			confidence = AmazonConfidenceHigh
		}
	}
	firstProduct := ""
	if len(order.items) > 0 {
		firstProduct = order.items[0].ProductName
	}
	return AmazonMatch{
		Class: class, Confidence: confidence, ProfileID: order.profileID,
		SourceRevision: order.revision, OrderID: order.orderID, OrderDate: order.date,
		OrderTotal:       domain.Money{Minor: order.total, Currency: order.currency, Scale: order.scale},
		DateDistanceDays: days, AmountDifferenceMinor: amountDifference,
		FirstProduct: firstProduct, Items: append([]AmazonMatchItem(nil), order.items...),
	}, true, nil
}

func matchAmazonItems(transaction domain.Transaction, order amazonOrderCandidate) ([]AmazonMatch, error) {
	tolerance, err := amazonExactTolerance(order.scale)
	if err != nil {
		return nil, err
	}
	matches := make([]AmazonMatch, 0)
	for _, item := range order.items {
		days := amazonDateDistance(transaction.Date, item.Date)
		if days > amazonMatchWindowDays {
			continue
		}
		difference, differenceErr := absoluteDifference(transaction.Amount.Minor, item.AmountMinor)
		if differenceErr != nil {
			return nil, differenceErr
		}
		if difference > tolerance {
			continue
		}
		confidence := AmazonConfidenceMedium
		if days <= 2 {
			confidence = AmazonConfidenceHigh
		}
		matches = append(matches, AmazonMatch{
			Class: AmazonMatchExactItem, Confidence: confidence, ProfileID: order.profileID,
			SourceRevision: order.revision, OrderID: order.orderID, OrderDate: item.Date,
			OrderTotal:       domain.Money{Minor: item.AmountMinor, Currency: order.currency, Scale: order.scale},
			DateDistanceDays: days, AmountDifferenceMinor: difference,
			FirstProduct: item.ProductName, Items: []AmazonMatchItem{item},
		})
	}
	return matches, nil
}

func amazonFuzzyMatch(transaction, order int64, scale uint8) (bool, error) {
	if transaction >= 0 || order >= 0 {
		return false, nil
	}
	transactionMagnitude, err := absoluteMinor(transaction)
	if err != nil {
		return false, err
	}
	orderMagnitude, err := absoluteMinor(order)
	if err != nil {
		return false, err
	}
	if transactionMagnitude >= orderMagnitude {
		return false, nil
	}
	difference := orderMagnitude - transactionMagnitude
	floor, err := checkedPower10(scale)
	if err != nil || floor > math.MaxInt64/15 {
		return false, errors.New("match Amazon orders: fuzzy threshold overflow")
	}
	floor *= 15
	if difference <= floor {
		return true, nil
	}
	return difference <= orderMagnitude/10, nil
}

func amazonExactTolerance(scale uint8) (int64, error) {
	if scale < 2 {
		return 0, nil
	}
	value, err := checkedPower10(scale - 2)
	if err != nil || value > math.MaxInt64/2 {
		return 0, errors.New("match Amazon orders: exact tolerance overflow")
	}
	return value * 2, nil
}

func amazonCentThreshold(scale uint8) (int64, error) {
	if scale <= 2 {
		return 1, nil
	}
	return checkedPower10(scale - 2)
}

func checkedPower10(scale uint8) (int64, error) {
	value := int64(1)
	for range scale {
		if value > math.MaxInt64/10 {
			return 0, errors.New("match Amazon orders: scale overflow")
		}
		value *= 10
	}
	return value, nil
}

func absoluteDifference(left, right int64) (int64, error) {
	if (right > 0 && left < math.MinInt64+right) ||
		(right < 0 && left > math.MaxInt64+right) {
		return 0, errors.New("match Amazon orders: money difference overflow")
	}
	return absoluteMinor(left - right)
}

func absoluteMinor(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, errors.New("match Amazon orders: money magnitude overflow")
	}
	if value < 0 {
		return -value, nil
	}
	return value, nil
}

func amazonDateDistance(left, right domain.Date) int {
	leftTime := time.Date(left.Year(), left.Month(), left.Day(), 0, 0, 0, 0, time.UTC)
	rightTime := time.Date(right.Year(), right.Month(), right.Day(), 0, 0, 0, 0, time.UTC)
	days := int(leftTime.Sub(rightTime).Hours() / 24)
	if days < 0 {
		return -days
	}
	return days
}

func compareAmazonMatches(left, right AmazonMatch) int {
	if left.DateDistanceDays != right.DateDistanceDays {
		return left.DateDistanceDays - right.DateDistanceDays
	}
	if left.AmountDifferenceMinor < right.AmountDifferenceMinor {
		return -1
	}
	if left.AmountDifferenceMinor > right.AmountDifferenceMinor {
		return 1
	}
	if comparison := strings.Compare(left.ProfileID, right.ProfileID); comparison != 0 {
		return comparison
	}
	if comparison := strings.Compare(left.OrderID, right.OrderID); comparison != 0 {
		return comparison
	}
	leftID, rightID := "", ""
	if len(left.Items) > 0 {
		leftID = string(left.Items[0].LocalTransactionID)
	}
	if len(right.Items) > 0 {
		rightID = string(right.Items[0].LocalTransactionID)
	}
	return strings.Compare(leftID, rightID)
}
