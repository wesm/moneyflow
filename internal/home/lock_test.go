package home

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const lockHelperEnvironment = "MONEYFLOW_HOME_LOCK_HELPER"

func TestLifecycleLockAllowsReadersAndRejectsWriter(t *testing.T) {
	root := t.TempDir()
	first, err := TryLock(root, LockProfile, LockShared)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Release()) })

	second, err := TryLock(root, LockProfile, LockShared)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Release()) })

	_, err = TryLock(root, LockProfile, LockExclusive)
	assert.ErrorIs(t, err, ErrLockBusy)
}

func TestProviderConnectLockIsIndependentAndExclusive(t *testing.T) {
	root := t.TempDir()
	profile, err := TryLock(root, LockProfile, LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Release()) })

	connect, err := TryLock(root, LockProviderConnect, LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connect.Release()) })

	_, err = TryLock(root, LockProviderConnect, LockShared)
	assert.ErrorIs(t, err, ErrLockBusy)
	assert.FileExists(t, filepath.Join(root, "profile.lock"))
	assert.FileExists(t, filepath.Join(root, "provider-connect.lock"))
}

func TestExportLockIsIndependentExclusiveAndSequential(t *testing.T) {
	root := t.TempDir()
	profile, err := TryLock(root, LockProfile, LockShared)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Release()) })

	export, err := TryLockExisting(root, LockExport, LockExclusive)
	require.NoError(t, err)
	_, err = TryLockExisting(root, LockExport, LockExclusive)
	assert.ErrorIs(t, err, ErrLockBusy)
	require.NoError(t, export.Release())

	export, err = TryLockExisting(root, LockExport, LockExclusive)
	require.NoError(t, err)
	require.NoError(t, export.Release())
	assert.FileExists(t, filepath.Join(root, "export.lock"))
}

func TestCatalogLockRejectsInvalidNameAndMode(t *testing.T) {
	root := t.TempDir()
	_, err := TryLock(root, LockName(99), LockExclusive)
	require.Error(t, err)
	_, err = TryLock(root, LockCatalog, LockMode(99))
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(root, "99.lock"))
}

func TestLockRejectsRedirectedOrNonregularTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "profile.lock")
	require.NoError(t, os.Mkdir(target, 0o700))
	_, err := TryLock(root, LockProfile, LockExclusive)
	require.Error(t, err)

	require.NoError(t, os.Remove(target))
	if err = os.Symlink(filepath.Join(root, "elsewhere"), target); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	_, err = TryLock(root, LockProfile, LockExclusive)
	require.Error(t, err)
}

func TestLifecycleLockConflictsAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	lock, err := TryLock(root, LockProfile, LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	command := lockHelperCommand(t, root, "try")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.True(t, strings.HasPrefix(string(output), "busy\n"), string(output))
}

func TestLockReleasedWhenProcessDies(t *testing.T) {
	root := t.TempDir()
	command := lockHelperCommand(t, root, "hold")
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)
	_, err = TryLock(root, LockProfile, LockExclusive)
	require.ErrorIs(t, err, ErrLockBusy)

	require.NoError(t, command.Process.Kill())
	require.NoError(t, stdin.Close())
	err = command.Wait()
	var exitError *exec.ExitError
	require.True(t, errors.As(err, &exitError))

	lock, err := TryLock(root, LockProfile, LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestExportLockReleasedWhenProcessDies(t *testing.T) {
	root := t.TempDir()
	command := lockHelperCommandForName(t, root, "hold", "export")
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)
	_, err = TryLockExisting(root, LockExport, LockExclusive)
	require.ErrorIs(t, err, ErrLockBusy)

	require.NoError(t, command.Process.Kill())
	require.NoError(t, stdin.Close())
	err = command.Wait()
	var exitError *exec.ExitError
	require.True(t, errors.As(err, &exitError))

	lock, err := TryLockExisting(root, LockExport, LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv(lockHelperEnvironment) == "" {
		t.Skip("helper process")
	}
	root := os.Getenv("MONEYFLOW_HOME_LOCK_ROOT")
	action := os.Getenv("MONEYFLOW_HOME_LOCK_ACTION")
	name := LockProfile
	if os.Getenv("MONEYFLOW_HOME_LOCK_NAME") == "export" {
		name = LockExport
	}
	lock, err := TryLock(root, name, LockExclusive)
	if errors.Is(err, ErrLockBusy) {
		_, _ = os.Stdout.WriteString("busy\n")
		return
	}
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	defer func() { _ = lock.Release() }()
	_, _ = os.Stdout.WriteString("locked\n")
	if action == "hold" {
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
	}
}

func lockHelperCommand(t *testing.T, root string, action string) *exec.Cmd {
	return lockHelperCommandForName(t, root, action, "profile")
}

func lockHelperCommandForName(t *testing.T, root string, action string, name string) *exec.Cmd {
	t.Helper()
	command := exec.Command( //nolint:gosec // Re-execute this fixed test binary and test name.
		os.Args[0], "-test.run=^TestLockHelperProcess$",
	)
	command.Env = append(os.Environ(),
		lockHelperEnvironment+"=1",
		"MONEYFLOW_HOME_LOCK_ROOT="+root,
		"MONEYFLOW_HOME_LOCK_ACTION="+action,
		"MONEYFLOW_HOME_LOCK_NAME="+name,
	)
	if runtime.GOOS == "windows" {
		command.Env = append(command.Env, "MONEYFLOW_HOME_LOCK_PLATFORM=windows")
	}
	return command
}

func TestTryLockExistingNeverRecreatesMissingRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "removed-profile")

	_, err := TryLockExisting(root, LockProfile, LockShared)
	require.Error(t, err)
	assert.NoDirExists(t, root)
}
