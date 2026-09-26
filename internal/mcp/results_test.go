package mcp

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestMoneyDocumentPreservesExactValues(t *testing.T) {
	money := MoneyDocument(domain.Money{Minor: -1234, Currency: "USD", Scale: 2})
	assert.Equal(t, "-12.34", money.Amount)
	assert.Equal(t, "-1234", money.AmountMinor)
	assert.Equal(t, "USD", money.Currency)
	assert.Equal(t, uint8(2), money.Scale)
}

func TestApplicationErrorDocumentUsesStableCodeAndStringRevision(t *testing.T) {
	document, ok := ApplicationErrorDocument(&app.AppError{
		Code: app.AppRevisionConflict, Detail: "The profile changed and must be refreshed.",
		CurrentRevision: 42,
	}, 7)
	require.True(t, ok)
	assert.Equal(t, DocumentVersion, document.Version)
	assert.Equal(t, StatusError, document.Status)
	assert.Equal(t, "7", document.Revision)
	assert.Equal(t, "42", document.CurrentRevision)
	assert.Equal(t, "revision_conflict", document.Code)
	assert.NotContains(t, document.Detail, "42")
}

func TestToolResultStructuredAndTextContentAreEquivalent(t *testing.T) {
	document := struct {
		Header
		Count int `json:"count"`
	}{Header: NewHeader(StatusOK, 17), Count: 3}
	result, err := ToolResult(document, false)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok)
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	assert.JSONEq(t, text.Text, string(structured))
	assert.False(t, result.IsError)
}

func TestToolResultPreservesLargeIntegersInStructuredContent(t *testing.T) {
	document := struct {
		Header
		Count int64 `json:"count"`
	}{Header: NewHeader(StatusOK, 17), Count: math.MaxInt64}
	result, err := ToolResult(document, false)
	require.NoError(t, err)
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok)
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	assert.JSONEq(t, text.Text, string(structured))
	assert.Contains(t, string(structured), `"count":9223372036854775807`)
}

func TestToolResultReplacesOversizedSuccessWithBoundedFailure(t *testing.T) {
	document := struct {
		Header
		Value string `json:"value"`
	}{Header: NewHeader(StatusOK, 9), Value: strings.Repeat("x", MaxResponseContentBytes/2)}
	result, err := ToolResult(document, false)
	require.NoError(t, err)
	require.True(t, result.IsError)
	errorDocument := decodeErrorDocument(t, result)
	assert.Equal(t, "mcp_response_too_large", errorDocument.Code)
	assert.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
}

func TestToolResultKeepsSuccessBelowCombinedResponseCeiling(t *testing.T) {
	document := struct {
		Header
		Value string `json:"value"`
	}{
		Header: NewHeader(StatusOK, 9),
		Value:  strings.Repeat("x", MaxResponseContentBytes/2-1_024),
	}
	result, err := ToolResult(document, false)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
}

func TestToolResultPreservesSpecificCodeWhenErrorDetailIsOversized(t *testing.T) {
	document := ErrorDocument{
		Header: NewHeader(StatusError, 11), Code: "invalid_target",
		Detail: strings.Repeat("sensitive", MaxResponseContentBytes),
	}
	result, err := ToolResult(document, true)
	require.NoError(t, err)
	require.True(t, result.IsError)
	bounded := decodeErrorDocument(t, result)
	assert.Equal(t, "invalid_target", bounded.Code)
	assert.NotEqual(t, document.Detail, bounded.Detail)
	assert.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
}

func TestToolResultBoundsPointerErrorDocument(t *testing.T) {
	document := &ErrorDocument{
		Header: NewHeader(StatusError, 11), Code: "invalid_target",
		Detail: strings.Repeat("sensitive", MaxResponseContentBytes),
	}
	result, err := ToolResult(document, true)
	require.NoError(t, err)
	bounded := decodeErrorDocument(t, result)
	assert.Equal(t, "invalid_target", bounded.Code)
	assert.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
}

func decodeErrorDocument(t *testing.T, result *mcpsdk.CallToolResult) ErrorDocument {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var document ErrorDocument
	require.NoError(t, json.Unmarshal(encoded, &document))
	return document
}

func resultContentBytes(t *testing.T, result *mcpsdk.CallToolResult) int {
	t.Helper()
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok)
	return len(structured) + len(text.Text)
}
