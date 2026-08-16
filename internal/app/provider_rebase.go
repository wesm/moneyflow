package app

import (
	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
)

// RebaseResult is the active journal rewritten over a refreshed committed base.
type RebaseResult = profilereplay.ProviderRebaseResult

// RebaseSummary contains only counts safe for durable status and logs.
type RebaseSummary = profilereplay.ProviderRebaseSummary

// RebaseDetail is ephemeral operation-level renderer context.
type RebaseDetail = profilereplay.ProviderRebaseDetail

// RebaseProviderJournal preserves resolvable user intent over a new provider base.
func RebaseProviderJournal(
	oldBase domain.CommittedProfile,
	newBase domain.CommittedProfile,
	journal []domain.Operation,
	cursor int,
) (RebaseResult, error) {
	return profilereplay.RebaseProviderJournal(oldBase, newBase, journal, cursor)
}
