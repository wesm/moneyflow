package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileNameFormCreatesOnlyAfterEnter(t *testing.T) {
	t.Parallel()
	form, focus := newProfileNameState()
	require.NotNil(t, focus)

	form, name, command := form.update(keyMessage("P"))
	assert.Empty(t, name)
	assert.NotNil(t, command)
	form, name, command = form.update(keyMessage("enter"))
	assert.Equal(t, "P", name)
	assert.Nil(t, command)
	assert.Equal(t, "P", form.input.Value())
}

func TestProfileNameFormRejectsBlankNameLocally(t *testing.T) {
	t.Parallel()
	form, _ := newProfileNameState()
	form, name, _ := form.update(keyMessage("enter"))
	assert.Empty(t, name)
	assert.Equal(t, "Enter a profile name.", form.status)
}

func TestShellCancelAfterCreateRollsBackOnlyThatPristineProfile(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	shell = updateShell(t, shell, keyMessage("a"))
	shell = updateShell(t, shell, keyMessage("m"))
	assert.Equal(t, shellName, shell.screen)
	shell = updateShell(t, shell, keyMessage("P"))
	updated, command := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, command)
	updated, command = shell.Update(command())
	shell = updated.(Shell)
	require.NotNil(t, command)
	shell = updateShell(t, shell, command())
	assert.Equal(t, shellOnboarding, shell.screen)
	assert.Equal(t, 1, state.creates)

	updated, command = shell.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	shell = updated.(Shell)
	require.NotNil(t, command)
	updated, command = shell.Update(command())
	shell = updated.(Shell)
	require.NotNil(t, command)
	assert.Equal(t, state.created.ID, state.lastCancel.ProfileID)
	shell = updateShell(t, shell, command())
	assert.Equal(t, shellSelector, shell.screen)
	assert.Equal(t, "Incomplete profile removed.", shell.status)
	assert.Equal(t, 1, state.cancelNewCalls)
	assert.Equal(t, state.created.ID, state.canceledID)
}

func TestShellNameConflictStaysOnFormWithSafeMessage(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	state.createErr = errors.New("synthetic create failure")
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell = updateShell(t, shell, keyMessage("a"))
	shell = updateShell(t, shell, keyMessage("m"))
	shell = updateShell(t, shell, keyMessage("P"))
	updated, command := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, command)
	shell = updateShell(t, shell, command())
	assert.Equal(t, shellName, shell.screen)
	assert.Contains(t, strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n"), "profile name")
}

func updateShell(t testing.TB, shell Shell, message tea.Msg) Shell {
	t.Helper()
	updated, _ := shell.Update(message)
	return updated.(Shell)
}
