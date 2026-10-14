//go:build unix

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevRejectsInvalidPortsBeforeStartingServers(t *testing.T) {
	trackScript(t, "dev.sh")
	// Stand-ins that fail at once, so a missed check never starts a real server.
	bin := t.TempDir()
	for _, name := range []string{"air", "bun"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 3\n"), 0o700)) //nolint:gosec // Executable fake inside t.TempDir.
	}
	for _, test := range []struct {
		name, web, backend, message string
	}{
		{"equal", "8081", "8081", "DEV_WEB_PORT and DEV_BACKEND_PORT are both 8081"},
		{"equal after leading zero", "8081", "08081", "DEV_WEB_PORT and DEV_BACKEND_PORT are both 8081"},
		{"not a number", "abc", "8081", "DEV_WEB_PORT must be a TCP port from 1 to 65535, got 'abc'"},
		{"zero", "5173", "0", "DEV_BACKEND_PORT must be a TCP port from 1 to 65535, got '0'"},
		{"too large", "5173", "65536", "DEV_BACKEND_PORT must be a TCP port from 1 to 65535, got '65536'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), "bash", "dev.sh")
			command.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DEV_WEB_PORT="+test.web, "DEV_BACKEND_PORT="+test.backend,
				"DEV_HOME="+filepath.Join(t.TempDir(), "dev-home"))
			output, err := command.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, string(output))
			require.Equal(t, 2, exitErr.ExitCode(), string(output))
			require.Contains(t, string(output), test.message)
		})
	}
}
