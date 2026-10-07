// Package runtime composes real Moneyflow services with isolated synthetic provider state.
package runtime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/uitest"
)

// Runtime owns one real profile and a synthetic remote account.
type Runtime struct {
	Service  *app.Service
	Profile  store.Profile
	Provider *uitest.Provider
	Paths    home.Paths
}

// Open keeps profile and simulator files separate beneath the caller's private root.
func Open(ctx context.Context, root string) (*Runtime, error) {
	paths, err := home.ResolveRoot(filepath.Join(root, "profile"), nil, "")
	if err != nil {
		return nil, err
	}
	return OpenProfile(ctx, paths, filepath.Join(root, "provider"), "tui")
}

// OpenLocal copies the same synthetic committed fixture into an unbound profile.
// This covers local-only taxonomy edits without inventing remote capabilities.
func OpenLocal(ctx context.Context, root string) (*Runtime, error) {
	seed, err := Open(ctx, filepath.Join(root, "seed"))
	if err != nil {
		return nil, err
	}
	snapshot, err := seed.Profile.Load(ctx)
	if closeErr := seed.Close(); err != nil || closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	paths, err := home.ResolveRoot(filepath.Join(root, "local"), nil, "")
	if err != nil {
		return nil, err
	}
	options := sqlite.DefaultOptions
	options.Now = seed.Provider.Now
	profile, err := sqlite.Open(ctx, paths, options)
	if err != nil {
		return nil, err
	}
	if _, err = profile.CreateSeededProfile(ctx, snapshot.Committed); err != nil {
		return nil, errors.Join(err, profile.Close())
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return nil, errors.Join(err, profile.Close())
	}
	return &Runtime{Profile: profile, Service: service, Provider: seed.Provider, Paths: paths}, nil
}

// OpenProfile also serves catalog-backed browser profiles, preserving both stores on reopen.
func OpenProfile(ctx context.Context, paths home.Paths, providerRoot, renderer string) (*Runtime, error) {
	source, err := uitest.OpenProvider(providerRoot)
	if err != nil {
		return nil, err
	}
	options := sqlite.DefaultOptions
	options.Now = source.Now
	profile, err := sqlite.Open(ctx, paths, options)
	if err != nil {
		return nil, err
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return nil, errors.Join(err, profile.Close())
	}
	err = service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: renderer, InstanceID: "ui-scenario-" + rand.Text(), Now: source.Now,
	})
	if err != nil {
		return nil, errors.Join(err, profile.Close())
	}
	state, err := profile.ProviderState(ctx)
	if err == nil && state.Binding == nil {
		_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	}
	if err != nil {
		return nil, errors.Join(err, profile.Close())
	}
	return &Runtime{Service: service, Profile: profile, Provider: source, Paths: paths}, nil
}

// Close is called after the UI and worker using the profile have stopped.
func (run *Runtime) Close() error { return run.Profile.Close() }

// FilteredSession is the hand-reviewed September scope; UI tests also reach it through user actions.
func FilteredSession() app.Session {
	start, err := domain.ParseDate("2026-09-01")
	if err != nil {
		panic(err)
	}
	end, err := domain.ParseDate("2026-09-30")
	if err != nil {
		panic(err)
	}
	session := app.NewSession()
	session.DateRange = &domain.DateRange{Start: start, End: end}
	session.ShowHidden = false
	return session
}

// Stage drives the public service mutation for the focused source merchant.
func (run *Runtime) Stage(ctx context.Context, session app.Session, kind, value string) error {
	rows, err := run.Service.QueryContext(ctx, session)
	if err != nil {
		return err
	}
	var target *app.RowTarget
	for _, row := range rows.AggregateRows {
		if row.Label == "Example Shop" {
			target = &app.RowTarget{Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(row)}
			break
		}
	}
	if target == nil {
		return errors.New("scenario source merchant is not visible")
	}
	request := app.MutationRequest{ExpectedRevision: run.Service.Revision(), State: session.ViewState(), Selection: app.EmptySelection(), Target: target}
	request.Input.Scope = app.EditScopeTransactions
	snapshot, err := run.Profile.Load(ctx)
	if err != nil {
		return err
	}
	switch kind {
	case "merchant":
		request.Action = app.ActionEditMerchant
		if value == "" {
			value = "Destination Shop"
		}
		request.Input.Label = value
		for _, merchant := range snapshot.Committed.Merchants {
			if merchant.Label == value {
				request.Input.DestinationID = merchant.ID
			}
		}
	case "category":
		request.Action = app.ActionEditCategory
		if value == "" {
			value = "Health"
		}
		for _, category := range snapshot.Committed.Categories {
			if category.Label == value {
				request.Input.DestinationID = category.ID
			}
		}
		if request.Input.DestinationID == "" {
			return fmt.Errorf("unknown scenario category %q", value)
		}
	case "hide":
		request.Action = app.ActionToggleHidden
	default:
		return fmt.Errorf("unknown scenario edit %q", kind)
	}
	_, err = run.Service.Mutate(ctx, request)
	return err
}

// Commit reviews the current revision and runs its actual durable provider worker.
func (run *Runtime) Commit(ctx context.Context, session app.Session) error {
	review, err := run.Service.Review(ctx, run.Service.Revision(), app.ReviewWindow{})
	if err != nil {
		return err
	}
	_, err = run.Service.Commit(ctx, app.CommitRequest{ExpectedRevision: review.Revision, ReviewedRevision: review.Revision, State: session.ViewState(), Selection: app.EmptySelection()})
	if err != nil {
		return err
	}
	_, err = run.Service.RunProviderWrite(ctx)
	return err
}
