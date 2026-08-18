package onboarding

import (
	"time"

	"github.com/wesm/moneyflow/internal/provider"
)

func (coordinator *Coordinator) observeProgress(
	attemptID string,
	startedAt time.Time,
	update provider.Progress,
) {
	phase := "fetching"
	if update.Pass > 1 {
		phase = "verifying"
	}
	elapsed := coordinator.now().Sub(startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	progress := &Progress{
		Phase: phase, Partition: update.Partition,
		Fetched: update.Fetched, Total: update.Total,
		Attempt: update.Attempt, Pass: update.Pass, Elapsed: elapsed,
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state != StateImporting || current.progress != nil && *current.progress == *progress {
		return
	}
	current.progress = progress
	current.stateVersion++
	current.lastActive = coordinator.now()
}
