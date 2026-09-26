package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type transactionInfoState struct {
	transaction domain.Transaction
	info        *app.TransactionInfo
	scroll      int
	previous    overlayKind
}

func (model *Model) openTransactionInfo() tea.Cmd {
	if model.result.DetailRows == nil || model.cursor < 0 || model.cursor >= len(model.result.DetailRows) {
		model.status = "Transaction information is available from a transaction row."
		return nil
	}
	return model.openTransactionInfoFor(model.result.DetailRows[model.cursor].Transaction, overlayNone)
}

func (model *Model) openTransactionInfoFor(transaction domain.Transaction, previous overlayKind) tea.Cmd {
	info, err := model.service.TransactionInfo(model.ctx, app.TransactionInfoRequest{
		ExpectedRevision: model.service.Revision(), TransactionID: transaction.ID,
	})
	if err != nil {
		model.status = safeInteractionMessage(err)
		return nil
	}
	model.transactionInfo = transactionInfoState{
		transaction: transaction.Clone(), info: &info, previous: previous,
	}
	model.status = ""
	model.overlay = overlayTransactionInfo
	return nil
}

func (model *Model) routeTransactionInfo(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc", "enter", "i":
		model.overlay = model.transactionInfo.previous
		model.transactionInfo = transactionInfoState{}
	case "up", "k":
		model.transactionInfo.scroll = max(0, model.transactionInfo.scroll-1)
	case "down", "j":
		model.transactionInfo.scroll = min(model.transactionInfoMaxScroll(), model.transactionInfo.scroll+1)
	case "pgup":
		model.transactionInfo.scroll = max(0, model.transactionInfo.scroll-model.transactionInfoPageSize())
	case "pgdown":
		model.transactionInfo.scroll = min(
			model.transactionInfoMaxScroll(),
			model.transactionInfo.scroll+model.transactionInfoPageSize(),
		)
	}
	return nil
}

func (model Model) transactionInfoRect() Rect {
	return responsiveOverlayRect(model.width, model.height, 92, 36)
}

func (model Model) transactionInfoPageSize() int {
	return max(1, model.transactionInfoRect().Height-4)
}

func (model Model) transactionInfoMaxScroll() int {
	return max(0, len(transactionInfoLines(model.transactionInfo))-model.transactionInfoPageSize())
}

func (model Model) renderTransactionInfo(screen *RenderedScreen) {
	rect := model.transactionInfoRect()
	drawOverlayBox(&screen.Frame, rect, model.palette, "Transaction details")
	lines := transactionInfoLines(model.transactionInfo)
	start := min(model.transactionInfo.scroll, max(0, len(lines)-1))
	end := min(len(lines), start+model.transactionInfoPageSize())
	x, width := rect.X+2, max(0, rect.Width-4)
	for index, line := range lines[start:end] {
		style := model.palette.Text
		if transactionInfoSection(line) {
			style = model.palette.Heading
		}
		screen.Frame.PutText(x, rect.Y+2+index, Truncate(line, width), style)
	}
	footer := "↑/↓ PageUp/PageDown=Scroll | Esc/Enter/i=Close"
	putCentered(
		&screen.Frame,
		Rect{X: rect.X + 1, Y: rect.Y + rect.Height - 2, Width: max(0, rect.Width-2), Height: 1},
		Truncate(footer, max(0, rect.Width-4)),
		model.palette.Muted,
	)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "transaction_info", Rect: rect})
	screen.Overlay = append([]string{"Transaction details"}, lines...)
}

func transactionInfoLines(state transactionInfoState) []string {
	transaction := state.transaction
	lines := []string{
		"Transaction",
		transactionInfoField("Date", transaction.Date.String()),
		transactionInfoField("Amount", FormatAmount(transaction.Amount)+" "+string(transaction.Amount.Currency)),
		transactionInfoField("Merchant", transaction.Merchant.Name),
		transactionInfoField("Category", transaction.Category.Name),
		transactionInfoField("Group", transaction.Category.Group),
		transactionInfoField("Account", transaction.Account.Name),
		transactionInfoField("Notes", transaction.Notes),
		transactionInfoField("Status", transactionStatus(transaction)),
		transactionInfoField("Visibility", transactionVisibility(transaction)),
		"",
		"Identifiers",
		transactionInfoField("Local transaction ID", transaction.ID),
		transactionInfoField("External transaction ID", transaction.ProviderID),
		transactionInfoField("Provider", transaction.Provider),
		transactionInfoField("Merchant ID", transaction.Merchant.ID),
		transactionInfoField("Category ID", transaction.Category.ID),
		transactionInfoField("Group ID", transaction.Category.GroupID),
		transactionInfoField("Account ID", transaction.Account.ID),
	}
	if state.info != nil && state.info.AmazonItem != nil {
		item := state.info.AmazonItem
		lines = append(lines, "", "Amazon order",
			transactionInfoField("Order", item.OrderID),
			transactionInfoField("Product", item.ProductName),
			transactionInfoField("ASIN", item.ASIN),
			transactionInfoField("Quantity", fmt.Sprintf("%d", item.Quantity)),
			transactionInfoField("Order status", item.OrderStatus),
			transactionInfoField("Shipment status", item.ShipmentStatus),
		)
		if item.UnitPrice != nil {
			lines = append(lines, transactionInfoField("Unit price", FormatAmount(*item.UnitPrice)))
		}
	}
	if state.info != nil && state.info.AmazonQualified && state.info.AmazonItem == nil {
		lines = append(lines, "", "Amazon matches")
		if len(state.info.Matches) == 0 {
			lines = append(lines, "No matching Amazon orders")
		}
		for index, match := range state.info.Matches {
			lines = append(lines,
				fmt.Sprintf("Match %d · %s · %s", index+1, match.Confidence, match.Class),
				transactionInfoField("Order", match.OrderID),
				transactionInfoField("Order date", match.OrderDate.String()),
				transactionInfoField("Order total", FormatAmount(match.OrderTotal)),
				transactionInfoField("Best product", match.FirstProduct),
			)
		}
	}
	if len(transaction.Metadata) == 0 {
		return lines
	}
	lines = append(lines, "", "Metadata")
	keys := make([]string, 0, len(transaction.Metadata))
	for key := range transaction.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, transactionInfoField(key, transaction.Metadata[key]))
	}
	return lines
}

func transactionInfoField(label string, value string) string {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	return padRight(label, 25) + value
}

func transactionInfoSection(line string) bool {
	return line == "Transaction" || line == "Identifiers" || line == "Metadata" ||
		line == "Amazon order" || line == "Amazon matches"
}

func transactionStatus(transaction domain.Transaction) string {
	if transaction.Pending {
		return "Pending"
	}
	return "Posted"
}

func transactionVisibility(transaction domain.Transaction) string {
	if transaction.Hidden {
		return "Hidden from reports"
	}
	return "Visible in reports"
}
