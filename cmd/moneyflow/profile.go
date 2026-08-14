package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

const (
	demoDirectoryPrefix     = "moneyflow-v2-demo-"
	contractDirectoryPrefix = "moneyflow-v2-contract-"
)

// OpenedProfile owns one command-scoped profile service and its exact cleanup.
type OpenedProfile struct {
	Service *app.Service
	Close   func() error
	Path    string
	Demo    bool
}

// ProfileOpener provides command-level profile lifecycle injection for tests.
type ProfileOpener func(context.Context, ProfileOptions) (OpenedProfile, error)

// ProfileOptions selects persistent or uniquely temporary seeded profile startup.
type ProfileOptions struct {
	Demo         bool
	ExplicitHome string
	FixturePath  string
}

func openProfile(ctx context.Context, options ProfileOptions) (OpenedProfile, error) {
	if options.Demo || options.FixturePath != "" {
		return openDemoProfile(ctx, options.FixturePath)
	}
	userHome := ""
	configuredHome, configured := os.LookupEnv("MONEYFLOW_HOME")
	if options.ExplicitHome == "" && (!configured || configuredHome == "") {
		var err error
		userHome, err = os.UserHomeDir()
		if err != nil {
			return OpenedProfile{}, fmt.Errorf("open profile: resolve user home: %w", err)
		}
	}
	paths, err := home.ResolveRoot(options.ExplicitHome, os.LookupEnv, userHome)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open profile: %w", err)
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open profile: %w", err)
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		_ = profile.Close()
		return OpenedProfile{}, fmt.Errorf("open profile: load service: %w", err)
	}
	return OpenedProfile{
		Service: service,
		Close:   idempotentClose(profile.Close),
		Path:    paths.Database,
	}, nil
}

func openDemoProfile(ctx context.Context, fixturePath string) (OpenedProfile, error) {
	root, err := os.MkdirTemp("", demoDirectoryPrefix)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open demo profile: create temporary root: %w", err)
	}
	paths, err := home.ResolveRoot(root, nil, "")
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, demoDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open demo profile: %w", err)
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, demoDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open demo profile: %w", err)
	}
	fail := func(cause error) (OpenedProfile, error) {
		return OpenedProfile{}, errors.Join(
			cause, profile.Close(), removeOwnedTemporaryRoot(root, demoDirectoryPrefix),
		)
	}
	transactionsReader := bytes.NewReader(paritydata.Transactions)
	transactions, err := fixture.Decode(transactionsReader)
	if fixturePath != "" {
		transactions, err = fixture.Load(fixturePath)
	}
	if err != nil {
		return fail(fmt.Errorf("open demo profile: load fixture: %w", err))
	}
	committed, err := fixture.CommittedProfile(transactions)
	if err != nil {
		return fail(fmt.Errorf("open demo profile: convert fixture: %w", err))
	}
	if _, err = profile.CreateSeededProfile(ctx, committed); err != nil {
		return fail(fmt.Errorf("open demo profile: seed fixture: %w", err))
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return fail(fmt.Errorf("open demo profile: load service: %w", err))
	}
	return OpenedProfile{
		Service: service,
		Close: idempotentClose(func() error {
			return errors.Join(
				profile.Close(), removeOwnedTemporaryRoot(root, demoDirectoryPrefix),
			)
		}),
		Path: paths.Database,
		Demo: true,
	}, nil
}

func openTemporaryContractProfile(ctx context.Context) (OpenedProfile, error) {
	root, err := os.MkdirTemp("", contractDirectoryPrefix)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open contract profile: create temporary root: %w", err)
	}
	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, contractDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open contract profile: %w", err)
	}
	closeProfile := opened.Close
	opened.Close = idempotentClose(func() error {
		return errors.Join(
			closeProfile(), removeOwnedTemporaryRoot(root, contractDirectoryPrefix),
		)
	})
	return opened, nil
}

func idempotentClose(closeProfile func() error) func() error {
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() { closeErr = closeProfile() })
		return closeErr
	}
}

func removeOwnedTemporaryRoot(root string, prefix string) error {
	clean := filepath.Clean(root)
	temporary := filepath.Clean(os.TempDir())
	if prefix == "" || filepath.Dir(clean) != temporary || !strings.HasPrefix(filepath.Base(clean), prefix) {
		return errors.New("refuse to remove unowned temporary profile root")
	}
	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("remove temporary profile root: %w", err)
	}
	return nil
}

func closeOpenedProfile(opened OpenedProfile, runErr error) error {
	if opened.Close == nil {
		return errors.Join(runErr, errors.New("opened profile has no close function"))
	}
	return errors.Join(runErr, opened.Close())
}
