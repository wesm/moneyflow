package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderSelectorDirectKeysAndDisabledRows(t *testing.T) {
	t.Parallel()
	selector := newProviderSelector()
	selected := selector.update(keyMessage("y"))
	assert.Equal(t, providerNone, selected.provider)
	assert.Equal(t, providerYNAB, selector.focused())
	assert.Equal(t, "YNAB is not available in Go yet.", selector.status)

	selected = selector.update(keyMessage("s"))
	assert.Equal(t, providerNone, selected.provider)
	assert.Equal(t, providerSimpleFIN, selector.focused())
	assert.Equal(t, "SimpleFIN is not available in Go yet.", selector.status)

	selected = selector.update(keyMessage("m"))
	assert.Equal(t, providerMonarch, selected.provider)

	selected = selector.update(keyMessage("a"))
	assert.Equal(t, providerAmazon, selected.provider)
}

func TestProviderSelectorNavigationKeepsDisabledRowsFocusable(t *testing.T) {
	t.Parallel()
	selector := newProviderSelector()
	assert.Equal(t, providerMonarch, selector.focused())
	selector.update(keyMessage("down"))
	assert.Equal(t, providerAmazon, selector.focused())
	selected := selector.update(keyMessage("enter"))
	assert.Equal(t, providerAmazon, selected.provider)
	selector.update(keyMessage("down"))
	assert.Equal(t, providerYNAB, selector.focused())
	selected = selector.update(keyMessage("enter"))
	assert.Equal(t, providerNone, selected.provider)
	assert.Contains(t, selector.status, "not available")
	selector.update(keyMessage("down"))
	assert.Equal(t, providerSimpleFIN, selector.focused())
	selector.update(keyMessage("down"))
	assert.Equal(t, providerMonarch, selector.focused())
	assert.True(t, selector.update(keyMessage("esc")).back)
}
