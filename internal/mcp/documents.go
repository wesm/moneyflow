// Package mcp adapts renderer-neutral Moneyflow services to the Model Context Protocol.
package mcp

import (
	"errors"
	"strconv"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	// DocumentVersion is the only MCP document version emitted by this server.
	DocumentVersion = "1"
	// StatusOK identifies a successful logical result.
	StatusOK = "ok"
	// StatusError identifies a business failure returned as a tool result.
	StatusError = "error"
	// MaxResponseContentBytes bounds structured content plus its JSON text fallback.
	MaxResponseContentBytes = 8 << 20
	// MaxRows bounds every transaction window exposed over MCP.
	MaxRows = 1_000
)

// Header is shared by every version-one MCP document.
type Header struct {
	Version  string `json:"version"`
	Status   string `json:"status"`
	Revision string `json:"revision"`
}

// NewHeader returns a canonical header with an exact decimal revision.
func NewHeader(status string, revision uint64) Header {
	return Header{
		Version: DocumentVersion, Status: status,
		Revision: strconv.FormatUint(revision, 10),
	}
}

// Money is the exact wire representation of one signed amount.
type Money struct {
	Amount      string `json:"amount"`
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
	Scale       uint8  `json:"scale"`
}

// MoneyDocument converts one domain amount without floating point.
func MoneyDocument(value domain.Money) Money {
	return Money{
		Amount: value.DecimalString(), AmountMinor: strconv.FormatInt(value.Minor, 10),
		Currency: string(value.Currency), Scale: value.Scale,
	}
}

// ErrorDocument is the stable business-failure envelope returned to MCP clients.
type ErrorDocument struct {
	Header
	Code            string `json:"code"`
	Detail          string `json:"detail"`
	CurrentRevision string `json:"current_revision,omitempty"`
	NextEligible    string `json:"next_eligible,omitempty"`
	CorrelationID   string `json:"correlation_id,omitempty"`
}

// ApplicationErrorDocument maps only renderer-safe application failures.
func ApplicationErrorDocument(err error, revision uint64) (ErrorDocument, bool) {
	var failure *app.AppError
	if !errors.As(err, &failure) {
		return ErrorDocument{}, false
	}
	document := ErrorDocument{
		Header: NewHeader(StatusError, revision), Code: string(failure.Code), Detail: failure.Detail,
	}
	if failure.CurrentRevision != 0 {
		document.CurrentRevision = strconv.FormatUint(failure.CurrentRevision, 10)
	}
	return document, true
}

func responseTooLargeDocument(revision uint64) ErrorDocument {
	return ErrorDocument{
		Header: NewHeader(StatusError, revision), Code: "mcp_response_too_large",
		Detail: "The requested result exceeds the MCP response limit.",
	}
}
