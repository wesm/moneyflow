package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
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
