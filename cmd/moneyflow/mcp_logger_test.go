package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/mcp"
)

func TestMCPLoggerEmitsOnlyAllowlistedFields(t *testing.T) {
	var output bytes.Buffer
	logger := newMCPLogger(&output)
	logger.LogAttrs(
		context.Background(), slog.LevelWarn, "sensitive request-derived message",
		slog.String("payload", "sensitive request-derived attribute"),
	)
	assert.Equal(t, "level=WARN code=mcp_sdk_event\n", output.String())
}

func TestMCPLoggerNamesAMoneyflowEventAndItsCauseCode(t *testing.T) {
	var output bytes.Buffer
	logger := newMCPLogger(&output)
	logger.LogAttrs(
		context.Background(), slog.LevelError, "provider write stopped",
		slog.String(mcp.LogEventKey, "provider_write_stopped"),
		slog.String(mcp.LogCauseKey, "store_error"),
		slog.String("payload", "sensitive request-derived attribute"),
	)
	logger.LogAttrs(
		context.Background(), slog.LevelError, "provider write stopped",
		slog.String(mcp.LogEventKey, "provider write stopped\nforged=line"),
		slog.String(mcp.LogCauseKey, "store_error"),
	)
	assert.Equal(t,
		"level=ERROR code=provider_write_stopped cause=store_error\n"+
			"level=ERROR code=mcp_sdk_event\n",
		output.String(),
	)
}
