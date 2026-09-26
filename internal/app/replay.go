package app

import (
	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
)

// EffectiveSnapshot retains the committed base, active replay, and complete review journal.
type EffectiveSnapshot = profilereplay.EffectiveSnapshot

// Replay is the reference implementation for committed state plus the active journal prefix.
func Replay(snapshot domain.ProfileSnapshot) (EffectiveSnapshot, error) {
	return profilereplay.Replay(snapshot)
}

// ApplyOperation clones and applies one already-persisted deterministic forward operation.
func ApplyOperation(
	committed domain.CommittedProfile,
	operation domain.Operation,
) (domain.CommittedProfile, error) {
	return profilereplay.ApplyOperation(committed, operation)
}
