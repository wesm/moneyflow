package app

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	defaultTransactionInfoLimit = 20
	maxTransactionInfoLimit     = 100
)

// TransactionInfoRequest identifies one stable transaction and bounded match windows.
type TransactionInfoRequest struct {
	ExpectedRevision uint64
	TransactionID    string
	MatchOffset      int
	MatchLimit       int
	ItemOffset       int
	ItemLimit        int
}

// AmazonOrderItemInfo is the user-visible committed source detail for one Amazon row.
type AmazonOrderItemInfo struct {
	OrderID        string
	ProductName    string
	ASIN           string
	Quantity       int64
	OrderStatus    string
	ShipmentStatus string
	UnitPrice      *domain.Money
}

// TransactionInfoMatch is one bounded matching order and item window.
type TransactionInfoMatch struct {
	Class                 analytics.AmazonMatchClass
	Confidence            analytics.AmazonMatchConfidence
	ProfileID             string
	OrderID               string
	OrderDate             domain.Date
	OrderTotal            domain.Money
	DateDistanceDays      int
	AmountDifferenceMinor int64
	FirstProduct          string
	TotalItems            int
	Items                 []AmazonOrderItemInfo
}

// TransactionInfo is one bounded, renderer-neutral individual-transaction projection.
type TransactionInfo struct {
	Revision        uint64
	Transaction     domain.Transaction
	AmazonQualified bool
	AmazonItem      *AmazonOrderItemInfo
	TotalMatches    int
	MatchOffset     int
	MatchLimit      int
	ItemOffset      int
	ItemLimit       int
	Matches         []TransactionInfoMatch
}

// TransactionInfo returns committed source facts or cross-profile Amazon matches.
func (service *Service) TransactionInfo(
	ctx context.Context,
	request TransactionInfoRequest,
) (TransactionInfo, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return TransactionInfo{}, err
	}
	revision := service.Revision()
	if request.ExpectedRevision != 0 && request.ExpectedRevision != revision {
		return TransactionInfo{}, newAppError(
			AppRevisionConflict, revision, errors.New("transaction information revision is stale"),
		)
	}
	matchOffset, matchLimit, err := normalizeInfoWindow(request.MatchOffset, request.MatchLimit)
	if err != nil {
		return TransactionInfo{}, newAppError(AppInvalidOperation, revision, err)
	}
	itemOffset, itemLimit, err := normalizeInfoWindow(request.ItemOffset, request.ItemLimit)
	if err != nil {
		return TransactionInfo{}, newAppError(AppInvalidOperation, revision, err)
	}
	transaction, matcher, profileKind, rawLabel, err := service.transactionInfoTarget(request.TransactionID)
	if err != nil {
		return TransactionInfo{}, newAppError(AppInvalidTarget, revision, err)
	}
	info := TransactionInfo{
		Revision: revision, Transaction: transaction, MatchOffset: matchOffset, MatchLimit: matchLimit,
		ItemOffset: itemOffset, ItemLimit: itemLimit,
	}
	if profileKind == amazonProvider && service.profile != nil {
		state, loadErr := service.profile.LoadAmazonMatchSource(ctx)
		if loadErr != nil {
			return TransactionInfo{}, mapAppError(loadErr, revision)
		}
		for _, item := range state.Items {
			if string(item.LocalTransactionID) != request.TransactionID {
				continue
			}
			info.AmazonQualified = true
			converted := amazonItemInfo(item.OrderID, item.ProductName, item.ASIN, item.Quantity,
				item.OrderStatus, item.ShipmentStatus, item.UnitPriceMinor,
				state.Settings.Currency, state.Settings.Scale)
			info.AmazonItem = &converted
			return info, nil
		}
		return TransactionInfo{}, newAppError(AppInvalidTarget, revision, errors.New("amazon source row is absent"))
	}
	if matcher == nil {
		info.AmazonQualified = isAmazonMerchantLabel(transaction.Merchant.Name) || isAmazonMerchantLabel(rawLabel)
		return info, nil
	}
	projection, err := matcher.Match(ctx, transaction, rawLabel, matchOffset+matchLimit)
	if err != nil {
		return TransactionInfo{}, newAppError(AppStoreError, revision, err)
	}
	info.AmazonQualified = projection.Qualified
	info.TotalMatches = projection.Result.Total
	if matchOffset >= len(projection.Result.Matches) {
		return info, nil
	}
	selected := projection.Result.Matches[matchOffset:min(matchOffset+matchLimit, len(projection.Result.Matches))]
	info.Matches = make([]TransactionInfoMatch, 0, len(selected))
	for _, match := range selected {
		projected := TransactionInfoMatch{
			Class: match.Class, Confidence: match.Confidence, ProfileID: match.ProfileID,
			OrderID: match.OrderID, OrderDate: match.OrderDate, OrderTotal: match.OrderTotal,
			DateDistanceDays:      match.DateDistanceDays,
			AmountDifferenceMinor: match.AmountDifferenceMinor,
			FirstProduct:          match.FirstProduct, TotalItems: len(match.Items),
		}
		if itemOffset < len(match.Items) {
			items := match.Items[itemOffset:min(itemOffset+itemLimit, len(match.Items))]
			for _, item := range items {
				projected.Items = append(projected.Items, amazonItemInfo(
					match.OrderID, item.ProductName, item.ASIN, item.Quantity,
					item.OrderStatus, item.ShipmentStatus, item.UnitPriceMinor,
					match.OrderTotal.Currency, match.OrderTotal.Scale,
				))
			}
		}
		info.Matches = append(info.Matches, projected)
	}
	return info, nil
}

func (service *Service) transactionInfoTarget(
	transactionID string,
) (domain.Transaction, *AmazonMatchingService, string, string, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	var transaction domain.Transaction
	found := false
	for _, candidate := range service.transactions {
		if candidate.ID == transactionID {
			transaction, found = candidate.Clone(), true
			break
		}
	}
	if !found {
		return domain.Transaction{}, nil, "", "", errors.New("transaction is absent")
	}
	rawLabel := service.rawProviderMerchantLabelLocked(transaction.Merchant.ID)
	return transaction, service.amazonMatcher, service.profileKind, rawLabel, nil
}

func (service *Service) rawProviderMerchantLabelLocked(merchantID string) string {
	return service.rawProviderMerchantLabelsLocked()[merchantID]
}

func (service *Service) rawProviderMerchantLabelsLocked() map[string]string {
	labels := make(map[string]string)
	if service.snapshot == nil {
		return labels
	}
	for _, identity := range service.snapshot.Effective.ExternalIdentities {
		if identity.EntityType != domain.EntityKindMerchant {
			continue
		}
		for _, allocation := range service.providerState.Allocations {
			if allocation.Kind == identity.EntityType && allocation.Namespace == identity.Namespace &&
				allocation.ExternalID == identity.ExternalID {
				labels[string(identity.EntityID)] = allocation.ProviderLabel
				break
			}
		}
	}
	return labels
}

func normalizeInfoWindow(offset, limit int) (int, int, error) {
	if offset < 0 || limit < 0 || limit > maxTransactionInfoLimit {
		return 0, 0, errors.New("transaction information window is invalid")
	}
	if limit == 0 {
		limit = defaultTransactionInfoLimit
	}
	return offset, limit, nil
}

func amazonItemInfo(
	orderID, productName, asin string,
	quantity int64,
	orderStatus, shipmentStatus string,
	unitPriceMinor *int64,
	currency domain.Currency,
	scale uint8,
) AmazonOrderItemInfo {
	result := AmazonOrderItemInfo{
		OrderID: orderID, ProductName: productName, ASIN: asin, Quantity: quantity,
		OrderStatus: orderStatus, ShipmentStatus: shipmentStatus,
	}
	if unitPriceMinor != nil {
		result.UnitPrice = &domain.Money{Minor: *unitPriceMinor, Currency: currency, Scale: scale}
	}
	return result
}
