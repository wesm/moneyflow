package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/wesm/moneyflow/internal/mcp"
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
	if event, cause, ok := moneyflowEvent(record); ok {
		_, err := fmt.Fprintf(
			handler.output, "level=%s code=%s cause=%s\n", record.Level.String(), event, cause,
		)
		return err
	}
	_, err := fmt.Fprintf(handler.output, "level=%s code=mcp_sdk_event\n", record.Level.String())
	return err
}

// moneyflowEvent reads the stable codes of an event Moneyflow logged itself. Only codes made of
// lowercase letters and underscores pass, so no free text reaches the log.
func moneyflowEvent(record slog.Record) (string, string, bool) {
	var event, cause string
	record.Attrs(func(attribute slog.Attr) bool {
		switch attribute.Key {
		case mcp.LogEventKey:
			event = attribute.Value.String()
		case mcp.LogCauseKey:
			cause = attribute.Value.String()
		}
		return true
	})
	return event, cause, stableCode(event) && stableCode(cause)
}

func stableCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && character != '_' {
			return false
		}
	}
	return true
}

func (handler mcpLogHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler mcpLogHandler) WithGroup(string) slog.Handler      { return handler }
