package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/diagnostic"
)

// Server failures reach the browser as generic problems, so the cause goes to the operator log.
// Log lines name the request method and path only: never the query string, headers, or body,
// which can carry mutation tokens, credentials, or financial data.

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

func logServerFailure(logger *log.Logger, request *http.Request, problem *Problem, cause error) {
	line := fmt.Sprintf("%s %s: %d %s", request.Method, request.URL.Path, problem.Status, problem.Code)
	if cause != nil {
		line += ": " + diagnostic.DescribeCause(cause)
	}
	logger.Print(line)
}

func logPanic(logger *log.Logger, request *http.Request, recovered any) {
	logger.Printf(
		"%s %s: panic: %s\n%s", request.Method, request.URL.Path,
		diagnostic.PanicText(recovered), diagnostic.PanicFrames(),
	)
}
