package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestTransactionInfoUsesFocusedRowAndRendersSortedMetadata(t *testing.T) {
	t.Parallel()

	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	require.GreaterOrEqual(t, len(model.result.DetailRows), 2)
	model.cursor = 1
	model.result.DetailRows[model.cursor].Transaction.Metadata = map[string]string{
		"zeta":  "last",
		"alpha": "first",
	}
	focused := model.result.DetailRows[model.cursor].Transaction.Clone()
	model.session.ToggleTransactionSelection(model.result.DetailRows[0].Transaction.ID)

	model = press(t, model, keyRune('i'))

	require.Equal(t, overlayTransactionInfo, model.overlay)
	assert.Equal(t, focused.ID, model.transactionInfo.transaction.ID)
	screen := model.RenderScreen()
	lines := strings.Join(screen.Frame.PlainLines(), "\n")
	assert.Contains(t, lines, "Transaction details")
	assert.Contains(t, lines, "Amount")
	assert.Contains(t, lines, FormatAmount(focused.Amount))
	assert.Contains(t, lines, string(focused.Amount.Currency))
	assert.Contains(t, lines, "Local transaction ID")
	semantic := strings.Join(screen.Overlay, "\n")
	assert.Less(t, strings.Index(semantic, "alpha"), strings.Index(semantic, "zeta"))
}

func TestTransactionInfoLinesRenderAmazonSourceAndFinanceMatches(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model.session.ShowAllDetail()
	model.refresh()
	transaction := model.result.DetailRows[0].Transaction
	date, err := domain.ParseDate("2026-08-20")
	require.NoError(t, err)
	state := transactionInfoState{transaction: transaction, info: &app.TransactionInfo{
		AmazonQualified: true,
		Matches: []app.TransactionInfoMatch{{
			Class: analytics.AmazonMatchExactOrder, Confidence: analytics.AmazonConfidenceHigh,
			OrderID: "example-order", OrderDate: date,
			OrderTotal:   domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
			FirstProduct: "Example Product",
		}},
	}}
	assert.Contains(t, strings.Join(transactionInfoLines(state), "\n"), "Example Product")

	state.info = &app.TransactionInfo{AmazonItem: &app.AmazonOrderItemInfo{
		OrderID: "example-order", ProductName: "Example Product", Quantity: 1,
	}}
	lines := strings.Join(transactionInfoLines(state), "\n")
	assert.Contains(t, lines, "Amazon order")
	assert.Contains(t, lines, "Product")
}

func TestTransactionInfoRejectsAggregateRowsClearly(t *testing.T) {
	t.Parallel()

	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('i'))

	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, "Transaction information is available from a transaction row.", model.status)
}

func TestTransactionInfoScrollAndClosePreserveTablePosition(t *testing.T) {
	t.Parallel()

	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model.cursor = min(2, len(model.result.DetailRows)-1)
	model.scroll = 1
	model.result.DetailRows[model.cursor].Transaction.Metadata = make(map[string]string, 40)
	for index := range 40 {
		model.result.DetailRows[model.cursor].Transaction.Metadata[fmt.Sprintf("field-%02d", index)] = "value"
	}
	wantCursor, wantScroll := model.cursor, model.scroll

	model = press(t, model, keyRune('i'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgDown})
	assert.Positive(t, model.transactionInfo.scroll)
	model = press(t, model, keyRune('i'))

	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, wantCursor, model.cursor)
	assert.Equal(t, wantScroll, model.scroll)
}
