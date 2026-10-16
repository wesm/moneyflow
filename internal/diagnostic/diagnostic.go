// Package diagnostic renders failure causes and recovered panics as text for operator logs.
package diagnostic

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// maximumPanicFrames bounds the stack written for one recovered panic.
const maximumPanicFrames = 16

// LogEventKey and LogCauseKey name the attributes of a Moneyflow log event: a stable event code
// and an application error code. A log handler may print these and nothing else.
const (
	LogEventKey = "moneyflow_event"
	LogCauseKey = "moneyflow_cause"
)

// DescribeCause renders a whole error tree, joined branches included. AppError, SafeError, and
// store.Error keep their cause out of Error(), so every link is unwrapped explicitly instead of
// trusting an outer message; a message that an earlier one already contains is left out.
func DescribeCause(err error) string {
	var messages []string
	for _, message := range causeMessages(err, nil) {
		if !containedIn(messages, message) {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, ": ")
}

// causeMessages lists the messages of err and everything it wraps, depth first.
func causeMessages(err error, messages []string) []string {
	if err == nil {
		return messages
	}
	messages = append(messages, err.Error())
	switch wrapped := err.(type) {
	case interface{ Unwrap() error }:
		return causeMessages(wrapped.Unwrap(), messages)
	case interface{ Unwrap() []error }:
		for _, branch := range wrapped.Unwrap() {
			messages = causeMessages(branch, messages)
		}
	}
	return messages
}

func containedIn(messages []string, message string) bool {
	for _, earlier := range messages {
		if strings.Contains(earlier, message) {
			return true
		}
	}
	return false
}

// PanicText describes a recovered panic value: an error's whole cause, a string as it is, and
// any other value by its type alone.
func PanicText(recovered any) string {
	switch value := recovered.(type) {
	case error:
		return DescribeCause(value)
	case string:
		return value
	default:
		return fmt.Sprintf("value of type %T", value)
	}
}

// PanicFrames lists the panicking call stack by function and file, without argument values. Call
// it from the deferred function that recovered the panic.
func PanicFrames() string {
	counters := make([]uintptr, 64)
	return FormatPanicFrames(counters[:runtime.Callers(1, counters)])
}

// FormatPanicFrames lists the frames below runtime.gopanic in counters, as PanicFrames does.
func FormatPanicFrames(counters []uintptr) string {
	frames := runtime.CallersFrames(counters)
	var lines []string
	panicking := false
	for len(lines) < maximumPanicFrames {
		frame, more := frames.Next()
		if frame.Function == "runtime.gopanic" {
			panicking = true
		} else if panicking && !strings.HasPrefix(frame.Function, "runtime.") {
			lines = append(lines, fmt.Sprintf(
				"    %s (%s:%d)", frame.Function, filepath.Base(frame.File), frame.Line,
			))
		}
		if !more {
			break
		}
	}
	return strings.Join(lines, "\n")
}
