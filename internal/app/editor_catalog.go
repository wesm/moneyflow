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

func editorCatalog(snapshot EffectiveSnapshot) EditorCatalog {
	catalog := EditorCatalog{}
	for _, merchant := range snapshot.Effective.Merchants {
		if !merchant.Retired {
			catalog.Merchants = append(catalog.Merchants, EditorChoice{
				ID: merchant.ID, Label: merchant.Label,
			})
		}
	}
	for _, category := range snapshot.Effective.Categories {
		if !category.Retired {
			catalog.Categories = append(catalog.Categories, EditorChoice{
				ID: category.ID, Label: category.Label, ParentID: category.GroupID,
				Protected: category.Protected,
			})
		}
	}
	for _, group := range snapshot.Effective.Groups {
		if !group.Retired {
			catalog.Groups = append(catalog.Groups, EditorChoice{
				ID: group.ID, Label: group.Label, Protected: group.Protected,
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
