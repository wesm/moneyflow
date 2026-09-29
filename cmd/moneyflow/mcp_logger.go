package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// mcpLogHandler deliberately emits only a stable code and severity. SDK messages and attributes
// are not part of Moneyflow's logging allowlist and may contain request-derived data.
type mcpLogHandler struct {
	output io.Writer
	mutex  *sync.Mutex
}

func newMCPLogger(output io.Writer) *slog.Logger {
	if output == nil {
		output = io.Discard
	}
	return slog.New(mcpLogHandler{output: output, mutex: &sync.Mutex{}})
}

func (handler mcpLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (handler mcpLogHandler) Handle(_ context.Context, record slog.Record) error {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	_, err := fmt.Fprintf(handler.output, "level=%s code=mcp_sdk_event\n", record.Level.String())
	return err
}

func (handler mcpLogHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler mcpLogHandler) WithGroup(string) slog.Handler      { return handler }
