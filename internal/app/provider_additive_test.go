package app_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/profiletransfer"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

type simpleFINAdmissionProfile struct {
	store.Profile
	beforeAcquire func()
}

func (profile *simpleFINAdmissionProfile) AcquireRefreshLease(ctx context.Context, lease store.RefreshLease, now time.Time) (store.RefreshLease, bool, error) {
	profile.beforeAcquire()
	return profile.Profile.AcquireRefreshLease(ctx, lease, now)
}

func TestSimpleFINAdmissionReloadsStateUnderOwnership(t *testing.T) {
	_, profile := newProviderRefreshService(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	wrapper := &simpleFINAdmissionProfile{Profile: profile, beforeAcquire: func() {
		_, acquired, err := profile.AcquireRefreshLease(t.Context(), store.RefreshLease{OwnerID: "other", Renderer: "tui", ExpiresAt: now.Add(time.Minute)}, now)
		require.NoError(t, err)
		require.True(t, acquired)
		require.NoError(t, profile.RecordRefreshAttempt(t.Context(), "other", now, now.Add(time.Hour)))
		require.NoError(t, profile.ReleaseRefreshLease(t.Context(), "other"))
	}}
	service, err := app.NewProfileService(t.Context(), wrapper)
	require.NoError(t, err)
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: simpleFINObservation(t, now, 0)}
	configureSimpleFIN(t, service, source, &now)
	_, err = service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()})
	assertProviderAppCode(t, err, provider.CodeRateLimited)
	require.Zero(t, source.fetchCalls())
}

func TestSimpleFINRefreshPreservesRedoAndMerge(t *testing.T) {
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	observation := simpleFINObservation(t, now, 2)
	observation.Merchants = append(observation.Merchants, domain.ImportEntity{Kind: domain.EntityKindMerchant, ExternalID: "merchant-other", Label: "Other Merchant"})
	observation.Transactions[1].MerchantExternalID = "merchant-other"
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: observation}
	configureSimpleFIN(t, service, source, &now)
	request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()}
	_, err := service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	var first, second domain.TransactionRecord
	for _, row := range loaded.Committed.Transactions {
		if row.ProviderID == transactionExternalID(0) {
			first = row
		} else {
			second = row
		}
	}
	_, err = service.Mutate(t.Context(), app.MutationRequest{Action: app.ActionEditMerchant, ExpectedRevision: service.Revision(), State: detailViewState(), Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(first.ID)}, Input: app.EditInput{Scope: app.EditScopeEntity, Label: "Other Merchant", DestinationID: second.MerchantID}})
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision()})
	require.NoError(t, err)
	_, err = service.Mutate(t.Context(), app.MutationRequest{Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(), State: detailViewState(), Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(first.ID)}})
	require.NoError(t, err)
	_, err = service.Undo(t.Context(), service.Revision())
	require.NoError(t, err)
	before, err := profile.Load(t.Context())
	require.NoError(t, err)
	now = now.Add(time.Hour)
	observation.ObservedAt = now
	row := observation.Transactions[0]
	row.ExternalID = "new-after-merge"
	observation.Transactions = append(observation.Transactions, row)
	source.setSnapshot(observation)
	_, err = service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	after, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Journal, after.Journal)
	require.Equal(t, before.Cursor, after.Cursor)
	for _, row := range after.Committed.Transactions {
		require.Equal(t, second.MerchantID, row.MerchantID)
	}
	_, err = service.Redo(t.Context(), service.Revision())
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision()})
	require.NoError(t, err)
	after, err = profile.Load(t.Context())
	require.NoError(t, err)
	for _, row := range after.Committed.Transactions {
		require.Equal(t, row.ID == first.ID, row.Hidden)
	}
}

func TestSimpleFINRefreshUsesLatestCommittedState(t *testing.T) {
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: simpleFINObservation(t, now, 2)}
	configureSimpleFIN(t, service, source, &now)
	request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()}
	_, err := service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	second, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	now = now.Add(time.Hour)
	source.setSnapshot(simpleFINObservation(t, now, 3))
	started, release := make(chan struct{}), make(chan struct{})
	source.setFetchContextHook(func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() { _, err := service.RefreshProvider(t.Context(), request); done <- err }()
	<-started
	_, err = second.Mutate(t.Context(), app.MutationRequest{Action: app.ActionDeleteTransaction, ExpectedRevision: second.Revision(), State: detailViewState(), Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(loaded.Committed.Transactions[0].ID)}})
	if err != nil {
		close(release)
		<-done
		t.Fatal(err)
	}
	_, err = second.Commit(t.Context(), app.CommitRequest{ExpectedRevision: second.Revision(), ReviewedRevision: second.Revision()})
	close(release)
	require.NoError(t, err)
	require.NoError(t, <-done)
	after, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, after.Committed.Transactions, 2)
	for _, row := range after.Committed.Transactions {
		require.NotEqual(t, loaded.Committed.Transactions[0].ID, row.ID)
	}
}

func TestSimpleFINRefreshCadenceSurvivesRestart(t *testing.T) {
	for _, failure := range []string{"success", "cancel", "unavailable", "revoked", "payment", "rate-limited"} {
		t.Run(failure, func(t *testing.T) {
			service, profile := newProviderRefreshService(t)
			now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
			source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: simpleFINObservation(t, now, 0)}
			configureSimpleFIN(t, service, source, &now)
			switch failure {
			case "cancel":
				source.setFetchContextHook(func(context.Context) error { return context.Canceled })
			case "unavailable":
				source.fetchErr = provider.NewError(provider.CodeUnavailable)
			case "revoked":
				source.fetchErr = provider.NewError(provider.CodeReconnectRequired)
			case "payment":
				source.fetchErr = provider.ErrPaymentRequired
			case "rate-limited":
				source.fetchErr = provider.NewErrorWithRetry(provider.CodeRateLimited, 3*time.Hour)
			}
			request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()}
			_, err := service.RefreshProvider(t.Context(), request)
			if failure == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if failure == "payment" {
				require.ErrorContains(t, err, "subscription")
			}
			require.Equal(t, 1, source.fetchCalls())
			service, err = app.NewProfileService(t.Context(), profile)
			require.NoError(t, err)
			configureSimpleFIN(t, service, source, &now)
			now = now.Add(59 * time.Minute)
			result, err := service.RefreshProvider(t.Context(), request)
			assertProviderAppCode(t, err, provider.CodeRateLimited)
			require.True(t, result.Status.NextEligible.After(now))
			require.Equal(t, 1, source.fetchCalls())
			status, err := service.ProviderStatus(t.Context())
			require.NoError(t, err)
			require.Equal(t, "simplefin", status.ProviderKind)
			require.Equal(t, now.Add(-59*time.Minute), status.LastAttempt)
			require.False(t, app.ProviderRefreshDue(status, now.Add(22*time.Hour)))
			require.Equal(t, failure == "success", app.ProviderRefreshDue(status, now.Add(24*time.Hour)))
			now = now.Add(time.Minute)
			if failure == "rate-limited" {
				now = now.Add(2 * time.Hour)
			}
			source.mu.Lock()
			source.fetchErr = nil
			source.mu.Unlock()
			source.setSnapshot(simpleFINObservation(t, now, 0))
			_, err = service.RefreshProvider(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, 2, source.fetchCalls())
		})
	}
}

func simpleFINObservation(t *testing.T, now time.Time, count int) domain.ImportSnapshot {
	t.Helper()
	value := providerSnapshot(t, now, count)
	value.Groups, value.Categories = nil, nil
	for i := range value.Transactions {
		value.Transactions[i].CategoryExternalID = ""
	}
	return value
}

func configureSimpleFIN(t *testing.T, service *app.Service, source provider.ReaderSource, now *time.Time) {
	t.Helper()
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "simplefin", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "simplefin-test", Now: func() time.Time { return *now },
	}))
}

func TestSimpleFINRefreshIsAdditive(t *testing.T) {
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: simpleFINObservation(t, now, 2)}
	configureSimpleFIN(t, service, source, &now)
	request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()}
	_, err := service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	now = now.Add(time.Hour)
	observation := simpleFINObservation(t, now, 3)
	observation.Transactions = observation.Transactions[2:]
	source.setSnapshot(observation)
	result, err := service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 3)
	require.Equal(t, 1, result.Status.Summary.ImportedTransactions)
	now = now.Add(time.Hour)
	source.setSnapshot(domain.ImportSnapshot{ObservedAt: now})
	_, err = service.RefreshProvider(t.Context(), request)
	require.NoError(t, err)
	loaded, err = profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 3)
}

func TestSimpleFINLocalCommitPreservesEditsAndTombstones(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		name := "restart"
		if transfer {
			name = "JSONL transfer"
		}
		t.Run(name, func(t *testing.T) { testSimpleFINLocalCommit(t, transfer) })
	}
}

func testSimpleFINLocalCommit(t *testing.T, transfer bool) {
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: "credential-digest"}, snapshot: simpleFINObservation(t, now, 2)}
	configureSimpleFIN(t, service, source, &now)
	_, err := service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()})
	require.NoError(t, err)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	id := loaded.Committed.Transactions[0].ID
	mutate := func(action app.ActionID, input app.EditInput, target *app.RowTarget) {
		t.Helper()
		_, err := service.Mutate(t.Context(), app.MutationRequest{Action: action, ExpectedRevision: service.Revision(), State: detailViewState(), Selection: app.EmptySelection(), Target: target, Input: input})
		require.NoError(t, err)
	}
	mutate(app.ActionManageGroups, app.EditInput{Taxonomy: app.TaxonomyCreate, EntityID: "local-group", Label: "Local Group"}, nil)
	mutate(app.ActionEditMerchant, app.EditInput{Scope: app.EditScopeEntity, Label: "Locally Renamed Merchant"}, &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(id)})
	mutate(app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyCreate, EntityID: "local-category", GroupID: "local-group", Label: "Local Category"}, nil)
	mutate(app.ActionEditCategory, app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "local-category"}, &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(id)})
	mutate(app.ActionToggleHidden, app.EditInput{}, &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(id)})
	mutate(app.ActionDeleteTransaction, app.EditInput{}, &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(loaded.Committed.Transactions[1].ID)})
	_, err = service.Undo(t.Context(), service.Revision())
	require.NoError(t, err)
	_, err = service.Redo(t.Context(), service.Revision())
	require.NoError(t, err)
	_, err = service.Review(t.Context(), service.Revision(), app.ReviewWindow{Limit: 20})
	require.NoError(t, err)
	result, err := service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision()})
	require.NoError(t, err)
	require.Nil(t, result.ProviderWrite)
	require.Equal(t, 1, source.fetchCalls())
	before, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, before.Committed.Transactions, 1)
	require.Equal(t, domain.EntityID("local-category"), before.Committed.Transactions[0].CategoryID)
	require.True(t, before.Committed.Transactions[0].Hidden)
	require.Equal(t, "Locally Renamed Merchant", before.Committed.Merchants[0].Label)
	if transfer {
		state, transferErr := profile.LoadProfileTransfer(t.Context())
		require.NoError(t, transferErr)
		state, _, transferErr = app.PrepareProfileTransfer(state, now)
		require.NoError(t, transferErr)
		var encoded bytes.Buffer
		require.NoError(t, profiletransfer.Encode(&encoded, profiletransfer.Document{
			Header: profiletransfer.Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: "simplefin", ExportedAt: now, SourceRevision: state.Snapshot.Revision, SourceName: "Example"}, State: state,
		}))
		document, transferErr := profiletransfer.Decode(&encoded)
		require.NoError(t, transferErr)
		_, profile = newProviderRefreshService(t)
		require.NoError(t, profile.InstallProfileTransfer(t.Context(), document.State))
	}
	service, err = app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	configureSimpleFIN(t, service, source, &now)
	now = now.Add(time.Hour)
	source.setSnapshot(simpleFINObservation(t, now, 2))
	_, err = service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState()})
	require.NoError(t, err)
	after, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Committed, after.Committed)
	state, err := profile.ProviderState(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Write)
}
