package analytics

import (
	"errors"
	"fmt"
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
)

// CrossTabMaxKeys bounds explicitly requested series keys. It matches one loaded browser window.
const CrossTabMaxKeys = 200

var errMinorOverflow = errors.New("minor-unit total overflows int64")

// CrossTabFrameSpec is one ordered set of non-overlapping periods with its own date bounds.
type CrossTabFrameSpec struct {
	Periods []PeriodRange
	// Series collects per-key cells in this frame. Totals are always collected.
	Series bool
}

// CrossTabSpec describes one pass over transactions that share a currency and scale.
type CrossTabSpec struct {
	// Dimension is merchant, category, group, or account.
	Dimension domain.Dimension
	// Keys selects series. Nil collects every key seen; an empty slice collects none.
	Keys   []string
	Frames []CrossTabFrameSpec
}

// CrossTabCell holds exact minor units and counts for one period. Hidden transactions count
// but add no money, as aggregates do.
type CrossTabCell struct {
	Count        int
	HiddenCount  int
	PendingCount int
	In           int64
	Out          int64
}

// Net returns In plus Out. In is never negative and Out never positive, so it cannot overflow.
func (cell CrossTabCell) Net() int64 {
	return cell.In + cell.Out
}

// CrossTabFrame holds one frame's totals and its per-key cells, aligned to the frame's periods.
type CrossTabFrame struct {
	Totals []CrossTabCell
	Series map[string][]CrossTabCell
}

// CrossTab is the exact result of one pass.
type CrossTab struct {
	Frames []CrossTabFrame
	// Labels holds the smallest label seen for each collected key, as Aggregate chooses.
	Labels map[string]string
}

// ComputeCrossTab accumulates every frame in one pass. Each frame applies its own period
// bounds, and every period in a frame appears in the result even when it has no transactions.
func ComputeCrossTab(transactions []domain.Transaction, spec CrossTabSpec) (CrossTab, error) {
	if err := validateCrossTabSpec(spec); err != nil {
		return CrossTab{}, err
	}
	result := newCrossTab(spec)
	var requested map[string]struct{}
	if spec.Keys != nil {
		requested = make(map[string]struct{}, len(spec.Keys))
		for _, key := range spec.Keys {
			requested[key] = struct{}{}
		}
	}
	var partition *moneyPartition
	for _, transaction := range transactions {
		current := moneyPartition{currency: transaction.Amount.Currency, scale: transaction.Amount.Scale}
		if partition == nil {
			partition = &current
		} else if *partition != current {
			return CrossTab{}, errors.New("cross-tab: transactions span more than one currency or scale")
		}
		if err := result.add(transaction, spec, requested); err != nil {
			return CrossTab{}, err
		}
	}
	return result, nil
}

func validateCrossTabSpec(spec CrossTabSpec) error {
	if !spec.Dimension.Valid() || spec.Dimension == domain.DimensionTime {
		return errors.New("cross-tab: dimension must be merchant, category, group, or account")
	}
	if len(spec.Keys) > CrossTabMaxKeys {
		return fmt.Errorf("cross-tab: at most %d series keys", CrossTabMaxKeys)
	}
	for index, frame := range spec.Frames {
		for period := range frame.Periods {
			current := frame.Periods[period]
			if current.Start.Compare(current.End) > 0 {
				return fmt.Errorf("cross-tab: frame %d period %d starts after it ends", index, period)
			}
			if period > 0 && frame.Periods[period-1].End.Compare(current.Start) >= 0 {
				return fmt.Errorf("cross-tab: frame %d periods overlap or are out of order", index)
			}
		}
	}
	return nil
}

func newCrossTab(spec CrossTabSpec) CrossTab {
	result := CrossTab{
		Frames: make([]CrossTabFrame, len(spec.Frames)),
		Labels: make(map[string]string),
	}
	for index, frame := range spec.Frames {
		result.Frames[index].Totals = make([]CrossTabCell, len(frame.Periods))
		if !frame.Series {
			continue
		}
		result.Frames[index].Series = make(map[string][]CrossTabCell, len(spec.Keys))
		for _, key := range spec.Keys {
			result.Frames[index].Series[key] = make([]CrossTabCell, len(frame.Periods))
		}
	}
	return result
}

func (result *CrossTab) add(
	transaction domain.Transaction, spec CrossTabSpec, requested map[string]struct{},
) error {
	key, label := dimensionValue(transaction, spec.Dimension)
	_, wanted := requested[key]
	collect := requested == nil || wanted
	for index, frame := range spec.Frames {
		period := periodIndex(frame.Periods, transaction.Date)
		if period < 0 {
			continue
		}
		state := &result.Frames[index]
		if err := addToCell(&state.Totals[period], transaction); err != nil {
			return fmt.Errorf("cross-tab: frame %d total: %w", index, err)
		}
		if !frame.Series || !collect {
			continue
		}
		cells, exists := state.Series[key]
		if !exists {
			cells = make([]CrossTabCell, len(frame.Periods))
			state.Series[key] = cells
		}
		if err := addToCell(&cells[period], transaction); err != nil {
			return fmt.Errorf("cross-tab: frame %d %q: %w", index, label, err)
		}
		if existing, seen := result.Labels[key]; !seen || label < existing {
			result.Labels[key] = label
		}
	}
	return nil
}

// periodIndex finds the period containing date by binary search, or returns -1.
func periodIndex(periods []PeriodRange, date domain.Date) int {
	index := sort.Search(len(periods), func(index int) bool {
		return periods[index].End.Compare(date) >= 0
	})
	if index == len(periods) || periods[index].Start.Compare(date) > 0 {
		return -1
	}
	return index
}

func addToCell(cell *CrossTabCell, transaction domain.Transaction) error {
	cell.Count++
	if transaction.Pending {
		cell.PendingCount++
	}
	if transaction.Hidden {
		cell.HiddenCount++
		return nil
	}
	var err error
	switch amount := transaction.Amount.Minor; {
	case amount > 0:
		cell.In, err = addMinor(cell.In, amount)
	case amount < 0:
		cell.Out, err = addMinor(cell.Out, amount)
	}
	return err
}

func addMinor(total, amount int64) (int64, error) {
	sum := total + amount
	if (amount > 0 && sum < total) || (amount < 0 && sum > total) {
		return 0, errMinorOverflow
	}
	return sum, nil
}
