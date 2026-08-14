package app

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// BuildFoldPlan captures one freshly replayed active prefix for atomic commit.
func BuildFoldPlan(snapshot EffectiveSnapshot, reviewedRevision uint64) (store.FoldPlan, error) {
	if reviewedRevision != snapshot.Revision {
		return store.FoldPlan{}, errors.New("build fold plan: reviewed revision is stale")
	}
	replayed, err := Replay(domain.ProfileSnapshot{
		Revision:    snapshot.Revision,
		Cursor:      snapshot.Cursor,
		Committed:   snapshot.Committed,
		Journal:     snapshot.Journal,
		KnownDrills: snapshot.KnownDrills,
	})
	if err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: %w", err)
	}
	if !reflect.DeepEqual(replayed.Effective, snapshot.Effective) {
		return store.FoldPlan{}, errors.New("build fold plan: effective state is not a fresh replay")
	}
	knownDrills, err := profilereplay.KnownDrillsForFold(
		snapshot.KnownDrills,
		replayed.Effective,
		replayed.Journal[:replayed.Cursor],
	)
	if err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: known drills: %w", err)
	}
	plan := store.FoldPlan{
		ReviewedRevision: reviewedRevision,
		Effective:        replayed.Effective.Clone(),
		KnownDrills:      knownDrills,
		ActiveOperationIDs: make(
			[]string,
			replayed.Cursor,
		),
	}
	for index := range replayed.Cursor {
		plan.ActiveOperationIDs[index] = replayed.Journal[index].ID
	}
	if err = plan.Validate(reviewedRevision); err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: %w", err)
	}
	return plan, nil
}
