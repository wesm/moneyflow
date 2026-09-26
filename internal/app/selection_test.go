package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestSelectionCodecCanonicalExplicit(t *testing.T) {
	t.Parallel()

	document := selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		IDs:     []string{"txn-1", "txn-2"},
	}
	value, err := encodeSelection(document)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(value), selectionPrefix))
	assert.NotContains(t, strings.TrimPrefix(string(value), selectionPrefix), "=")

	decoded, err := decodeSelection(value)
	require.NoError(t, err)
	assert.Equal(t, document, decoded)

	reencoded, err := encodeSelection(decoded)
	require.NoError(t, err)
	assert.Equal(t, value, reencoded)
	assert.NotEmpty(t, EmptySelection())
}

func TestSelectionCodecCanonicalAll(t *testing.T) {
	t.Parallel()

	state := selectionDetailState()
	document := selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseAll,
		State:   &state,
		Include: []string{"txn-outside"},
		Exclude: []string{"txn-hidden"},
	}
	value, err := encodeSelection(document)
	require.NoError(t, err)
	decoded, err := decodeSelection(value)
	require.NoError(t, err)
	assert.Equal(t, document, decoded)
}

func TestSelectionCodecCarriesBoundedReturnFrameSelections(t *testing.T) {
	t.Parallel()

	state := DefaultViewState().Current
	document := selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		Returns: []selectionPayload{{
			Kind: IdentityAggregate, Base: selectionBaseAll, State: &state,
		}},
	}
	value, err := encodeSelection(document)
	require.NoError(t, err)
	decoded, err := decodeSelection(value)
	require.NoError(t, err)
	assert.Equal(t, document, decoded)

	document.IDs = []string{"current"}
	document.Returns[0] = selectionPayload{
		Kind: IdentityAggregate, Base: selectionBaseExplicit,
		IDs: make([]string, MaxSelectionIdentities),
	}
	for index := range document.Returns[0].IDs {
		document.Returns[0].IDs[index] = fmt.Sprintf("aggregate-%05d", index)
	}
	_, err = encodeSelection(document)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionTooLarge)
}

func TestSelectionCodecRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	validState := selectionDetailState()
	labelState := validState.Clone()
	labelState.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant,
		Currency:  "USD",
		Scale:     2,
		Key:       "merchant-example",
		Label:     "Example",
	}}

	tests := map[string]SelectionValue{
		"empty":            "",
		"prefix":           "mfsel2.invalid",
		"padding":          SelectionValue(selectionPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"v":1}`)) + "="),
		"invalid base64":   SelectionValue(selectionPrefix + "!"),
		"unknown field":    rawSelection(`{"v":1,"kind":"transaction","base":"explicit","unknown":true}`),
		"trailing json":    rawSelection(`{"v":1,"kind":"transaction","base":"explicit"}{}`),
		"version":          rawSelection(`{"v":2,"kind":"transaction","base":"explicit"}`),
		"kind":             rawSelection(`{"v":1,"kind":"missing","base":"explicit"}`),
		"base":             rawSelection(`{"v":1,"kind":"transaction","base":"missing"}`),
		"unsorted ids":     rawSelection(`{"v":1,"kind":"transaction","base":"explicit","ids":["b","a"]}`),
		"duplicate ids":    rawSelection(`{"v":1,"kind":"transaction","base":"explicit","ids":["a","a"]}`),
		"empty identity":   rawSelection(`{"v":1,"kind":"transaction","base":"explicit","ids":[""]}`),
		"explicit state":   mustRawSelection(t, selectionDocument{Version: 1, Kind: IdentityTransaction, Base: selectionBaseExplicit, State: &validState}),
		"all ids":          mustRawSelection(t, selectionDocument{Version: 1, Kind: IdentityTransaction, Base: selectionBaseAll, State: &validState, IDs: []string{"txn-1"}}),
		"all no state":     rawSelection(`{"v":1,"kind":"transaction","base":"all"}`),
		"overlap deltas":   rawSelection(`{"v":1,"kind":"transaction","base":"explicit","include":["a"],"exclude":["a"]}`),
		"overlap explicit": rawSelection(`{"v":1,"kind":"transaction","base":"explicit","ids":["a"],"include":["a"]}`),
		"display label":    mustRawSelection(t, selectionDocument{Version: 1, Kind: IdentityTransaction, Base: selectionBaseAll, State: &labelState}),
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeSelection(value)
			require.Error(t, err)
			assertSelectionCode(t, err, SelectionInvalid)
		})
	}
}

func TestSelectionCodecBounds(t *testing.T) {
	t.Parallel()

	document := selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		IDs:     []string{strings.Repeat("i", MaxSelectionIdentityBytes)},
	}
	_, err := encodeSelection(document)
	require.NoError(t, err)
	document.IDs[0] += "i"
	_, err = encodeSelection(document)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionTooLarge)

	document.IDs = make([]string, MaxSelectionIdentities)
	for index := range document.IDs {
		document.IDs[index] = fmt.Sprintf("id-%05d", index)
	}
	_, err = encodeSelection(document)
	require.NoError(t, err)
	document.IDs = append(document.IDs, "id-over-limit")
	_, err = encodeSelection(document)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionTooLarge)

	document = selectionDocumentAtSize(t, MaxSelectionDocumentBytes)
	_, err = encodeSelection(document)
	require.NoError(t, err)
	document.IDs[len(document.IDs)-1] += "x"
	_, err = encodeSelection(document)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionTooLarge)

	oversized := SelectionValue(selectionPrefix + strings.Repeat("a", MaxEncodedSelectionBytes+1))
	_, err = decodeSelection(oversized)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionInvalid)
}

func TestSelectionResolveExplicitAndToggle(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := selectionDetailState()
	value, err := encodeSelection(selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		IDs:     []string{"txn-000", "txn-outside"},
	})
	require.NoError(t, err)

	snapshot, err := service.ResolveSelection(state, value)
	require.NoError(t, err)
	assert.Equal(t, IdentityTransaction, snapshot.Kind)
	assert.Equal(t, stringSetForSelection("txn-000", "txn-outside"), snapshot.IDs)

	next, err := service.ToggleSelection(state, value, IdentityTransaction, "txn-001")
	require.NoError(t, err)
	nextSnapshot, err := service.ResolveSelection(state, next)
	require.NoError(t, err)
	assert.Equal(t, stringSetForSelection("txn-000", "txn-001", "txn-outside"), nextSnapshot.IDs)

	restored, err := service.ToggleSelection(state, next, IdentityTransaction, "txn-001")
	require.NoError(t, err)
	assert.Equal(t, value, restored)
}

func TestSelectionAllBasePreservesDefiningResultAcrossQueryChanges(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 120)
	state := selectionDetailState()
	selected, err := service.ToggleAllSelection(state, EmptySelection())
	require.NoError(t, err)

	document, err := decodeSelection(selected)
	require.NoError(t, err)
	assert.Equal(t, selectionBaseAll, document.Base)
	assert.NotNil(t, document.State)

	narrow := state.Clone()
	narrow.Search = "^Merchant 000$"
	narrow.SearchAnchor = &NavigationScope{
		Mode: domain.ResultModeDetail, Dimension: domain.DimensionMerchant,
	}
	snapshot, err := service.ResolveSelection(narrow, selected)
	require.NoError(t, err)
	assert.Len(t, snapshot.IDs, 120)

	clearedCurrent, err := service.ToggleAllSelection(narrow, selected)
	require.NoError(t, err)
	clearedSnapshot, err := service.ResolveSelection(narrow, clearedCurrent)
	require.NoError(t, err)
	assert.Len(t, clearedSnapshot.IDs, 119)
	assert.NotContains(t, clearedSnapshot.IDs, "txn-000")

	reselectedCurrent, err := service.ToggleAllSelection(narrow, clearedCurrent)
	require.NoError(t, err)
	reselectedSnapshot, err := service.ResolveSelection(state, reselectedCurrent)
	require.NoError(t, err)
	assert.Len(t, reselectedSnapshot.IDs, 120)
}

func TestSelectionToggleAllPreservesOutOfViewAndUsesSmallestExactValue(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 120)
	state := selectionDetailState()
	value, err := encodeSelection(selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		IDs:     []string{"txn-outside"},
	})
	require.NoError(t, err)

	next, err := service.ToggleAllSelection(state, value)
	require.NoError(t, err)
	document, err := decodeSelection(next)
	require.NoError(t, err)
	assert.Equal(t, selectionBaseAll, document.Base)
	assert.Equal(t, []string{"txn-outside"}, document.Include)

	snapshot, err := service.ResolveSelection(state, next)
	require.NoError(t, err)
	assert.Len(t, snapshot.IDs, 121)
	assert.Contains(t, snapshot.IDs, "txn-outside")

	cleared, err := service.ToggleAllSelection(state, next)
	require.NoError(t, err)
	clearedSnapshot, err := service.ResolveSelection(state, cleared)
	require.NoError(t, err)
	assert.Equal(t, stringSetForSelection("txn-outside"), clearedSnapshot.IDs)
}

func TestSelectionSmallestRepresentationBreaksTiesByCanonicalBytes(t *testing.T) {
	t.Parallel()

	candidates := []encodedSelectionCandidate{
		{value: "a", canonical: []byte{1}},
		{value: "z", canonical: []byte{0}},
	}
	assert.Equal(t, SelectionValue("z"), chooseSmallestSelection(candidates))
}

func TestSelectionAggregateKindIncludesMoneyPartition(t *testing.T) {
	t.Parallel()

	usd := appTransaction(t, "usd", "2024-01-01", "-1.00", "Example", "Category", "Group")
	eur := usd.Clone()
	eur.ID = "eur"
	eur.ProviderID = "provider-eur"
	eur.Amount.Currency = "EUR"
	service, err := NewService([]domain.Transaction{usd, eur})
	require.NoError(t, err)
	state := DefaultViewState().Current

	value, err := service.ToggleAllSelection(state, EmptySelection())
	require.NoError(t, err)
	snapshot, err := service.ResolveSelection(state, value)
	require.NoError(t, err)
	assert.Equal(t, IdentityAggregate, snapshot.Kind)
	assert.Len(t, snapshot.IDs, 2)

	identities := sortedSelectionIDs(snapshot.IDs)
	assert.NotEqual(t, identities[0], identities[1])
}

func TestSelectionAllBaseResolvesStableDrillKeysWithoutPersistedLabels(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	session := NewSession()
	result, err := service.Query(session)
	require.NoError(t, err)
	require.NotEmpty(t, result.AggregateRows)
	require.NoError(t, session.Drill(result.AggregateRows[0], ViewPosition{}))
	state := session.ViewState().Current
	require.NotEmpty(t, state.Drilldowns)
	assert.Empty(t, state.Drilldowns[0].Label)

	value, err := service.ToggleAllSelection(state, EmptySelection())
	require.NoError(t, err)
	snapshot, err := service.ResolveSelection(state, value)
	require.NoError(t, err)
	assert.Equal(t, IdentityTransaction, snapshot.Kind)
	assert.Len(t, snapshot.IDs, 1)
}

func TestSelectionRejectsKindMismatchAndPreservesOldValueOnLimit(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 1)
	state := selectionDetailState()
	old := EmptySelection()

	next, err := service.ToggleSelection(
		state,
		old,
		IdentityAggregate,
		"aggregate-target",
	)
	require.Error(t, err)
	assert.Equal(t, old, next)
	assertSelectionCode(t, err, SelectionInvalid)

	next, err = service.ToggleSelection(
		state,
		old,
		IdentityTransaction,
		strings.Repeat("x", MaxSelectionIdentityBytes+1),
	)
	require.Error(t, err)
	assert.Equal(t, old, next)
	assertSelectionCode(t, err, SelectionTooLarge)
}

func TestSelectionResolutionIgnoresSortAndRejectsResultKindMismatch(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 2)
	state := selectionDetailState()
	selected, err := service.ToggleAllSelection(state, EmptySelection())
	require.NoError(t, err)

	resorted := state.Clone()
	resorted.Sort = domain.SortSpec{
		Field: domain.SortFieldMerchant, Direction: domain.SortDirectionAsc,
	}
	snapshot, err := service.ResolveSelection(resorted, selected)
	require.NoError(t, err)
	assert.Len(t, snapshot.IDs, 2)

	aggregate := DefaultViewState().Current
	_, err = service.ResolveSelection(aggregate, selected)
	require.Error(t, err)
	assertSelectionCode(t, err, SelectionInvalid)
}

func rawSelection(document string) SelectionValue {
	return SelectionValue(selectionPrefix + base64.RawURLEncoding.EncodeToString([]byte(document)))
}

func mustRawSelection(t testing.TB, document selectionDocument) SelectionValue {
	t.Helper()
	encoded, err := marshalSelectionDocument(document)
	require.NoError(t, err)
	return SelectionValue(selectionPrefix + base64.RawURLEncoding.EncodeToString(encoded))
}

func selectionDetailState() AnalyticalState {
	session := NewSession()
	session.ShowAllDetail()
	return session.ViewState().Current
}

func selectionService(t testing.TB, count int) *Service {
	t.Helper()
	transactions := make([]domain.Transaction, count)
	for index := range transactions {
		transactions[index] = appTransaction(
			t,
			fmt.Sprintf("txn-%03d", index),
			"2024-01-01",
			"-1.00",
			fmt.Sprintf("Merchant %03d", index),
			"Category",
			"Group",
		)
	}
	service, err := NewService(transactions)
	require.NoError(t, err)
	return service
}

func stringSetForSelection(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortedSelectionIDs(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func selectionDocumentAtSize(t testing.TB, size int) selectionDocument {
	t.Helper()
	document := selectionDocument{
		Version: 1,
		Kind:    IdentityTransaction,
		Base:    selectionBaseExplicit,
		IDs:     make([]string, MaxSelectionIdentities),
	}
	for index := range document.IDs {
		document.IDs[index] = fmt.Sprintf("%05d-%s", index, strings.Repeat("x", 96))
	}
	data, err := marshalSelectionDocument(document)
	require.NoError(t, err)
	remaining := size - len(data)
	require.Positive(t, remaining)
	for index := range document.IDs {
		capacity := MaxSelectionIdentityBytes - len(document.IDs[index])
		added := min(remaining, capacity)
		document.IDs[index] += strings.Repeat("x", added)
		remaining -= added
		if remaining == 0 {
			break
		}
	}
	require.Zero(t, remaining)
	data, err = marshalSelectionDocument(document)
	require.NoError(t, err)
	require.Len(t, data, size)
	return document
}

func assertSelectionCode(t testing.TB, err error, code SelectionErrorCode) {
	t.Helper()
	var selectionErr *SelectionError
	require.True(t, errors.As(err, &selectionErr), err)
	assert.Equal(t, code, selectionErr.Code)
}
