//go:build unix

package scripts

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevBackendOpensTheSyntheticProfileByDefault(t *testing.T) {
	devHome := filepath.Join(t.TempDir(), "dev-home")
	home, args, output := runDev(t, devScript(), "DEV_HOME="+devHome)

	require.Equal(t, devHome, home)
	require.Equal(t, "Local", profileArgument(t, args))
	require.NotContains(t, output, "real profile")
}

func TestDevBackendOpensTheNamedRealProfileFromTheUsualHome(t *testing.T) {
	userHome := filepath.Join(t.TempDir(), "user-home")
	home, args, output := runDev(t, devScript(),
		"DEV_HOME="+filepath.Join(t.TempDir(), "dev-home"),
		"DEV_PROFILE=Example's $HOME Monarch", "MONEYFLOW_HOME="+userHome)

	require.Equal(t, userHome, home)
	require.Equal(t, "Example's $HOME Monarch", profileArgument(t, args))
	require.Contains(t, output, "dev.sh: opening the real profile 'Example's $HOME Monarch'")
	require.Contains(t, output, "written to the provider")
}

func TestMakeDevBackendPassesTheProfileNameUnchanged(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "expanded")
	name := `Example "quoted" ` + "`echo changed`" + ` $HOME $$ $(shell touch ` + marker +
		`) Monarch's`
	userHome := filepath.Join(t.TempDir(), "user-home")
	home, args, _ := runDev(t, makeDevBackend(t, "PROFILE="+name), "MONEYFLOW_HOME="+userHome)

	require.Equal(t, userHome, home)
	require.Equal(t, name, profileArgument(t, args))
	require.NoFileExists(t, marker, "make expanded the profile name")
}

func TestMakeDevBackendIgnoresAnExportedProfile(t *testing.T) {
	for _, test := range []struct {
		name  string
		flags []string
		env   []string
	}{
		{name: "plain"},
		{name: "environment overrides", flags: []string{"-e"}},
		{name: "environment overrides from MAKEFLAGS", env: []string{"MAKEFLAGS=e"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			devHome := t.TempDir()
			arguments := append(test.flags, "DEV_HOME="+devHome)
			environment := append(test.env, "PROFILE=Example Monarch", "DEV_PROFILE=Example Monarch")
			home, args, output := runDev(t, makeDevBackend(t, arguments...), environment...)

			require.Equal(t, devHome, home)
			require.Equal(t, "Local", profileArgument(t, args))
			require.NotContains(t, output, "real profile")
		})
	}
}

func devScript() []string {
	return []string{"bash", "dev.sh", "backend"}
}

// makeDevBackend returns a `make dev-backend` command for the repository Makefile. The tests
// that use it give DEV_HOME an existing directory or a real profile, so make never seeds one.
func makeDevBackend(t *testing.T, arguments ...string) []string {
	t.Helper()
	trackScript(t, "../Makefile")
	return append([]string{"make", "--no-print-directory", "-C", "..", "dev-backend"}, arguments...)
}

// runDev runs a command that starts `dev.sh backend` with a stand-in air that records the profile
// root and arguments the Go server would get, then exits. Like air, the stand-in joins the server
// arguments with spaces and runs them with /bin/sh -c.
func runDev(t *testing.T, command []string, environment ...string) (
	home string, args []string, output string,
) {
	t.Helper()
	trackScript(t, "dev.sh")
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "air-call")
	air := "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\n" +
		"exec /bin/sh -c \"server $*\"\n"
	server := "#!/bin/sh\nprintf '%s\\n' \"${MONEYFLOW_HOME-unset}\" \"$@\" > \"$AIR_RECORD\"\n"
	for name, script := range map[string]string{"air": air, "server": server} {
		//nolint:gosec // Executable fake inside t.TempDir.
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700))
	}

	//nolint:gosec // Fixed test commands.
	run := exec.CommandContext(t.Context(), command[0], command[1:]...)
	run.Env = withoutVariables(os.Environ(), "MONEYFLOW_HOME", "PROFILE", "DEV_PROFILE", "MAKEFLAGS")
	ports := freePorts(t, 2)
	run.Env = append(run.Env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AIR_RECORD="+record,
		"DEV_WEB_PORT="+ports[0], "DEV_BACKEND_PORT="+ports[1])
	run.Env = append(run.Env, environment...)
	combined, err := run.CombinedOutput()
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

func withoutVariables(environment []string, names ...string) []string {
	kept := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(names, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// freePorts returns count different free ports. Every listener stays open until all the ports
// are chosen: the system can hand out a port again as soon as its listener closes, and dev.sh
// refuses two equal ports.
func freePorts(t *testing.T, count int) []string {
	t.Helper()
	var config net.ListenConfig
	listeners := make([]net.Listener, count)
	ports := make([]string, count)
	for index := range listeners {
		listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners[index] = listener
		ports[index] = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		require.NoError(t, listener.Close())
	}
	return ports
}
