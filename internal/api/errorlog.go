package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Server failures reach the browser as generic problems, so the cause goes to the operator log.
// Log lines name the request method and path only: never the query string, headers, or body,
// which can carry mutation tokens, credentials, or financial data.

// maximumPanicFrames bounds the stack written for one recovered panic.
const maximumPanicFrames = 16

type problemCauseKey struct{}

// problemCause carries one request's failure cause from the handler to safeProblemResponses.
type problemCause struct {
	err error
}

func withProblemCause(request *http.Request) (*http.Request, *problemCause) {
	slot := &problemCause{}
	return request.WithContext(context.WithValue(request.Context(), problemCauseKey{}, slot)), slot
}

// noteProblemCause records why the current request failed, for the server log only.
func noteProblemCause(ctx context.Context, err error) {
	if slot, ok := ctx.Value(problemCauseKey{}).(*problemCause); ok && err != nil {
		slot.err = err
	}
}

// recordProblemCause is a Huma transformer. Huma serializes only a problem's public fields, so
// this keeps the cause of a server failure, including an error a handler returned unmapped.
func recordProblemCause(ctx huma.Context, _ string, value any) (any, error) {
	switch body := value.(type) {
	case *Problem:
		noteProblemCause(ctx.Context(), body.cause)
	case *huma.ErrorModel:
		if body.Status >= http.StatusInternalServerError {
			noteProblemCause(ctx.Context(), errors.New(humaErrorText(body)))
		}
	}
	return value, nil
}

func humaErrorText(model *huma.ErrorModel) string {
	parts := []string{model.Detail}
	for _, detail := range model.Errors {
		if detail != nil && detail.Message != "" {
			parts = append(parts, detail.Message)
		}
	}
	return strings.Join(parts, ": ")
}

// describeCause renders an error chain. AppError and SafeError keep their cause out of
// Error(), so the chain is unwrapped explicitly instead of trusting the outer message.
func describeCause(err error) string {
	message := err.Error()
	next := errors.Unwrap(err)
	if next == nil || strings.Contains(message, next.Error()) {
		return message
	}
	return message + ": " + describeCause(next)
}

func logServerFailure(logger *log.Logger, request *http.Request, problem *Problem, cause error) {
	line := fmt.Sprintf("%s %s: %d %s", request.Method, request.URL.Path, problem.Status, problem.Code)
	if cause != nil {
		line += ": " + describeCause(cause)
	}
	logger.Print(line)
}

func logPanic(logger *log.Logger, request *http.Request, recovered any) {
	logger.Printf(
		"%s %s: panic: %s\n%s", request.Method, request.URL.Path, panicText(recovered), panicFrames(),
	)
}

func panicText(recovered any) string {
	switch value := recovered.(type) {
	case error:
		return describeCause(value)
	case string:
		return value
	default:
		return fmt.Sprintf("value of type %T", value)
	}
}

// panicFrames lists the panicking call stack by function and file, without argument values.
func panicFrames() string {
	counters := make([]uintptr, 64)
	frames := runtime.CallersFrames(counters[:runtime.Callers(1, counters)])
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
