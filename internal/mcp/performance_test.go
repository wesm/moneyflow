package mcp

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMCPPerformance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented jobs")
	}
	service, closeProfile := writeTestService(t, 100_000)
	defer closeProfile()
	ctx := context.Background()
	limit := 1_000

	_, err := searchTransactionsDocument(ctx, service, SearchTransactionsInput{
		Query: "Example Merchant", Limit: &limit,
	})
	require.NoError(t, err)
	assertMCPPerformance(t, "search projection and encoding", 500*time.Millisecond, func() {
		document, projectErr := searchTransactionsDocument(ctx, service, SearchTransactionsInput{
			Query: "Example Merchant", Limit: &limit,
		})
		require.NoError(t, projectErr)
		result, encodeErr := ToolResult(document, false)
		require.NoError(t, encodeErr)
		require.False(t, result.IsError)
		require.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
	})

	assertMCPPerformance(t, "list projection and encoding", 500*time.Millisecond, func() {
		document, projectErr := getTransactionsDocument(ctx, service, GetTransactionsInput{Limit: &limit})
		require.NoError(t, projectErr)
		result, encodeErr := ToolResult(document, false)
		require.NoError(t, encodeErr)
		require.False(t, result.IsError)
		require.LessOrEqual(t, resultContentBytes(t, result), MaxResponseContentBytes)
	})

	targets := make([]string, 100)
	for index := range targets {
		targets[index] = fmt.Sprintf("transaction_%03d", index)
	}
	assertMCPPerformance(t, "100-target dry run", 100*time.Millisecond, func() {
		_, previewErr := categoryMutationDocument(
			ctx, service, targets, "1", "category_b", "", true,
		)
		require.NoError(t, previewErr)
	})
	assertMCPPerformance(t, "100-target staging", 100*time.Millisecond, func() {
		_, mutationErr := categoryMutationDocument(
			ctx, service, targets, "1", "category_b", "", false,
		)
		require.NoError(t, mutationErr)
	})
}

func assertMCPPerformance(t *testing.T, name string, ceiling time.Duration, run func()) {
	t.Helper()
	start := time.Now()
	run()
	duration := time.Since(start)
	require.Less(t, duration, ceiling, "%s took %s", name, duration)
}
