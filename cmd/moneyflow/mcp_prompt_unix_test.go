//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPPromptWithoutControllingTerminal(t *testing.T) {
	if os.Getenv("MONEYFLOW_TEST_MCP_TERMINAL") == "headless" {
		value, err := controllingTerminalPrompt(context.Background(), "Account password", true)
		require.ErrorContains(t, err, "requires a controlling terminal")
		assert.Empty(t, value)
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestMCPPromptWithoutControllingTerminal$") //nolint:gosec // current test binary, fixed arguments.
	command.Env = append(os.Environ(), "MONEYFLOW_TEST_MCP_TERMINAL=headless")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdin = strings.NewReader("protocol-data-not-a-password\n")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.NotContains(t, string(output), "Account password:")
}
