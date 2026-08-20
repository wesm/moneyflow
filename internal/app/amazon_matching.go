package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// AmazonSourceDescriptor is one catalog entry without financial contents.
type AmazonSourceDescriptor struct {
	ProfileID string
	Kind      string
}

// AmazonSourceDirectory lists current catalog entries for matching.
type AmazonSourceDirectory interface {
	ListAmazonSources(context.Context) ([]AmazonSourceDescriptor, error)
}

// AmazonSourceLoader probes one source revision and returns data only when it changed.
type AmazonSourceLoader func(
	context.Context,
	AmazonSourceDescriptor,
	uint64,
) (*store.AmazonMatchSourceState, func() error, error)

// AmazonMatchProjection contains one result plus counts-only source diagnostics.
type AmazonMatchProjection struct {
	Qualified bool
	Result    analytics.AmazonMatchResult
	Skipped   map[string]int
}

// AmazonMatchInput is one transaction plus its provider-owned merchant label.
type AmazonMatchInput struct {
	Transaction              domain.Transaction
	RawProviderMerchantLabel string
}

type amazonCachedSource struct {
	revision uint64
	source   analytics.AmazonMatchSource
}

// AmazonMatchingService owns immutable source indexes shared by every renderer.
type AmazonMatchingService struct {
	directory AmazonSourceDirectory
	loader    AmazonSourceLoader

	mu     sync.Mutex
	cache  map[string]amazonCachedSource
	builds int
}

// NewAmazonMatchingService validates and creates one cross-profile matcher.
func NewAmazonMatchingService(
	directory AmazonSourceDirectory,
	loader AmazonSourceLoader,
) (*AmazonMatchingService, error) {
	if directory == nil || loader == nil {
		return nil, errors.New("create Amazon matching service: dependencies are incomplete")
	}
	return &AmazonMatchingService{
		directory: directory, loader: loader, cache: make(map[string]amazonCachedSource),
	}, nil
}

// Match qualifies one finance transaction and evaluates all compatible source profiles.
func (service *AmazonMatchingService) Match(
	ctx context.Context,
	transaction domain.Transaction,
	rawProviderMerchantLabel string,
	limit int,
) (AmazonMatchProjection, error) {
	results, err := service.MatchBatch(ctx, []AmazonMatchInput{{
		Transaction: transaction, RawProviderMerchantLabel: rawProviderMerchantLabel,
	}}, limit)
	if err != nil {
		return AmazonMatchProjection{}, err
	}
	return results[0], nil
}

// MatchBatch loads each source snapshot once and evaluates a bounded transaction batch.
func (service *AmazonMatchingService) MatchBatch(
	ctx context.Context,
	inputs []AmazonMatchInput,
	limit int,
) ([]AmazonMatchProjection, error) {
	results := make([]AmazonMatchProjection, len(inputs))
	qualified := false
	for index, input := range inputs {
		results[index].Qualified = isAmazonMerchantLabel(input.Transaction.Merchant.Name) ||
			isAmazonMerchantLabel(input.RawProviderMerchantLabel)
		results[index].Skipped = make(map[string]int)
		qualified = qualified || results[index].Qualified
	}
	if !qualified {
		return results, nil
	}
	sources, skipped, err := service.loadSources(ctx)
	if err != nil {
		return nil, err
	}
	for index, input := range inputs {
		if !results[index].Qualified {
			continue
		}
		for reason, count := range skipped {
			results[index].Skipped[reason] = count
		}
		for _, source := range sources {
			if source.Currency != input.Transaction.Amount.Currency ||
				source.Scale != input.Transaction.Amount.Scale {
				results[index].Skipped["money_mismatch"]++
			}
		}
		results[index].Result, err = analytics.MatchAmazonOrders(input.Transaction, sources, limit)
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (service *AmazonMatchingService) loadSources(
	ctx context.Context,
) ([]analytics.AmazonMatchSource, map[string]int, error) {
	skipped := make(map[string]int)
	descriptors, err := service.directory.ListAmazonSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(descriptors, func(left, right AmazonSourceDescriptor) int {
		return strings.Compare(left.ProfileID, right.ProfileID)
	})
	present := make(map[string]struct{}, len(descriptors))
	sources := make([]analytics.AmazonMatchSource, 0, len(descriptors))
	for _, descriptor := range descriptors {
		present[descriptor.ProfileID] = struct{}{}
		if descriptor.Kind != amazonProvider {
			skipped["not_amazon"]++
			continue
		}
		state, closeSource, loadErr := service.loader(
			ctx, descriptor, service.cachedRevision(descriptor.ProfileID),
		)
		if loadErr != nil {
			skipped["source_unavailable"]++
			continue
		}
		if closeSource == nil {
			skipped["source_unavailable"]++
			continue
		}
		closeErr := closeSource()
		if closeErr != nil {
			skipped["source_unavailable"]++
			continue
		}
		if state == nil {
			cached, ok := service.cloneCachedSource(descriptor.ProfileID)
			if !ok {
				skipped["source_unavailable"]++
				continue
			}
			sources = append(sources, cached)
			continue
		}
		sources = append(sources, service.cachedSource(descriptor.ProfileID, *state))
	}
	service.evictMissing(present)
	return sources, skipped, nil
}

func (service *AmazonMatchingService) cachedRevision(profileID string) uint64 {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.cache[profileID].revision
}

func (service *AmazonMatchingService) cloneCachedSource(
	profileID string,
) (analytics.AmazonMatchSource, bool) {
	service.mu.Lock()
	defer service.mu.Unlock()
	cached, ok := service.cache[profileID]
	if !ok {
		return analytics.AmazonMatchSource{}, false
	}
	return cloneAnalyticsAmazonSource(cached.source), true
}

// ProductMatches reports whether one bounded canonical match contains a raw product substring.
func (service *AmazonMatchingService) ProductMatches(
	ctx context.Context,
	transaction domain.Transaction,
	rawProviderMerchantLabel string,
	query string,
) (bool, error) {
	projection, err := service.Match(ctx, transaction, rawProviderMerchantLabel, 20)
	if err != nil || !projection.Qualified {
		return false, err
	}
	query = strings.ToLower(query)
	for _, match := range projection.Result.Matches {
		for _, item := range match.Items {
			if strings.Contains(strings.ToLower(item.ProductName), query) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (service *AmazonMatchingService) cachedSource(
	profileID string,
	state store.AmazonMatchSourceState,
) analytics.AmazonMatchSource {
	service.mu.Lock()
	defer service.mu.Unlock()
	if cached, ok := service.cache[profileID]; ok && cached.revision == state.Revision {
		return cloneAnalyticsAmazonSource(cached.source)
	}
	items := make([]analytics.AmazonMatchItem, 0, len(state.Items))
	for _, item := range state.Items {
		if item.Retired {
			continue
		}
		unitPrice := item.UnitPriceMinor
		if unitPrice != nil {
			value := *unitPrice
			unitPrice = &value
		}
		items = append(items, analytics.AmazonMatchItem{
			LocalTransactionID: item.LocalTransactionID, OrderID: item.OrderID,
			ProductName: item.ProductName, Date: item.OrderDate, AmountMinor: item.AmountMinor,
			ASIN: item.ASIN, Quantity: item.Quantity, OrderStatus: item.OrderStatus,
			ShipmentStatus: item.ShipmentStatus, UnitPriceMinor: unitPrice,
		})
	}
	source := analytics.AmazonMatchSource{
		ProfileID: profileID, Revision: state.Revision,
		Currency: state.Settings.Currency, Scale: state.Settings.Scale, Items: items,
	}
	service.cache[profileID] = amazonCachedSource{revision: state.Revision, source: source}
	service.builds++
	return cloneAnalyticsAmazonSource(source)
}

func (service *AmazonMatchingService) evictMissing(present map[string]struct{}) {
	service.mu.Lock()
	defer service.mu.Unlock()
	for profileID := range service.cache {
		if _, ok := present[profileID]; !ok {
			delete(service.cache, profileID)
		}
	}
}

// CacheBuilds returns a test/diagnostic count without exposing source facts.
func (service *AmazonMatchingService) CacheBuilds() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.builds
}

// CacheSize returns the number of immutable profile/revision indexes.
func (service *AmazonMatchingService) CacheSize() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return len(service.cache)
}

func cloneAnalyticsAmazonSource(source analytics.AmazonMatchSource) analytics.AmazonMatchSource {
	clone := source
	clone.Items = append([]analytics.AmazonMatchItem(nil), source.Items...)
	for index := range clone.Items {
		if clone.Items[index].UnitPriceMinor != nil {
			value := *clone.Items[index].UnitPriceMinor
			clone.Items[index].UnitPriceMinor = &value
		}
	}
	return clone
}

func isAmazonMerchantLabel(label string) bool {
	lowered := strings.ToLower(label)
	return strings.Contains(lowered, "amazon") || strings.Contains(lowered, "amzn")
}
