package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretInputShowsOneBulletPerCharacterAndNeverPlaintext(t *testing.T) {
	t.Parallel()
	input, focus := newSecretInput("Monarch password")
	require.NotNil(t, focus)
	for _, character := range "synthetic-secret" {
		input, _ = input.Update(keyMessage(string(character)))
	}
	rendered := input.View()
	assert.NotContains(t, rendered, "synthetic-secret")
	assert.Equal(t, len([]rune("synthetic-secret")), strings.Count(rendered, "•"))
	assert.NotContains(t, fmt.Sprintf("%#v", input), "synthetic-secret")
	assert.NotContains(t, fmt.Sprintf("%v", input), "synthetic-secret")
}

func TestSecretInputClearRemovesValueAndMask(t *testing.T) {
	t.Parallel()
	input, _ := newSecretInput("Password")
	input.SetValue("temporary")
	input.Clear()
	assert.Empty(t, input.Value())
	assert.NotContains(t, input.View(), "•")
}

func TestSecretInputMasksBracketedPaste(t *testing.T) {
	t.Parallel()
	input, _ := newSecretInput("Password")
	input, _ = input.Update(tea.PasteMsg{Content: "pasted-secret"})
	assert.Equal(t, "pasted-secret", input.Value())
	assert.NotContains(t, input.View(), "pasted-secret")
	assert.Equal(t, len("pasted-secret"), strings.Count(input.View(), "•"))
}
