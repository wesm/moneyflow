package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func runCSVCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	command := newRootCommand(IOStreams{In: strings.NewReader(""), Out: &output, Err: &output})
	command.SetArgs(append([]string{"provider", "import"}, args...))
	err := command.Execute()
	return output.String(), err
}

func TestCSVCommandCreatesAndReimportsProfile(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "any-name.csv")
	require.NoError(t, os.WriteFile(source, []byte("Transaction Date,Description,Amount\n09/01/2026,Example Shop,-12.34\n"), 0o600))
	output, err := runCSVCommand(t, "institution", "chase_credit", source, "--profile", "Bank exports")
	require.NoError(t, err)
	assert.Contains(t, output, "Inserted 1")
	assert.Contains(t, output, "moneyflow tui --profile")
	output, err = runCSVCommand(t, "institution", "chase_credit", source, "--profile", "Bank exports")
	require.NoError(t, err)
	assert.Contains(t, output, "1 unchanged")
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entry, err := catalog.Resolve(t.Context(), "Bank exports")
	require.NoError(t, err)
	assert.Equal(t, "csv", entry.ProviderKind)
	profile, err := sqlite.Open(t.Context(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, snapshot.Committed.Transactions, 1)
	assert.Equal(t, int64(-1234), snapshot.Committed.Transactions[0].Amount.Minor)
	assert.Equal(t, "Chase Credit Card", snapshot.Committed.Accounts[0].Label)
}

func TestCSVCommandFailureKeepsEarlierFiles(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chase-a.csv"), []byte("Transaction Date,Description,Amount\n09/01/2026,Example Shop,-1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chase-b.csv"), []byte("bad header\n"), 0o600))
	output, err := runCSVCommand(t, "institution", "chase_credit", dir, "--profile", "Partial")
	require.Error(t, err)
	assert.Contains(t, output, "Inserted 1")
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entry, err := catalog.Resolve(t.Context(), "Partial")
	require.NoError(t, err)
	assert.Equal(t, profilecatalog.StatusLocalOnly, entry.Status)
}

func TestCSVCommandValidatesBeforeCreatingProfile(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "bad.csv")
	require.NoError(t, os.WriteFile(file, []byte("bad header\n"), 0o600))
	_, err := runCSVCommand(t, "institution", "chase_credit", file, "--profile", "Unpublished")
	require.Error(t, err)
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entries, err := catalog.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, entries)
	require.NoError(t, os.WriteFile(file, []byte("Transaction Date,Description,Amount\n"), 0o600))
	_, err = runCSVCommand(t, "institution", "chase_credit", file, "--profile", "Empty", "--account", " ")
	require.Error(t, err)
	_, err = runCSVCommand(t, "institution", "chase_credit", file, "--profile", "Empty")
	require.NoError(t, err)
	entry, err := catalog.Resolve(t.Context(), "Empty")
	require.NoError(t, err)
	assert.Equal(t, profilecatalog.StatusLocalOnly, entry.Status)
	_, err = catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Local", ProviderKind: "local"})
	require.NoError(t, err)
	_, err = runCSVCommand(t, "institution", "chase_credit", file, "--profile", "Local")
	require.ErrorContains(t, err, "not a CSV profile")
}
