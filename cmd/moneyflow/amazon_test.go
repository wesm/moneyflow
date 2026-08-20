package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestAmazonImportCommandPresentsProgressAndNextStep(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var got AmazonCommandOptions
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &stderr,
		ImportAmazon: func(_ context.Context, options AmazonCommandOptions, observe func(amazonimport.Progress)) (amazonimport.Snapshot, error) {
			got = options
			observe(amazonimport.Progress{Phase: "parsing", Completed: 2, Total: 3})
			return amazonimport.Snapshot{ProfileID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", State: amazonimport.StateComplete, Result: app.AmazonImportResult{Revision: 4, Inserted: 8, Updated: 2, Retired: 1}}, nil
		},
	})
	command.SetArgs([]string{"provider", "import", "amazon", "/orders", "--profile", "Purchases", "--currency", "USD", "--scale", "2", "--clone-taxonomy-from", "Primary"})

	require.NoError(t, command.Execute())
	assert.Equal(t, "/orders", got.Directory)
	assert.Equal(t, "Purchases", got.Profile)
	assert.Equal(t, amazon.Settings{Currency: "USD", Scale: 2}, got.Settings)
	assert.True(t, got.SettingsConfigured)
	assert.Equal(t, "Primary", got.CloneTaxonomyFrom)
	assert.Contains(t, stderr.String(), "Parsed 2 of 3 files.")
	assert.Equal(t, "Imported 8, updated 2, restored 0, retired 1 Amazon transactions.\nOpen it with: moneyflow tui --profile profile_aaaaaaaaaaaaaaaaaaaaaaaaaa\n", stdout.String())
}

func TestAmazonImportCommandShowsActionableCoordinateOnlyToInitiator(t *testing.T) {
	var stderr bytes.Buffer
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr,
		ImportAmazon: func(context.Context, AmazonCommandOptions, func(amazonimport.Progress)) (amazonimport.Snapshot, error) {
			return amazonimport.Snapshot{}, &amazonimport.Error{
				Code:       amazonimport.CodeImportInvalid,
				Coordinate: amazon.Coordinate{RelativeFilename: "Retail.OrderHistory.2.csv", Record: 9, Column: "Total Owed", Reason: "invalid_money"},
			}
		},
	})
	command.SetArgs([]string{"provider", "import", "amazon", "/orders", "--profile", "Purchases", "--currency", "USD", "--scale", "2"})

	err := command.Execute()
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "Retail.OrderHistory.2.csv: record 9: Total Owed: invalid_money")
	assert.NotContains(t, err.Error(), "Retail.OrderHistory")
}

func TestAmazonImportCommandRequiresCurrencyAndScaleTogether(t *testing.T) {
	called := false
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		ImportAmazon: func(context.Context, AmazonCommandOptions, func(amazonimport.Progress)) (amazonimport.Snapshot, error) {
			called = true
			return amazonimport.Snapshot{}, errors.New("unexpected")
		},
	})
	command.SetArgs([]string{"provider", "import", "amazon", "/orders", "--currency", "USD"})

	err := command.Execute()
	require.ErrorContains(t, err, "--currency and --scale must be provided together")
	assert.False(t, called)
}

func TestAmazonImportCommandCreatesAndInstallsProfile(t *testing.T) {
	homeRoot := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", homeRoot)
	source := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(source, "Retail.OrderHistory.1.csv"),
		[]byte("Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status,ASIN,Currency,Unit Price\n"+
			"example-order,2026-08-19,Example Product,1,12.34,Closed,Delivered,EXAMPLE1,USD,12.34\n"),
		0o600,
	))
	var stdout bytes.Buffer
	command := newRootCommand(IOStreams{In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{}})
	command.SetArgs([]string{"provider", "import", "amazon", source, "--profile", "Purchases", "--currency", "USD", "--scale", "2"})

	require.NoError(t, command.Execute())
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entry, err := catalog.Resolve(context.Background(), "Purchases")
	require.NoError(t, err)
	assert.Equal(t, "amazon", entry.ProviderKind)
	profile, err := sqlite.Open(context.Background(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	state, err := profile.LoadAmazonState(context.Background())
	require.NoError(t, err)
	require.NotNil(t, state.Settings)
	assert.Equal(t, "USD", string(state.Settings.Currency))
	assert.Len(t, state.Snapshot.Committed.Transactions, 1)
	assert.Contains(t, stdout.String(), "moneyflow tui --profile "+entry.ID)
}

func TestAmazonImportCommandPromptsForCreationSettings(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	source := writeAmazonCommandSource(t)
	prompt := &recordingPrompt{answers: []string{"EUR", "2"}}
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Prompt: prompt.Prompt,
	})
	command.SetArgs([]string{"provider", "import", "amazon", source, "--profile", "Purchases"})

	require.NoError(t, command.Execute())
	assert.Equal(t, []promptCall{
		{label: "Import currency [USD]", secret: false},
		{label: "Minor-unit scale [2]", secret: false},
	}, prompt.calls)
}

func TestAmazonImportCommandRollsBackNewProfileOnFailure(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	command := newRootCommand(IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	command.SetArgs([]string{"provider", "import", "amazon", filepath.Join(t.TempDir(), "missing"), "--profile", "Abandoned", "--currency", "USD", "--scale", "2"})

	require.Error(t, command.Execute())
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entries, err := catalog.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func writeAmazonCommandSource(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(source, "Retail.OrderHistory.1.csv"),
		[]byte("Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status,ASIN,Currency,Unit Price\n"+
			"example-order,2026-08-19,Example Product,1,12.34,Closed,Delivered,EXAMPLE1,EUR,12.34\n"),
		0o600,
	))
	return source
}
