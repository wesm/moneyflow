package main

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

type catalogAmazonSources struct {
	catalog *profilecatalog.Catalog
}

func (sources catalogAmazonSources) ListAmazonSources(ctx context.Context) ([]app.AmazonSourceDescriptor, error) {
	entries, err := sources.catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]app.AmazonSourceDescriptor, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		result = append(result, app.AmazonSourceDescriptor{ProfileID: entry.ID, Kind: entry.ProviderKind})
	}
	return result, nil
}

func newCatalogAmazonMatcher(catalog *profilecatalog.Catalog) (*app.AmazonMatchingService, error) {
	return app.NewAmazonMatchingService(catalogAmazonSources{catalog: catalog}, func(
		ctx context.Context,
		descriptor app.AmazonSourceDescriptor,
	) (store.AmazonMatchSourceState, func() error, error) {
		entry, err := catalog.Resolve(ctx, descriptor.ProfileID)
		if err != nil {
			return store.AmazonMatchSourceState{}, nil, err
		}
		lifecycle, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
		if err != nil {
			return store.AmazonMatchSourceState{}, nil, err
		}
		profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
		if err != nil {
			_ = lifecycle.Release()
			return store.AmazonMatchSourceState{}, nil, err
		}
		state, err := profile.LoadAmazonMatchSource(ctx)
		if err != nil {
			_ = errors.Join(profile.Close(), lifecycle.Release())
			return store.AmazonMatchSourceState{}, nil, err
		}
		return state, func() error { return errors.Join(profile.Close(), lifecycle.Release()) }, nil
	})
}
