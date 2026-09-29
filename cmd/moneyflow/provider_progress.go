package main

import (
	"fmt"
	"io"
	"sync"

	"github.com/wesm/moneyflow/internal/provider"
)

type cliProviderProgress struct {
	mu     sync.Mutex
	output io.Writer
	last   provider.Progress
	wrote  bool
	err    error
}

func newCLIProviderProgress(output io.Writer) *cliProviderProgress {
	return &cliProviderProgress{output: output}
}

func (progress *cliProviderProgress) Observe(update provider.Progress) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if progress.err != nil || !progress.shouldWrite(update) {
		return
	}
	verb := "Fetched"
	if update.Pass > 1 {
		verb = "Verified"
	}
	_, progress.err = fmt.Fprintf(
		progress.output,
		"%s %d of %d %s transactions (attempt %d).\n",
		verb,
		update.Fetched,
		update.Total,
		update.Partition,
		update.Attempt,
	)
	if progress.err == nil {
		progress.last = update
		progress.wrote = true
	}
}

func (progress *cliProviderProgress) shouldWrite(update provider.Progress) bool {
	if progress.wrote && update == progress.last {
		return false
	}
	if !progress.wrote || update.Partition != progress.last.Partition || update.Pass != progress.last.Pass ||
		update.Attempt != progress.last.Attempt || update.Fetched == update.Total {
		return true
	}
	interval := update.Total / 10
	if interval < 1_000 {
		interval = 1_000
	}
	return update.Fetched-progress.last.Fetched >= interval
}

func (progress *cliProviderProgress) Err() error {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	return progress.err
}
