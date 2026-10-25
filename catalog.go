package moneyflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/diagnostic"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/profilepool"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// list surveys the catalog. It keeps a profile it cannot inspect, with StatusUnavailable, and
// drops the profile at the root of the data directory, legacy or activated, because Remove,
// Backup and Verify handle only Root/profiles. It returns ErrClosed after Close.
func (instance *Instance) list(ctx context.Context) ([]profilecatalog.Entry, error) {
	survey := func() ([]profilecatalog.Entry, error) { return instance.catalog.Survey(ctx) }
	entries, err := lockCatalog(instance, instance.catalogMu.RLocker(), survey)
	if err != nil {
		return nil, err
	}
	// Survey gives the root profile the catalog's own root, unchanged, so equal strings match.
	root := instance.catalog.Paths().Root
	return slices.DeleteFunc(entries, func(entry profilecatalog.Entry) bool {
		return entry.ID == "" || entry.Root == root
	}), nil
}

// resolve finds the profile selector names. After Close it fails with ErrClosed and
// api.ErrProfileUnavailable, which the API answers with 503 profile_busy, so a client retries.
func (instance *Instance) resolve(
	ctx context.Context,
	selector string,
) (profilecatalog.Entry, error) {
	entries, err := instance.list(ctx)
	if errors.Is(err, ErrClosed) {
		return profilecatalog.Entry{}, fmt.Errorf("%w: %w", err, api.ErrProfileUnavailable)
	}
	if err != nil {
		return profilecatalog.Entry{}, err
	}
	entry, err := profilecatalog.ResolveEntries(entries, selector)
	if err != nil {
		return profilecatalog.Entry{}, err
	}
	if entry.Status == profilecatalog.StatusUnavailable {
		return profilecatalog.Entry{}, fmt.Errorf(
			"profile %s is unavailable: %s", entry.ID, entry.Problem)
	}
	return entry, nil
}

func (instance *Instance) lookup(
	ctx context.Context,
	profileID string,
) (profilecatalog.Entry, error) {
	// ponytail: each profile's first open surveys the catalog again; pass the entry if that is slow.
	entries, err := instance.list(ctx)
	if err != nil {
		return profilecatalog.Entry{}, err
	}
	for _, entry := range entries {
		if entry.ID == profileID {
			return entry, nil
		}
	}
	return profilecatalog.Entry{}, ErrProfileNotFound
}

// entryProblem explains why the catalog says a profile cannot open, or returns "".
func entryProblem(entry profilecatalog.Entry) string {
	switch entry.Status {
	case profilecatalog.StatusUnavailable:
		return entry.Problem
	case profilecatalog.StatusNeedsRecovery, profilecatalog.StatusRequiresNewer,
		profilecatalog.StatusManifestUnsupported:
		return "profile status is " + string(entry.Status)
	default:
		return ""
	}
}

// errOpenPanicked replaces the error of a profile open that panicked, so no panic value reaches
// the host.
var errOpenPanicked = errors.New("opening the profile stopped unexpectedly")

// unopenable carries why a profile cannot open, for any reason but another holder or Close. The
// API answers it with 404 not_found, and its message stays the cause's.
type unopenable struct{ cause error }

func (failure unopenable) Error() string { return failure.cause.Error() }

func (failure unopenable) Unwrap() error { return failure.cause }

// Is matches api.ErrProfileNotFound, which the API answers with 404 not_found.
func (failure unopenable) Is(target error) bool { return target == api.ErrProfileNotFound }

// openProfile opens one profile for the registry. Every failure but a busy profile or a closed
// instance is unopenable. A closed instance fails with ErrClosed and api.ErrProfileUnavailable,
// which the API answers with 503 profile_busy, so a client retries instead of dropping the
// profile. A panic is logged and recorded as the profile's failure, and the open fails with fixed
// text.
func (instance *Instance) openProfile(
	ctx context.Context,
	profileID string,
) (opened profilepool.RegistryProfile, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			instance.logger.Error("moneyflow profile open panicked", "profile", profileID,
				"panic", diagnostic.PanicText(recovered), "frames", diagnostic.PanicFrames())
			instance.recordFailure(profileID, errOpenPanicked)
			opened, err = profilepool.RegistryProfile{}, errOpenPanicked
		}
		if errors.Is(err, ErrClosed) {
			err = fmt.Errorf("%w: %w", err, api.ErrProfileUnavailable)
		} else if err != nil && !errors.Is(err, home.ErrLockBusy) {
			err = unopenable{cause: err}
		}
	}()
	return instance.openShared(ctx, profileID)
}

// openShared opens one profile under a shared lifecycle lock, so standalone moneyflow can open it
// too while an exclusive holder, such as an import, keeps it busy.
func (instance *Instance) openShared(
	ctx context.Context,
	profileID string,
) (_ profilepool.RegistryProfile, err error) {
	entry, err := instance.lookup(ctx, profileID)
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	if problem := entryProblem(entry); problem != "" {
		// Survey reports a profile that another process holds as unavailable, with this code.
		if strings.HasPrefix(problem, string(profilecatalog.CodeProfileBusy)+":") {
			return profilepool.RegistryProfile{}, fmt.Errorf(
				"profile %s: %w", profileID, home.ErrLockBusy)
		}
		return profilepool.RegistryProfile{}, fmt.Errorf("profile %s cannot open: %s", profileID, problem)
	}
	paths := entry.ProfilePaths()
	lock, err := home.TryLockExisting(paths.Root, home.LockProfile, home.LockShared)
	if errors.Is(err, home.ErrLockBusy) {
		return profilepool.RegistryProfile{}, fmt.Errorf(
			"profile %s is in use by another process: %w", profileID, err)
	}
	if err != nil {
		return profilepool.RegistryProfile{}, fmt.Errorf("lock profile %s: %w", profileID, err)
	}
	// The release also runs during a panic, which openProfile recovers.
	owned := true
	defer func() {
		if owned {
			err = errors.Join(err, lock.Release())
		}
	}()
	opened, err := instance.openService(ctx, entry)
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	closeStore := opened.Close
	opened.Close = func() error { return errors.Join(closeStore(), lock.Release()) }
	owned = false
	return opened, nil
}

func (instance *Instance) openService(
	ctx context.Context,
	entry profilecatalog.Entry,
) (_ profilepool.RegistryProfile, err error) {
	if err = instance.catalog.ValidateEntry(entry); err != nil {
		return profilepool.RegistryProfile{}, err
	}
	paths := entry.ProfilePaths()
	profile, err := sqlite.Open(ctx, paths, instance.sqlite)
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	// The close also runs during a panic, which openProfile recovers.
	owned := true
	defer func() {
		if owned {
			err = errors.Join(err, profile.Close())
		}
	}()
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	err = assembly.ConfigureProvider(ctx, service, paths, instance.seams.factories, renderer)
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	service.ConfigureAmazonMatching(instance.matcher)
	owned = false
	return profilepool.RegistryProfile{
		ID: entry.ID, Paths: paths, Service: service, Close: profile.Close,
	}, nil
}

// lockCatalog runs call while it holds lock, one side of the instance's catalog lock, so catalog
// calls in this process wait for each other instead of answering profile_busy. Once the instance
// is closed, it returns ErrClosed without running call.
func lockCatalog[T any](instance *Instance, lock sync.Locker, call func() (T, error)) (T, error) {
	lock.Lock()
	defer lock.Unlock()
	if err := instance.checkOpen(); err != nil {
		var zero T
		return zero, err
	}
	return call()
}

// lockedCatalog is the catalog the API reaches. Every call but List holds the catalog write lock.
// After Close every call fails with ErrClosed and profile_busy, which the API answers with 503, so
// a client retries instead of reading a server failure.
type lockedCatalog struct{ instance *Instance }

// busyWhenClosed adds profile_busy to ErrClosed and returns any other error as it is.
func busyWhenClosed(err error) error {
	if !errors.Is(err, ErrClosed) {
		return err
	}
	return fmt.Errorf("%w: %w", err, &profilecatalog.Error{Code: profilecatalog.CodeProfileBusy})
}

// lockForAPI runs call under the catalog write lock, as lockCatalog does, for the API.
func lockForAPI[T any](instance *Instance, call func() (T, error)) (T, error) {
	result, err := lockCatalog(instance, &instance.catalogMu, call)
	return result, busyWhenClosed(err)
}

// List lists profiles through list, so one broken profile shows up as unavailable instead of
// failing the whole list.
func (locked lockedCatalog) List(ctx context.Context) ([]profilecatalog.Entry, error) {
	entries, err := locked.instance.list(ctx)
	return entries, busyWhenClosed(err)
}

func (locked lockedCatalog) Create(
	ctx context.Context,
	request profilecatalog.CreateRequest,
) (profilecatalog.Entry, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (profilecatalog.Entry, error) {
		return instance.catalog.Create(ctx, request)
	})
}

func (locked lockedCatalog) CancelNewProfile(ctx context.Context, profileID string) (bool, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (bool, error) {
		return instance.catalog.CancelNewProfile(ctx, profileID)
	})
}

func (locked lockedCatalog) ActivateForProvider(
	ctx context.Context,
	selector, providerKind string,
) (profilecatalog.Entry, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (profilecatalog.Entry, error) {
		return instance.catalog.ActivateForProvider(ctx, selector, providerKind)
	})
}

// RecoveryPlan only reads, but it reads under the catalog's own shared lock, which a change in this
// process holds exclusively. It takes the write lock so it waits for that change.
func (locked lockedCatalog) RecoveryPlan(
	ctx context.Context,
	selector string,
) (profilecatalog.RecoveryPlan, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (profilecatalog.RecoveryPlan, error) {
		return instance.catalog.RecoveryPlan(ctx, selector)
	})
}

func (locked lockedCatalog) Recreate(
	ctx context.Context,
	request profilecatalog.RecoveryRequest,
) (profilecatalog.RecoveryResult, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (profilecatalog.RecoveryResult, error) {
		return instance.catalog.Recreate(ctx, request)
	})
}

// Rename leaves the profile open, unlike removal, so its work and its process-local state, such
// as a pending confirmation or an unlocked YNAB budget, carry on.
func (locked lockedCatalog) Rename(
	ctx context.Context,
	profileID, displayName string,
) (profilecatalog.Entry, error) {
	instance := locked.instance
	return lockForAPI(instance, func() (profilecatalog.Entry, error) {
		return instance.catalog.Rename(ctx, profileID, displayName)
	})
}

func (locked lockedCatalog) Remove(ctx context.Context, profileID string) error {
	instance := locked.instance
	_, err := lockForAPI(instance, func() (struct{}, error) {
		return struct{}{}, instance.catalog.Remove(ctx, profileID)
	})
	return err
}
