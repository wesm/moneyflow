package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

var usdScope = domain.CurrencyScope{Currency: "USD", Scale: 2}

func TestInsightsUseCommittedValuesWhileEditsArePending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := insightsService(t)
	state := insightsState()
	request := app.InsightsRequest{
		State: state, Rows: []string{merchantIdentity("merchant_a")}, Summary: true,
	}
	before, err := service.Insights(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), before.Revision)
	require.Len(t, before.Insights.Series, 1)
	assert.Equal(t, "merchant_a", before.Insights.Series[0].Key)
	assert.Equal(t, "2026-09-01", before.Insights.Today.String())
	scope := before.Insights.Summary.KPIs.Scope
	assert.Equal(t, int64(-100), scope.Net, "the hidden transaction adds no money")
	assert.Equal(t, 2, scope.Count)
	assert.Equal(t, 1, scope.HiddenCount)

	detail := detailViewState()
	projection, err := service.ProjectView(detail, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: projection.Revision,
		State: detail, Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: projection.DetailRows[0].Identity,
		},
	})
	require.NoError(t, err)
	after, err := service.Insights(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, before.Insights, after.Insights, "pending edits do not change insights")
	assert.Equal(t, uint64(8), after.Revision, "the revision follows the pending edit")
}

func TestInsightsDeduplicateRequestedRows(t *testing.T) {
	t.Parallel()
	service := insightsService(t)
	identity := merchantIdentity("merchant_b")
	result, err := service.Insights(context.Background(), app.InsightsRequest{
		State: insightsState(), Rows: []string{identity, identity},
	})
	require.NoError(t, err)
	require.Len(t, result.Insights.Series, 1)
	assert.Equal(t, "merchant_b", result.Insights.Series[0].Key)
	assert.Nil(t, result.Insights.Summary, "the summary is computed only on request")
}

func TestInsightsRejectInvalidRequests(t *testing.T) {
	t.Parallel()
	noCurrency := insightsState()
	noCurrency.Currency = nil
	tooMany := make([]string, app.MaxInsightsRows+1)
	for index := range tooMany {
		tooMany[index] = merchantIdentity("merchant_a")
	}
	category := domain.AggregateRow{
		Dimension: domain.DimensionCategory, Key: "category_a",
		Total: domain.Money{Currency: "USD", Scale: 2},
	}
	euro := domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "merchant_a",
		Total: domain.Money{Currency: "EUR", Scale: 2},
	}
	tests := map[string]app.InsightsRequest{
		"no currency scope": {State: noCurrency},
		"too many rows":     {State: insightsState(), Rows: tooMany},
		"malformed row":     {State: insightsState(), Rows: []string{"merchant:9:merchant_a:2:USD"}},
		"padded length":     {State: insightsState(), Rows: []string{"merchant:010:merchant_a:2:USD"}},
		"empty row":         {State: insightsState(), Rows: []string{""}},
		"no length":         {State: insightsState(), Rows: []string{"merchant:"}},
		"no key":            {State: insightsState(), Rows: []string{"merchant:0:"}},
		"negative length":   {State: insightsState(), Rows: []string{"merchant:-1:x:2:USD"}},
		"signed length":     {State: insightsState(), Rows: []string{"merchant:+1:a:2:USD"}},
		"text scale":        {State: insightsState(), Rows: []string{"merchant:1:a:two:USD"}},
		"other dimension":   {State: insightsState(), Rows: []string{app.AggregateIdentity(category)}},
		"other currency":    {State: insightsState(), Rows: []string{app.AggregateIdentity(euro)}},
	}
	service := insightsService(t)
	for name, request := range tests {
		_, err := service.Insights(context.Background(), request)
		var webErr *app.WebError
		require.True(t, errors.As(err, &webErr), "%s: %v", name, err)
		assert.Equal(t, app.WebInvalidRequest, webErr.Code, name)
	}
}

func TestInsightsAcceptRowKeysContainingSeparators(t *testing.T) {
	t.Parallel()
	row := domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: strings.Repeat(":", 3),
		Total: domain.Money{Currency: "USD", Scale: 2},
	}
	result, err := insightsService(t).Insights(context.Background(), app.InsightsRequest{
		State: insightsState(), Rows: []string{app.AggregateIdentity(row)},
	})
	require.NoError(t, err)
	require.Len(t, result.Insights.Series, 1)
	assert.Equal(t, ":::", result.Insights.Series[0].Key)
}

func insightsService(t *testing.T) *app.Service {
	t.Helper()
	service, err := app.NewProfileService(context.Background(), newMemoryProfile(t, 7))
	require.NoError(t, err)
	service.SetClock(func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local) })
	return service
}

func insightsState() app.AnalyticalState {
	state := app.DefaultViewState().Current
	scope := usdScope
	state.Currency = &scope
	return state
}

func merchantIdentity(key string) string {
	return app.AggregateIdentity(domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: key,
		Total: domain.Money{Currency: usdScope.Currency, Scale: usdScope.Scale},
	})
}
