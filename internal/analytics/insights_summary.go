package analytics

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
)

type netEntry struct {
	key   string
	label string
	net   int64
}

func buildKPIs(query domain.QuerySpec, ranges Ranges, crossTab CrossTab) (InsightsKPIs, error) {
	var kpis InsightsKPIs
	if totals := crossTab.Frames[frameScope].Totals; len(totals) > 0 {
		kpis.Scope = insightsTotals(totals[0])
	}
	if ranges.Comparison != nil {
		comparison := insightsTotals(crossTab.Frames[frameComparison].Totals[0])
		if err := kpis.compare(comparison); err != nil {
			return InsightsKPIs{}, err
		}
	}
	var err error
	if len(query.Drilldowns) > 0 {
		kpis.AveragePerTransaction = averagePerTransaction(kpis.Scope)
	} else if query.Search == "" {
		kpis.SavingsRateTenths, err = SavingsRateTenths(kpis.Scope.Net, kpis.Scope.In)
	}
	return kpis, err
}

// compare sets the comparison totals and the income, spending, and net changes.
func (kpis *InsightsKPIs) compare(comparison InsightsTotals) error {
	income, err := IncomeChange(kpis.Scope.In, comparison.In)
	if err != nil {
		return fmt.Errorf("income change: %w", err)
	}
	spending, err := SpendingChange(kpis.Scope.Out, comparison.Out)
	if err != nil {
		return fmt.Errorf("spending change: %w", err)
	}
	net, err := NetChange(kpis.Scope.Net, comparison.Net)
	if err != nil {
		return fmt.Errorf("net change: %w", err)
	}
	kpis.Comparison = &comparison
	kpis.IncomeChange, kpis.SpendingChange, kpis.NetChange = &income, &spending, &net
	return nil
}

func insightsTotals(cell CrossTabCell) InsightsTotals {
	return InsightsTotals{
		In: cell.In, Out: cell.Out, Net: cell.Net(),
		Count: cell.Count, HiddenCount: cell.HiddenCount, PendingCount: cell.PendingCount,
	}
}

// SavingsRateTenths returns Net ÷ In in tenths of a percent, with the magnitude rounded half
// up. It returns nil when In is not positive. The rate is negative when Net is.
func SavingsRateTenths(net, income int64) (*int, error) {
	if income <= 0 {
		return nil, nil
	}
	tenths, err := ratioHalfUp(absMinor(net), absMinor(income), 1000)
	if err != nil {
		return nil, fmt.Errorf("savings rate: %w", err)
	}
	if net < 0 {
		tenths = -tenths
	}
	return &tenths, nil
}

// averagePerTransaction rounds Net ÷ count half away from zero to the minor unit.
func averagePerTransaction(totals InsightsTotals) *int64 {
	// HiddenCount never exceeds Count.
	count := uint64(totals.Count - totals.HiddenCount) //nolint:gosec // see above
	if count == 0 {
		return nil
	}
	magnitude := absMinor(totals.Net)
	quotient, remainder := magnitude/count, magnitude%count
	if remainder >= count-remainder {
		quotient++
	}
	average := int64(quotient) //nolint:gosec // |Net| ÷ count never exceeds |Net|.
	if totals.Net < 0 {
		average = -average
	}
	return &average
}

func cashflow(periods []PeriodRange, totals []CrossTabCell) []InsightsPeriod {
	result := make([]InsightsPeriod, len(periods))
	for index, period := range periods {
		result[index] = InsightsPeriod{
			Period: period.Period, Label: periodLabel(period.Period),
			Start: period.Start, End: period.End, Partial: period.Partial,
			In: totals[index].In, Out: totals[index].Out, Net: totals[index].Net(),
			Count: totals[index].Count,
		}
	}
	return result
}

func calendar(days []PeriodRange, totals []CrossTabCell) []CalendarDay {
	if len(days) == 0 {
		return nil
	}
	result := make([]CalendarDay, len(days))
	for index, day := range days {
		result[index] = CalendarDay{Date: day.Start, In: totals[index].In, Out: totals[index].Out}
	}
	return result
}

func scopeComposition(filtered []domain.Transaction, plan insightsPlan) (Composition, error) {
	bounds, ok := plan.scopeBounds()
	if !ok {
		return Composition{}, nil
	}
	compositions, err := compositions(filtered, plan.grouping, []PeriodRange{
		{Start: bounds.Start, End: bounds.End},
	})
	if err != nil {
		return Composition{}, err
	}
	return compositions[0], nil
}

func periodCompositions(
	filtered []domain.Transaction, context []PeriodRange,
) ([]PeriodComposition, error) {
	byPeriod, err := compositions(filtered, domain.DimensionGroup, context)
	if err != nil {
		return nil, err
	}
	result := make([]PeriodComposition, len(context))
	for index, period := range context {
		result[index] = PeriodComposition{Period: period.Period, Composition: byPeriod[index]}
	}
	return result, nil
}

// compositions ranks every key of one dimension inside each period.
func compositions(
	filtered []domain.Transaction, dimension domain.Dimension, periods []PeriodRange,
) ([]Composition, error) {
	crossTab, err := ComputeCrossTab(filtered, CrossTabSpec{
		Dimension: dimension, Frames: []CrossTabFrameSpec{{Periods: periods, Series: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("composition: %w", err)
	}
	keys := make([]string, 0, len(crossTab.Frames[0].Series))
	for key := range crossTab.Frames[0].Series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Composition, len(periods))
	for index := range periods {
		entries := make([]netEntry, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, netEntry{
				key: key, label: crossTab.Labels[key],
				net: crossTab.Frames[0].Series[key][index].Net(),
			})
		}
		if result[index], err = composeEntries(entries); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// composeEntries ranks rows by net share within each sign. Shares use the same denominators
// as the table's Share column, so the two always agree.
func composeEntries(entries []netEntry) (Composition, error) {
	var spending, income []netEntry
	for _, entry := range entries {
		switch {
		case entry.net < 0:
			spending = append(spending, entry)
		case entry.net > 0:
			income = append(income, entry)
		}
	}
	spendingEntries, err := rankEntries(spending)
	if err != nil {
		return Composition{}, err
	}
	incomeEntries, err := rankEntries(income)
	if err != nil {
		return Composition{}, err
	}
	return Composition{Spending: spendingEntries, Income: incomeEntries}, nil
}

func rankEntries(entries []netEntry) ([]CompositionEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	sortEntries(entries)
	denominator, err := shareDenominator(entries)
	if err != nil {
		return nil, err
	}
	result := make([]CompositionEntry, 0, min(len(entries), compositionTop+1))
	for index, entry := range entries {
		if index == compositionTop {
			return appendOthers(result, entries[index:], denominator)
		}
		share, err := ratioHalfUp(absMinor(entry.net), denominator, 1000)
		if err != nil {
			return nil, fmt.Errorf("composition share: %w", err)
		}
		result = append(result, CompositionEntry{
			Key: entry.key, Label: entry.label, Net: entry.net, ShareTenths: share,
		})
	}
	return result, nil
}

// sortEntries orders by magnitude, largest first, then by label and key for stable output.
func sortEntries(entries []netEntry) {
	sort.Slice(entries, func(left, right int) bool {
		leftMagnitude, rightMagnitude := absMinor(entries[left].net), absMinor(entries[right].net)
		if leftMagnitude != rightMagnitude {
			return leftMagnitude > rightMagnitude
		}
		if entries[left].label != entries[right].label {
			return entries[left].label < entries[right].label
		}
		return entries[left].key < entries[right].key
	})
}

func shareDenominator(entries []netEntry) (uint64, error) {
	var denominator uint64
	for _, entry := range entries {
		magnitude := absMinor(entry.net)
		if math.MaxUint64-denominator < magnitude {
			return 0, errors.New("composition: share denominator overflow")
		}
		denominator += magnitude
	}
	return denominator, nil
}

func appendOthers(
	result []CompositionEntry, rest []netEntry, denominator uint64,
) ([]CompositionEntry, error) {
	var net int64
	for _, entry := range rest {
		var err error
		if net, err = addMinor(net, entry.net); err != nil {
			return nil, fmt.Errorf("composition others: %w", err)
		}
	}
	share, err := ratioHalfUp(absMinor(net), denominator, 1000)
	if err != nil {
		return nil, fmt.Errorf("composition others share: %w", err)
	}
	others := CompositionEntry{
		Label: fmt.Sprintf("%d others", len(rest)), Net: net, ShareTenths: share, Others: len(rest),
	}
	return append(result, others), nil
}
