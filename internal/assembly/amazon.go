package assembly

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// AmazonMatchingLifecycle invalidates Amazon matches around a destructive profile recovery.
type AmazonMatchingLifecycle struct {
	api.ProfileCatalog
	Matcher interface {
		InvalidateDuring(string, func() error) error
	}
}

// Recreate recovers one profile while its Amazon matches are invalid.
func (lifecycle AmazonMatchingLifecycle) Recreate(
	ctx context.Context,
	request profilecatalog.RecoveryRequest,
) (profilecatalog.RecoveryResult, error) {
	var result profilecatalog.RecoveryResult
	err := lifecycle.Matcher.InvalidateDuring(request.Plan.ProfileID, func() error {
		var recreateErr error
		result, recreateErr = lifecycle.ProfileCatalog.Recreate(ctx, request)
		return recreateErr
	})
	return result, err
}

// AmazonMatchingEvictor drops a profile's Amazon matches after evicting its service.
type AmazonMatchingEvictor struct {
	Evictor api.ProfileEvictor
	Matcher *app.AmazonMatchingService
}

// Evict closes the profile's cached service and invalidates its Amazon matches.
func (evictor AmazonMatchingEvictor) Evict(ctx context.Context, profileID string) error {
	if err := evictor.Evictor.Evict(ctx, profileID); err != nil {
		return err
	}
	evictor.Matcher.Invalidate(profileID)
	return nil
}

type amazonSources struct {
	list func(context.Context) ([]profilecatalog.Entry, error)
}

func (sources amazonSources) ListAmazonSources(
	ctx context.Context,
) ([]app.AmazonSourceDescriptor, error) {
	entries, err := sources.list(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]app.AmazonSourceDescriptor, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		result = append(result, app.AmazonSourceDescriptor{
			ProfileID: entry.ID, DisplayName: entry.DisplayName, Kind: entry.ProviderKind,
		})
	}
	return result, nil
}

// NewAmazonMatcher matches Amazon orders across the profiles list returns. It opens each source
// profile under a shared lock with options.
func NewAmazonMatcher(
	list func(context.Context) ([]profilecatalog.Entry, error),
	resolve func(context.Context, string) (profilecatalog.Entry, error),
	options sqlite.Options,
) (*app.AmazonMatchingService, error) {
	return app.NewAmazonMatchingService(amazonSources{list: list}, func(
		ctx context.Context,
		descriptor app.AmazonSourceDescriptor,
		knownRevision uint64,
	) (*store.AmazonMatchSourceState, func() error, error) {
		entry, err := resolve(ctx, descriptor.ProfileID)
		if err != nil {
			return nil, nil, err
		}
		lifecycle, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
		if err != nil {
			return nil, nil, err
		}
		paths := entry.ProfilePaths()
		currentRevision, err := sqlite.ProbeRevision(ctx, paths, options)
		if err != nil {
			_ = lifecycle.Release()
			return nil, nil, err
		}
		if knownRevision != 0 && currentRevision == knownRevision {
			return nil, lifecycle.Release, nil
		}
		profile, err := sqlite.Open(ctx, paths, options)
		if err != nil {
			_ = lifecycle.Release()
			return nil, nil, err
		}
		closeSource := func() error { return errors.Join(profile.Close(), lifecycle.Release()) }
		state, err := profile.LoadAmazonMatchSource(ctx)
		if err != nil {
			_ = closeSource()
			return nil, nil, err
		}
		return &state, closeSource, nil
	})
}

// LoadAmazonTaxonomyClone copies the categories of the profile selector names. An empty
// selector returns nil.
func LoadAmazonTaxonomyClone(
	ctx context.Context,
	resolve func(context.Context, string) (profilecatalog.Entry, error),
	selector string,
	options sqlite.Options,
) (*app.TaxonomyClone, error) {
	if selector == "" {
		return nil, nil
	}
	entry, err := resolve(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("resolve taxonomy source: %w", err)
	}
	lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return nil, fmt.Errorf("open taxonomy source: %w", err)
	}
	defer func() { _ = lock.Release() }()
	profile, err := sqlite.Open(ctx, entry.ProfilePaths(), options)
	if err != nil {
		return nil, fmt.Errorf("open taxonomy source: %w", err)
	}
	defer func() { _ = profile.Close() }()
	snapshot, err := profile.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load taxonomy source: %w", err)
	}
	return &app.TaxonomyClone{SourceProfileID: entry.ID, Committed: snapshot.Committed}, nil
}

// OpenAmazonImportTarget holds an Amazon profile under a shared lock for one import.
func OpenAmazonImportTarget(
	_ context.Context,
	catalog *profilecatalog.Catalog,
	entry profilecatalog.Entry,
	options sqlite.Options,
) (amazonimport.Target, error) {
	lifecycle, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return amazonimport.Target{}, err
	}
	if err = catalog.ValidateEntry(entry); err != nil {
		_ = lifecycle.Release()
		return amazonimport.Target{}, err
	}
	return amazonimport.Target{
		ProfileID: entry.ID, Root: entry.Root, Close: lifecycle.Release,
		Import: func(
			importContext context.Context,
			request app.AmazonImportRequest,
		) (app.AmazonImportResult, error) {
			profile, openErr := sqlite.Open(importContext, entry.ProfilePaths(), options)
			if openErr != nil {
				return app.AmazonImportResult{}, openErr
			}
			defer func() { _ = profile.Close() }()
			return app.ImportAmazonProfile(importContext, profile, request)
		},
	}, nil
}
