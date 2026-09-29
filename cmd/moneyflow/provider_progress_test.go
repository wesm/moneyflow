package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestCLIProviderProgressThrottlesPagesAndDeduplicatesCompletion(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	progress := newCLIProviderProgress(&output)
	for _, update := range []provider.Progress{
		{Partition: "visible", Fetched: 1_000, Total: 10_000, Attempt: 1},
		{Partition: "visible", Fetched: 1_500, Total: 10_000, Attempt: 1},
		{Partition: "visible", Fetched: 2_000, Total: 10_000, Attempt: 1},
		{Partition: "visible", Fetched: 10_000, Total: 10_000, Attempt: 1},
		{Partition: "visible", Fetched: 10_000, Total: 10_000, Attempt: 1},
	} {
		progress.Observe(update)
	}
	require.NoError(t, progress.Err())
	assert.Len(t, strings.Split(strings.TrimSpace(output.String()), "\n"), 3)
	assert.Contains(t, output.String(), "Fetched 10000 of 10000 visible transactions")
}

func TestCLIProviderProgressNamesTheVerificationRead(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	progress := newCLIProviderProgress(&output)
	progress.Observe(provider.Progress{
		Partition: "visible", Fetched: 1_000, Total: 10_000, Attempt: 1, Pass: 1,
	})
	progress.Observe(provider.Progress{
		Partition: "visible", Fetched: 1_000, Total: 10_000, Attempt: 1, Pass: 2,
	})

	assert.Contains(t, output.String(), "Fetched 1000 of 10000 visible transactions")
	assert.Contains(t, output.String(), "Verified 1000 of 10000 visible transactions")
}
