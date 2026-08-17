package tui

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestRefreshKeyShowsUnboundCapabilityReason(t *testing.T) {
	t.Parallel()

	model := newPersistentModel(t, app.NewSession()).model
	updated, command := model.Update(keyRune('r'))
	model = updated.(Model)

	assert.Nil(t, command)
	assert.False(t, model.provider.refreshing)
	assert.Equal(t, "Connect a provider before refreshing.", model.status)
	statusLine := model.RenderScreen().Frame.PlainLines()[model.height-2]
	assert.Equal(t, "Connect a provider before refreshing.", statusLine[1:len(model.status)+1])
	assert.Empty(t, strings.TrimSpace(statusLine[len(model.status)+1:]))
}

func TestRefreshKeyStartsAsyncAndEscapeCancelsBeforeFold(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := fixture.model
	assert.NotNil(t, model.Init())
	model.cursor = 1
	identity := model.rowIdentity(model.cursor)
	started := make(chan struct{})
	fixture.source.setFetch(func(ctx context.Context, progress provider.ProgressFunc) (domain.ImportSnapshot, error) {
		progress(provider.Progress{Partition: "visible", Fetched: 2, Total: 3, Attempt: 1})
		close(started)
		<-ctx.Done()
		return domain.ImportSnapshot{}, ctx.Err()
	})

	updated, command := model.Update(keyRune('r'))
	model = updated.(Model)
	require.NotNil(t, command)
	assert.True(t, model.provider.refreshing)
	assert.Contains(t, model.status, "Refreshing")

	result := make(chan tea.Msg, 1)
	go func() { result <- model.providerRefreshCommand(true, "")() }()
	<-started
	statusMessage := model.providerStatusCommand(fixture.now)().(providerStatusMsg)
	updated, _ = model.Update(statusMessage)
	model = updated.(Model)
	assert.Contains(t, model.status, "2 of 3")

	updated, cancelCommand := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	assert.Nil(t, cancelCommand)
	assert.Contains(t, model.status, "Cancellation requested")
	updated, _ = model.Update(<-result)
	model = updated.(Model)
	assert.False(t, model.provider.refreshing)
	assert.Equal(t, identity, model.rowIdentity(model.cursor))
	assert.Equal(t, uint64(1), model.provider.status.Generation)
}

func TestProviderRefreshDoesNotBlockInputOrRestoreStaleSelection(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := press(t, fixture.model, keyRune('d'))
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.source.setFetch(func(
		_ context.Context,
		_ provider.ProgressFunc,
	) (domain.ImportSnapshot, error) {
		close(started)
		<-release
		return tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 3), nil
	})
	updated, _ := model.Update(keyRune('r'))
	model = updated.(Model)
	result := make(chan tea.Msg, 1)
	go func(command tea.Cmd) { result <- command() }(model.providerRefreshCommand(true, ""))
	<-started

	interaction := make(chan Model, 1)
	go func(current Model) {
		updatedModel, _ := current.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		current = updatedModel.(Model)
		updatedModel, _ = current.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		interaction <- updatedModel.(Model)
	}(model)
	select {
	case model = <-interaction:
		assert.Equal(t, 1, model.cursor)
		require.Len(t, model.session.SelectedTransactionIDs, 1)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("provider network fetch blocked TUI input")
	}
	selected := model.rowIdentity(model.cursor)
	close(release)
	updated, _ = model.Update(<-result)
	model = updated.(Model)

	assert.Equal(t, selected, model.rowIdentity(model.cursor))
	_, selectedStillVisible := model.session.SelectedTransactionIDs[selected]
	assert.True(t, selectedStillVisible)
	require.Len(t, model.session.SelectedTransactionIDs, 1)
}

func TestRefreshResultClearsExactSelectionAndRestoresCursor(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 10)
	model := press(t, fixture.model, keyRune('d'))
	model.cursor = 4
	identity := model.rowIdentity(model.cursor)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model.cursor = 9
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model.cursor = 4
	fixture.source.setSnapshot(tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 9))

	updated, _ := model.Update(keyRune('r'))
	model = updated.(Model)
	message := model.providerRefreshCommand(true, "")()
	updated, _ = model.Update(message)
	model = updated.(Model)

	assert.Empty(t, model.session.SelectedTransactionIDs)
	assert.Equal(t, app.EmptySelection(), model.selection)
	assert.Equal(t, identity, model.rowIdentity(model.cursor))
	assert.Contains(t, model.status, "Selection cleared")
}

func TestProviderStandingTickStartsOnlyWhenSixHoursOld(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := fixture.model

	updated, command := model.Update(providerScheduleTickMsg{
		at:              fixture.now.Add(6*time.Hour - time.Second),
		timerGeneration: model.provider.timerGeneration,
	})
	model = updated.(Model)
	require.NotNil(t, command)
	updated, refreshCommand := model.Update(command())
	model = updated.(Model)
	assert.False(t, model.provider.refreshing)
	assert.NotNil(t, refreshCommand)

	updated, command = model.Update(providerScheduleTickMsg{
		at: fixture.now.Add(6 * time.Hour), timerGeneration: model.provider.timerGeneration,
	})
	model = updated.(Model)
	require.NotNil(t, command)
	updated, refreshCommand = model.Update(command())
	model = updated.(Model)
	assert.True(t, model.provider.refreshing)
	assert.NotNil(t, refreshCommand)
}

func TestProviderLateProgressStatusCannotStartSecondRefresh(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := fixture.model
	updated, command := model.Update(providerStatusMsg{
		status:   app.ProviderStatus{Generation: 1},
		at:       fixture.now.Add(7 * time.Hour),
		progress: true,
	})
	model = updated.(Model)

	assert.False(t, model.provider.refreshing)
	assert.NotNil(t, command)
}

func TestProviderManualRefreshInvalidatesOutstandingStandingTimer(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := fixture.model
	standingGeneration := model.provider.timerGeneration
	updated, command := model.Update(keyRune('r'))
	model = updated.(Model)
	require.NotNil(t, command)
	require.NotEqual(t, standingGeneration, model.provider.timerGeneration)

	updated, staleCommand := model.Update(providerScheduleTickMsg{
		at: fixture.now.Add(7 * time.Hour), timerGeneration: standingGeneration,
	})
	model = updated.(Model)
	assert.Nil(t, staleCommand)
	assert.True(t, model.provider.refreshing)
}

func TestProviderStatusHealsReconnectAndExplainsOtherConfirmationOwner(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 2)
	model := fixture.model
	fixture.source.setProbeError(provider.NewError(provider.CodeReconnectRequired))
	message := model.providerRefreshCommand(true, "")().(providerRefreshMsg)
	updated, _ := model.Update(message)
	model = updated.(Model)
	assert.Contains(t, model.status, "Reconnect")

	fixture.source.setProbeError(nil)
	fixture.source.setFingerprint("session-b")
	status := model.providerStatusCommand(fixture.now.Add(time.Minute))().(providerStatusMsg)
	updated, _ = model.Update(status)
	model = updated.(Model)
	assert.Empty(t, model.provider.status.Code)
	assert.Contains(t, model.status, "scheduling resumed")

	updated, _ = model.Update(providerStatusMsg{status: app.ProviderStatus{
		Code: provider.CodeDeletionConfirmationRequired, OwnerRenderer: "web",
		OwnerInstanceID: "instance-other",
	}, at: fixture.now.Add(time.Minute), timerGeneration: model.provider.timerGeneration})
	model = updated.(Model)
	assert.Contains(t, model.status, "confirm in the web interface")
	assert.Contains(t, model.status, "press r")
}

func TestProviderRefreshKeepsStableLabelDrillAndEmptiesRetiredDrill(t *testing.T) {
	t.Parallel()

	t.Run("stable label", func(t *testing.T) {
		fixture := newProviderModel(t, 3)
		model := press(t, fixture.model, tea.KeyPressMsg{Code: tea.KeyEnter})
		require.Len(t, model.result.DetailRows, 3)
		state := model.session.ViewState()
		snapshot := tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 3)
		snapshot.Merchants[0].Label = "Renamed Example Merchant"
		fixture.source.setSnapshot(snapshot)

		updated, _ := model.Update(keyRune('r'))
		model = updated.(Model)
		updated, _ = model.Update(model.providerRefreshCommand(true, "")())
		model = updated.(Model)

		assert.Equal(t, state.Current.Drilldowns[0].Key, model.session.Drilldowns[0].Key)
		assert.Len(t, model.result.DetailRows, 3)
		assert.Contains(t, model.displayBreadcrumb(), "Renamed Example Merchant")
	})

	t.Run("retired identity", func(t *testing.T) {
		now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
		initial := tuiProviderSnapshot(t, now, 10)
		initial.Merchants = append(initial.Merchants, domain.ImportEntity{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-keeper", Label: "Keeper Merchant",
		})
		for index := 1; index < len(initial.Transactions); index++ {
			initial.Transactions[index].MerchantExternalID = "merchant-keeper"
		}
		fixture := newProviderModelFromSnapshot(t, initial, now)
		model := fixture.model
		for index, row := range model.result.AggregateRows {
			if row.Label == "Example Merchant" {
				model.cursor = index
				break
			}
		}
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
		retiredID := model.session.Drilldowns[0].Key
		snapshot := initial.Clone()
		snapshot.ObservedAt = fixture.now.Add(time.Minute)
		snapshot.Merchants = snapshot.Merchants[1:]
		snapshot.Transactions = snapshot.Transactions[1:]
		fixture.source.setSnapshot(snapshot)

		updated, _ := model.Update(keyRune('r'))
		model = updated.(Model)
		updated, _ = model.Update(model.providerRefreshCommand(true, "")())
		model = updated.(Model)

		assert.Equal(t, retiredID, model.session.Drilldowns[0].Key)
		assert.Empty(t, model.result.DetailRows)
		assert.Nil(t, model.err)
	})
}

func TestProviderDeletionCandidateOpensOwnedConfirmation(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 10)
	model := fixture.model
	fixture.source.setSnapshot(tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 5))
	updated, _ := model.Update(keyRune('r'))
	model = updated.(Model)
	updated, _ = model.Update(model.providerRefreshCommand(true, "")())
	model = updated.(Model)

	assert.Equal(t, overlayProviderConfirmation, model.overlay)
	assert.NotEmpty(t, model.provider.confirmationToken)
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "Confirm Provider Refresh")

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assert.True(t, model.provider.refreshing)
	assert.NotNil(t, command)
	updated, _ = model.Update(providerRefreshMsg{result: app.ProviderRefreshResult{
		Status: app.ProviderStatus{Generation: 2}, Selection: app.EmptySelection(),
		SelectionDisposition: app.SelectionPreserved,
	}})
	model = updated.(Model)
	assert.Empty(t, model.provider.confirmationToken)
}

func TestProviderConfirmationAndReviewKeepCommitDisabled(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 4)
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, keyRune('h'))
	require.Equal(t, 1, model.pending.ActiveOperations)
	model = press(t, model, keyRune('w'))
	require.Equal(t, overlayReview, model.overlay)
	model = press(t, model, keyRune('c'))
	assert.Equal(t, reviewPhaseSummary, model.review.phase)
	assert.Contains(t, model.review.err, "write-back")
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "stored until write-back")
}

func TestCategoryOverlayConsumesRenameBeforeGlobalRefresh(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	model := press(t, fixture.model, keyRune('C'))
	model.categoryManager.searchFocused = false
	for index, choice := range model.categoryManager.filtered {
		if !choice.Protected {
			model.categoryManager.selected = index
			break
		}
	}
	model = press(t, model, keyRune('r'))

	assert.Equal(t, overlayCategoryManager, model.overlay)
	assert.Equal(t, taxonomyPhaseLabel, model.categoryManager.phase)
	assert.False(t, model.provider.refreshing)
}

type providerModelFixture struct {
	model  Model
	source *tuiProviderSource
	now    time.Time
}

func newProviderModel(t testing.TB, count int) providerModelFixture {
	t.Helper()
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	return newProviderModelFromSnapshot(t, tuiProviderSnapshot(t, now, count), now)
}

func newProviderModelFromSnapshot(
	t testing.TB,
	snapshot domain.ImportSnapshot,
	now time.Time,
) providerModelFixture {
	t.Helper()
	source := &tuiProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: snapshot, fingerprint: "session-a",
	}
	model := newPristineProviderModel(t, source, now)
	return providerModelFixture{model: model, source: source, now: now}
}

func newPristineProviderModel(
	t testing.TB,
	source provider.Source,
	now time.Time,
) Model {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-tui",
		Now: func() time.Time { return now }, Random: &tuiIncrementingReader{},
	}))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	model, err := NewModel(ctx, service, app.NewSession(), Options{
		Theme: ThemeDefault, ColorMode: ColorModeNone, Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	return model
}

func tuiProviderSnapshot(t testing.TB, observedAt time.Time, count int) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	snapshot := domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category-example",
			ParentExternalID: "group-example", Label: "Example Category",
		}},
	}
	for index := range count {
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID: tuiTransactionExternalID(index), AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		})
	}
	return snapshot
}

func tuiTransactionExternalID(index int) string {
	return "transaction-example-" + time.Unix(int64(index), 0).UTC().Format("150405")
}

type tuiProviderSource struct {
	mu          sync.Mutex
	identity    provider.ProfileIdentity
	snapshot    domain.ImportSnapshot
	fingerprint provider.SessionFingerprint
	probeErr    error
	fetch       func(context.Context, provider.ProgressFunc) (domain.ImportSnapshot, error)
}

func (source *tuiProviderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return (*tuiProviderReader)(source), source.fingerprint, nil
}

func (source *tuiProviderSource) Changed(previous provider.SessionFingerprint) (bool, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return previous != source.fingerprint, nil
}

func (source *tuiProviderSource) setFetch(
	fetch func(context.Context, provider.ProgressFunc) (domain.ImportSnapshot, error),
) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fetch = fetch
}

func (source *tuiProviderSource) setSnapshot(snapshot domain.ImportSnapshot) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.snapshot = snapshot.Clone()
	source.fetch = nil
}

func (source *tuiProviderSource) setProbeError(err error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.probeErr = err
}

func (source *tuiProviderSource) setFingerprint(fingerprint provider.SessionFingerprint) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fingerprint = fingerprint
}

type tuiProviderReader tuiProviderSource

func (reader *tuiProviderReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	source := (*tuiProviderSource)(reader)
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.identity, source.probeErr
}

func (reader *tuiProviderReader) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	source := (*tuiProviderSource)(reader)
	source.mu.Lock()
	fetch := source.fetch
	snapshot := source.snapshot.Clone()
	source.mu.Unlock()
	if fetch != nil {
		return fetch(ctx, progress)
	}
	if progress != nil {
		progress(provider.Progress{Partition: "visible", Fetched: len(snapshot.Transactions), Total: len(snapshot.Transactions), Attempt: 1})
	}
	return snapshot, nil
}

type tuiIncrementingReader struct {
	mu    sync.Mutex
	value byte
}

func (reader *tuiIncrementingReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.value++
	for index := range buffer {
		buffer[index] = reader.value
	}
	return len(buffer), nil
}

var _ io.Reader = (*tuiIncrementingReader)(nil)
