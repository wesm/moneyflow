package app

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// AnalyticalRoot names the view that view.back-to with depth 0 reaches.
type AnalyticalRoot struct {
	Mode            domain.ResultMode
	Dimension       domain.Dimension
	TimeGranularity domain.TimeGranularity
}

// rootView reports the view that view.back-to with depth 0 reaches from state. It steps a
// copy of the state's session back to depth 0, reading only the saved frames. A link leaves
// the unit out of a currency-scoped return frame, so a Time root gets the granularity of its
// period, as the transition that restores it does.
func (service *Service) rootView(
	state ViewState, currencies []domain.CurrencyCount,
) (AnalyticalRoot, error) {
	session, err := NewSessionFromViewState(state)
	if err != nil {
		return AnalyticalRoot{}, fmt.Errorf("root view: %w", err)
	}
	session.BackTo(0)
	root := analyticalStateFromSession(session)
	if _, err := service.deriveTimeRows(&root, currencies); err != nil {
		return AnalyticalRoot{}, fmt.Errorf("root view: %w", err)
	}
	return AnalyticalRoot{
		Mode: root.Mode, Dimension: root.Dimension, TimeGranularity: root.TimeGranularity,
	}, nil
}
