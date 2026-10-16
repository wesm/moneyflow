package tuidriver_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/tui"
	"github.com/wesm/moneyflow/internal/uitest"
	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
	"github.com/wesm/moneyflow/internal/uitest/tuidriver"
)

// stepTimeout bounds one rendered transition, matching Execute's checkpoints.
// A budget for a whole scenario fails on slow race-detector runners even when
// every individual step is healthy.
const stepTimeout = 5 * time.Second

func waitFor(t *testing.T, driver *tuidriver.Driver, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), stepTimeout)
	defer cancel()
	require.NoError(t, driver.Wait(ctx, text))
}

func TestTUICommitsThroughActualEventLoop(t *testing.T) {
	ctx := t.Context()
	run, err := scenario.Open(ctx, uitest.TestRoot(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, run.Close()) }()
	driver, err := tuidriver.Start(ctx, run.Service, app.NewSession(), tui.Options{Now: run.Provider.Now, ColorMode: tui.ColorModeNone})
	require.NoError(t, err)
	defer func() { require.NoError(t, driver.Close()) }()
	waitFor(t, driver, "Example Shop")
	require.NoError(t, driver.Key("p"))
	waitFor(t, driver, "Choose time")
	require.NoError(t, driver.Text("2026-09"))
	require.NoError(t, driver.Key("enter"))
	waitFor(t, driver, "Sep 2026")
	require.NoError(t, driver.Key("f"))
	waitFor(t, driver, "Show hidden")
	for range 2 {
		require.NoError(t, driver.Key("tab"))
	}
	require.NoError(t, driver.Key("space"))
	for range 2 {
		require.NoError(t, driver.Key("tab"))
	}
	require.NoError(t, driver.Key("enter"))
	require.NoError(t, driver.Key("m"))
	waitFor(t, driver, "2 transactions affected")
	require.NoError(t, driver.Text("Dest"))
	waitFor(t, driver, "Destination Shop")
	require.NoError(t, driver.Key("enter"))
	waitFor(t, driver, "Pending: 1")
	before, err := run.Observe(ctx)
	require.NoError(t, err)
	require.NoError(t, scenario.CheckTargets(before, []string{"current-a", "current-b"}))
	assert.Equal(t, "Example Shop", before.Rows["current-a"].Merchant)
	require.NoError(t, run.Provider.SetFault(uitest.FaultBlock))
	require.NoError(t, driver.Key("w"))
	waitFor(t, driver, "Pending Changes")
	require.NoError(t, driver.Key("enter"))
	waitFor(t, driver, "Monarch Write")
	require.Eventually(t, func() bool {
		for _, call := range run.Provider.State().Calls {
			if call.Method == "update" {
				return true
			}
		}
		return false
	}, stepTimeout, time.Millisecond)
	run.Provider.Release()
	waitFor(t, driver, "Provider write complete")
	after, err := run.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Destination Shop", after.Rows["current-a"].Merchant)
	assert.Equal(t, "Example Shop", after.Rows["older"].Merchant)
	assert.Equal(t, 1, after.SnapshotCalls)
	require.NoError(t, driver.Resize(60, 16))
	waitFor(t, driver, "80")
	require.NoError(t, driver.Resize(120, 40))
	waitFor(t, driver, "Destination Shop")
}

type delayedCleanup struct {
	*uitest.Provider
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (source *delayedCleanup) Writer(context.Context, bool) (provider.Writer, provider.SessionFingerprint, error) {
	return source, "scenario-session", nil
}

func (source *delayedCleanup) UpdateTransaction(ctx context.Context, _ provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
	source.started <- struct{}{}
	<-ctx.Done()
	source.canceled <- struct{}{}
	<-source.release
	return provider.TransactionUpdateResult{}, ctx.Err()
}

func TestCloseWaitsForInflightCommandCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run, err := scenario.Open(ctx, uitest.TestRoot(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, run.Close()) }()
	source := &delayedCleanup{Provider: run.Provider, started: make(chan struct{}, 2), canceled: make(chan struct{}, 2), release: make(chan struct{})}
	require.NoError(t, run.Service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2, Renderer: "tui", InstanceID: "cleanup-test", Now: source.Now}))
	require.NoError(t, run.Stage(ctx, scenario.FilteredSession(), "category", ""))
	driver, err := tuidriver.Start(ctx, run.Service, scenario.FilteredSession(), tui.Options{Now: run.Provider.Now, ColorMode: tui.ColorModeNone})
	require.NoError(t, err)
	require.NoError(t, driver.Wait(ctx, "Pending: 1"))
	require.NoError(t, driver.Key("w"))
	require.NoError(t, driver.Wait(ctx, "Pending Changes"))
	require.NoError(t, driver.Key("enter"))
	require.NoError(t, driver.Wait(ctx, "Monarch Write"))
	select {
	case <-source.started:
	case <-ctx.Done():
		t.Fatal("write did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- driver.Close() }()
	select {
	case <-source.canceled:
	case <-ctx.Done():
		t.Fatal("write did not observe cancellation")
	}
	select {
	case err := <-closed:
		close(source.release)
		t.Fatalf("Close returned before command cleanup: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(source.release)
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Close did not join the command")
	}
}

func TestRealKeysSelectMatchesCreateExplicitlyAndHideOnlyVisibleRows(t *testing.T) {
	for _, test := range []struct {
		name, key, text, review string
		down                    int
	}{
		{"category partial", "c", "Heal", "Health", 0},
		{"category exact", "c", "Health Extras", "Health Extras", 0},
		{"merchant creation despite partial match", "m", "Dest", "Dest", 1},
		{"category creation despite partial match", "c", "Heal", "Heal", 2},
		{"mixed hidden group", "h", "", "Hide transactions", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			open := scenario.Open
			if test.key == "c" && test.down > 0 {
				open = scenario.OpenLocal
			}
			run, err := open(ctx, uitest.TestRoot(t))
			require.NoError(t, err)
			defer func() { require.NoError(t, run.Close()) }()
			before, err := run.Observe(ctx)
			require.NoError(t, err)
			session := scenario.FilteredSession()
			if test.key == "h" {
				session.ShowHidden = true
			}
			driver, err := tuidriver.Start(ctx, run.Service, session, tui.Options{Now: run.Provider.Now, ColorMode: tui.ColorModeNone})
			require.NoError(t, err)
			defer func() { require.NoError(t, driver.Close()) }()
			waitFor(t, driver, "Example Shop")
			if test.key != "h" {
				require.NoError(t, driver.Key(test.key))
				require.NoError(t, driver.Text("cancel me"))
				require.NoError(t, driver.Key("esc"))
				waitFor(t, driver, "g Group By")
			}
			require.NoError(t, driver.Key(test.key))
			if test.text != "" {
				require.NoError(t, driver.Text(test.text))
				for range test.down {
					require.NoError(t, driver.Key("down"))
				}
				require.NoError(t, driver.Key("enter"))
				if test.key == "c" && test.down > 0 {
					waitFor(t, driver, "Choose Group")
					require.NoError(t, driver.Key("enter"))
				}
			}
			waitFor(t, driver, "Pending: 1")
			pending, err := run.Observe(ctx)
			require.NoError(t, err)
			require.Equal(t, before.Rows, pending.Rows)
			require.Equal(t, []string{"current-a", "current-b"}, pending.Targets)
			require.NoError(t, driver.Key("u"))
			waitFor(t, driver, "Redo")
			undone, err := run.Observe(ctx)
			require.NoError(t, err)
			require.Zero(t, undone.Pending)
			require.NoError(t, driver.Key("U"))
			waitFor(t, driver, "Pending: 1")
			require.NoError(t, driver.Key("w"))
			waitFor(t, driver, "Pending Changes")
			waitFor(t, driver, test.review)
		})
	}
}
