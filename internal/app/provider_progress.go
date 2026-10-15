package app

import "github.com/wesm/moneyflow/internal/provider"

// refreshProgress combines a provider's progress reports into one count per read. Monarch counts
// its visible and its hidden transactions as separate partitions, each from zero, and then reads
// both again to confirm that nothing changed. Shown as reported, one refresh would appear to
// start over three times.
//
// Within a pass, Fetched continues across partitions. A partition's total is unknown until its
// first page arrives, so the first pass's total can grow; a later pass counts against the
// complete total of the pass before it. The percentage spans every pass and never decreases
// within an attempt, so it holds still while a newly reported total catches up. A new attempt
// rereads changed data from the start, so its count does start over.
type refreshProgress struct {
	attempt int
	pass    int
	passes  int
	// partitions holds the latest report of each partition in the current pass.
	partitions map[string]provider.Progress
	// previousTotal is the complete total of the pass before the current one.
	previousTotal int
	percent       int
}

func (progress *refreshProgress) observe(update provider.Progress) {
	if update.Attempt != progress.attempt {
		*progress = refreshProgress{attempt: update.Attempt}
	}
	pass := max(update.Pass, 1)
	if pass != progress.pass {
		if progress.pass != 0 {
			progress.previousTotal = progress.total()
		}
		progress.pass = pass
		progress.partitions = make(map[string]provider.Progress)
	}
	progress.passes = max(update.Passes, 1, pass)
	progress.partitions[update.Partition] = update
	progress.percent = max(progress.percent, progress.overallPercent())
}

// fetched counts the transactions read in the current pass, across partitions.
func (progress *refreshProgress) fetched() int {
	fetched := 0
	for _, partition := range progress.partitions {
		fetched += partition.Fetched
	}
	return fetched
}

// total is the number of transactions the current pass reads, as far as it is known.
func (progress *refreshProgress) total() int {
	total := 0
	for _, partition := range progress.partitions {
		total += partition.Total
	}
	return max(total, progress.previousTotal)
}

func (progress *refreshProgress) overallPercent() int {
	if progress.pass == 0 {
		return 0
	}
	finished := progress.pass - 1
	total := progress.total()
	if total == 0 {
		return finished * 100 / progress.passes
	}
	fetched := min(max(progress.fetched(), 0), total)
	return (finished*total + fetched) * 100 / (progress.passes * total)
}

// applyTo copies the combined counts into a provider status.
func (progress *refreshProgress) applyTo(status *ProviderStatus) {
	status.Fetched = progress.fetched()
	status.Total = progress.total()
	status.Verifying = progress.pass > 1
	status.Percent = progress.percent
}

func copyProgress(status *ProviderStatus, progress ProviderStatus) {
	status.Fetched = progress.Fetched
	status.Total = progress.Total
	status.Verifying = progress.Verifying
	status.Percent = progress.Percent
}
