//go:build unix

package scripts

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevBackendOpensTheSyntheticProfileByDefault(t *testing.T) {
	devHome := filepath.Join(t.TempDir(), "dev-home")
	home, args, output := runDevBackend(t, "DEV_HOME="+devHome)

	require.Equal(t, devHome, home)
	require.Equal(t, "Moneyflow", profileArgument(t, args))
	require.NotContains(t, output, "real profile")
}

func TestDevBackendOpensTheNamedRealProfileFromTheUsualHome(t *testing.T) {
	userHome := filepath.Join(t.TempDir(), "user-home")
	home, args, output := runDevBackend(t,
		"DEV_HOME="+filepath.Join(t.TempDir(), "dev-home"),
		"DEV_PROFILE=Example's $HOME Monarch", "MONEYFLOW_HOME="+userHome)

	require.Equal(t, userHome, home)
	require.Equal(t, "Example's $HOME Monarch", profileArgument(t, args))
	require.Contains(t, output, "dev.sh: opening the real profile 'Example's $HOME Monarch'")
	require.Contains(t, output, "written to the provider")
}

// runDevBackend runs `dev.sh backend` with a stand-in air that records the profile root and
// arguments the Go server would get, then exits. Like air, the stand-in joins the server
// arguments with spaces and runs them with /bin/sh -c.
func runDevBackend(t *testing.T, environment ...string) (
	home string, args []string, output string,
) {
	t.Helper()
	trackScript(t, "dev.sh")
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "air-call")
	air := "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec /bin/sh -c \"server $*\"\n"
	server := "#!/bin/sh\nprintf '%s\\n' \"${MONEYFLOW_HOME-unset}\" \"$@\" > \"$AIR_RECORD\"\n"
	for name, script := range map[string]string{"air": air, "server": server} {
		//nolint:gosec // Executable fake inside t.TempDir.
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700))
	}

	command := exec.CommandContext(t.Context(), "bash", "dev.sh", "backend")
	command.Env = append(withoutVariable(os.Environ(), "MONEYFLOW_HOME"),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AIR_RECORD="+record,
		"DEV_WEB_PORT="+freePort(t), "DEV_BACKEND_PORT="+freePort(t))
	command.Env = append(command.Env, environment...)
	combined, err := command.CombinedOutput()
	require.NoError(t, err, string(combined))

	recorded, err := os.ReadFile(record) //nolint:gosec // Record inside t.TempDir.
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(recorded), "\n"), "\n")
	return lines[0], lines[1:], string(combined)
}

func profileArgument(t *testing.T, args []string) string {
	t.Helper()
	for index, arg := range args {
		if arg == "--profile" && index+1 < len(args) {
			return args[index+1]
		}
	}
	require.Failf(t, "no --profile argument", "air arguments: %q", args)
	return ""
}

func withoutVariable(environment []string, name string) []string {
	kept := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, name+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

func freePort(t *testing.T) string {
	t.Helper()
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return strconv.Itoa(port)
}
