package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// EditorChoice is one active stable entity exposed to renderer-owned selectors.
type EditorChoice struct {
	ID        domain.EntityID
	Label     string
	ParentID  domain.EntityID
	Protected bool
	// TransactionCount is the number of committed, non-deleted transactions using the choice.
	TransactionCount int
	// InUse reports, with pending edits applied, that a transaction uses the merchant or
	// category, or that a category belongs to the group. Deleting a category or group in use
	// requires a replacement.
	InUse bool
}

// EditorCatalog contains detached active entities needed by editing interfaces.
type EditorCatalog struct {
	Merchants  []EditorChoice
	Categories []EditorChoice
	Groups     []EditorChoice
}

// EditorCatalog returns active profile choices without exposing the effective snapshot.
func (service *Service) EditorCatalog() (EditorCatalog, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return EditorCatalog{}, errors.New("editor catalog is unavailable")
	}
	return editorCatalog(snapshot), nil
}

// EditorCatalogAt refreshes, revision-checks, and derives choices from one cache generation.
func (service *Service) EditorCatalogAt(
	ctx context.Context,
	expected uint64,
) (EditorCatalog, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return EditorCatalog{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return EditorCatalog{}, mapAppError(err, service.Revision())
	}
	if snapshot.Revision != expected {
		return EditorCatalog{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("editor catalog revision is stale"),
		)
	}
	return editorCatalog(snapshot), nil
}

// committedUsage counts committed, non-deleted transactions per merchant, category, and group.
// Counts follow the committed view so they match grouped results, which stay committed until
// the pending edits are committed.
type committedUsage struct {
	merchants  map[domain.EntityID]int
	categories map[domain.EntityID]int
	groups     map[domain.EntityID]int
}

func countCommittedUsage(committed domain.CommittedProfile) committedUsage {
	groupOf := make(map[domain.EntityID]domain.EntityID, len(committed.Categories))
	for _, category := range committed.Categories {
		groupOf[category.ID] = category.GroupID
	}
	usage := committedUsage{
		merchants:  make(map[domain.EntityID]int),
		categories: make(map[domain.EntityID]int),
		groups:     make(map[domain.EntityID]int),
	}
	for _, transaction := range committed.Transactions {
		usage.merchants[transaction.MerchantID]++
		usage.categories[transaction.CategoryID]++
		usage.groups[groupOf[transaction.CategoryID]]++
	}
	return usage
}

// effectiveUse marks what the pending profile still uses, by the rules that make a delete
// require a replacement: categoryHasTransactions and groupHasCategories.
type effectiveUse struct {
	merchants  map[domain.EntityID]bool
	categories map[domain.EntityID]bool
	groups     map[domain.EntityID]bool
}

func markEffectiveUse(effective domain.CommittedProfile) effectiveUse {
	use := effectiveUse{
		merchants:  make(map[domain.EntityID]bool),
		categories: make(map[domain.EntityID]bool),
		groups:     make(map[domain.EntityID]bool),
	}
	for _, transaction := range effective.Transactions {
		use.merchants[transaction.MerchantID] = true
		use.categories[transaction.CategoryID] = true
	}
	for _, category := range effective.Categories {
		if !category.Retired {
			use.groups[category.GroupID] = true
		}
	}
	return use
}

func editorCatalog(snapshot EffectiveSnapshot) EditorCatalog {
	catalog := EditorCatalog{}
	usage := countCommittedUsage(snapshot.Committed)
	use := markEffectiveUse(snapshot.Effective)
	for _, merchant := range snapshot.Effective.Merchants {
		if !merchant.Retired {
			catalog.Merchants = append(catalog.Merchants, EditorChoice{
				ID: merchant.ID, Label: merchant.Label,
				TransactionCount: usage.merchants[merchant.ID], InUse: use.merchants[merchant.ID],
			})
		}
	}
	for _, category := range snapshot.Effective.Categories {
		if !category.Retired {
			catalog.Categories = append(catalog.Categories, EditorChoice{
				ID: category.ID, Label: category.Label, ParentID: category.GroupID,
				Protected: category.Protected, TransactionCount: usage.categories[category.ID],
				InUse: use.categories[category.ID],
			})
		}
	}
	for _, group := range snapshot.Effective.Groups {
		if !group.Retired {
			catalog.Groups = append(catalog.Groups, EditorChoice{
				ID: group.ID, Label: group.Label, Protected: group.Protected,
				TransactionCount: usage.groups[group.ID], InUse: use.groups[group.ID],
			})
		}
	}
	sortEditorChoices(catalog.Merchants, false)
	sortEditorChoices(catalog.Categories, true)
	sortEditorChoices(catalog.Groups, false)
	return catalog
}

func sortEditorChoices(choices []EditorChoice, protectedFirst bool) {
	sort.Slice(choices, func(left, right int) bool {
		if protectedFirst && choices[left].Protected != choices[right].Protected {
			return choices[left].Protected
		}
		leftLabel := strings.ToLower(choices[left].Label)
		rightLabel := strings.ToLower(choices[right].Label)
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		return choices[left].ID < choices[right].ID
	})
}
