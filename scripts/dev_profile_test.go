//go:build unix

package scripts

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/fixture"
)

// A fake `go` that does what an interrupted seedprofile leaves behind: it creates the database
// in the target directory, records that it ran, and then fails or hangs.
const fakeSeeder = `#!/bin/sh
for target; do :; done
: > "$target/moneyflow.db"
: > "$FAKE_SEEDER_STARTED"
if [ "$FAKE_SEEDER_MODE" = hang ]; then sleep 60; fi
exit 1
`

func TestDevProfileRetriesAfterFailedSeeding(t *testing.T) {
	home := filepath.Join(t.TempDir(), "tmp", "dev-home")
	output, err := runDevProfile(t, home, fakeSeederEnv(t, "fail")...)
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "seeding failed")
	requireNoDevProfile(t, home)

	output, err = runDevProfile(t, home)
	require.NoError(t, err, string(output))
	requireWorkingDevProfile(t, home)
}

func TestDevProfileRetriesAfterInterruptedSeeding(t *testing.T) {
	home := filepath.Join(t.TempDir(), "tmp", "dev-home")
	environment := fakeSeederEnv(t, "hang")
	started := strings.TrimPrefix(environment[len(environment)-1], "FAKE_SEEDER_STARTED=")
	command := devProfileCommand(t, home, environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, command.Start())
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(started)
		return statErr == nil
	}, 10*time.Second, 10*time.Millisecond, "fake seeder did not start")
	// Ctrl-C signals the whole foreground process group.
	require.NoError(t, syscall.Kill(-command.Process.Pid, syscall.SIGINT))
	err := command.Wait()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 130, exitErr.ExitCode())
	requireNoDevProfile(t, home)

	output, err := runDevProfile(t, home)
	require.NoError(t, err, string(output))
	requireWorkingDevProfile(t, home)
}

func TestDevProfileLeavesExistingHomeUnchanged(t *testing.T) {
	home := filepath.Join(t.TempDir(), "dev-home")
	require.NoError(t, os.Mkdir(home, 0o700))
	marker := filepath.Join(home, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("existing profile"), 0o600))

	// The fake seeder fails if it runs, so success shows that seeding was skipped.
	output, err := runDevProfile(t, home, fakeSeederEnv(t, "fail")...)
	require.NoError(t, err, string(output))
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	content, err := os.ReadFile(marker) //nolint:gosec // Marker inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, "existing profile", string(content))
}

func fakeSeederEnv(t *testing.T, mode string) []string {
	t.Helper()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(fakeSeeder), 0o700)) //nolint:gosec // Executable fake inside t.TempDir.
	return []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"FAKE_SEEDER_MODE=" + mode,
		"FAKE_SEEDER_STARTED=" + filepath.Join(t.TempDir(), "started"),
	}
}

func devProfileCommand(t *testing.T, home string, environment ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, "bash", "dev-profile.sh", home) //nolint:gosec // Fixed script and isolated home.
	command.Env = append(os.Environ(), environment...)
	return command
}

func runDevProfile(t *testing.T, home string, environment ...string) ([]byte, error) {
	t.Helper()
	return devProfileCommand(t, home, environment...).CombinedOutput()
}

func requireNoDevProfile(t *testing.T, home string) {
	t.Helper()
	_, err := os.Stat(home)
	require.True(t, errors.Is(err, os.ErrNotExist), "partial profile left at %s: %v", home, err)
	staging, err := filepath.Glob(home + ".seeding.*")
	require.NoError(t, err)
	require.Empty(t, staging, "staging directory left behind")
}

// requireWorkingDevProfile opens the profile by name, as make dev does, and exports it.
func requireWorkingDevProfile(t *testing.T, home string) {
	t.Helper()
	info, err := os.Stat(home)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	export := filepath.Join(t.TempDir(), "export.jsonl")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "./cmd/moneyflow", "profile", "export", //nolint:gosec // Fixed command and isolated paths.
		"--profile", "Moneyflow", "--output", export)
	command.Dir = ".."
	command.Env = append(os.Environ(), "MONEYFLOW_HOME="+home)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	file, err := os.Open(export) //nolint:gosec // Export inside t.TempDir.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	transactions := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		if record.Type == "transaction" {
			transactions++
		}
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, len(fixture.Demo()), transactions)
}
