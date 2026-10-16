package moneyflow

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/diagnostic"
	"github.com/wesm/moneyflow/internal/mcp"
)

// RunScheduler starts due provider refreshes for every Monarch, YNAB and SimpleFIN profile: at
// once and then every minute. Each refresh runs in the background under the instance. It
// returns ctx.Err() when ctx ends and ErrClosed when the instance closes.
func (instance *Instance) RunScheduler(ctx context.Context) error {
	if err := instance.checkOpen(); err != nil {
		return err
	}
	ticker := time.NewTicker(instance.seams.schedulerTick)
	defer ticker.Stop()
	for {
		instance.schedulePass(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-instance.ctx.Done():
			return ErrClosed
		case <-ticker.C:
		}
	}
}

// schedulePass schedules every provider profile's refresh once. It logs why it could not list the
// profiles, unless ctx ended or the instance closed, which a host expects. It also logs a panic
// while it lists them and returns, because RunScheduler runs on the host's goroutine.
func (instance *Instance) schedulePass(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			instance.logger.Error("moneyflow scheduler pass panicked",
				"panic", diagnostic.PanicText(recovered), "frames", diagnostic.PanicFrames())
		}
	}()
	entries, err := instance.list(ctx)
	if err != nil {
		if ctx.Err() == nil && !errors.Is(err, ErrClosed) {
			instance.logger.Warn("moneyflow could not list profiles to schedule refreshes",
				"cause", diagnostic.DescribeCause(err))
		}
		return
	}
	for _, entry := range entries {
		switch entry.ProviderKind {
		case "monarch", "ynab", "simplefin":
			instance.scheduleRefresh(ctx, entry.ID)
		}
	}
}

// scheduleRefresh starts one profile's refresh when it is due. A profile that cannot open, has
// no provider runtime or cannot refresh now is skipped until the next pass. A panic before the
// refresh starts is logged and becomes the profile's failure; the supervisor recovers the refresh.
func (instance *Instance) scheduleRefresh(ctx context.Context, profileID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			instance.logger.Error("moneyflow scheduled refresh panicked", "profile", profileID,
				"panic", diagnostic.PanicText(recovered), "frames", diagnostic.PanicFrames())
			instance.recordFailure(profileID, mcp.ErrWorkPanicked)
		}
	}()
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return
	}
	defer func() { _ = lease.Release() }()
	if instance.seams.beforeDueCheck != nil {
		instance.seams.beforeDueCheck(profileID)
	}
	service := lease.Service()
	status, err := service.ProviderStatus(ctx)
	if err != nil || status.RefreshDue == app.ProviderRefreshNone ||
		!refreshAvailable(ctx, service) {
		return
	}
	instance.startDueRefresh(ctx, profileID, service)
}

// startDueRefresh starts the scheduled refresh of a profile whose refresh is due, or joins a
// refresh that reads at least as much. A parked refresh that the profile's stored state has moved
// past fails first, as mcp.Supervisor.ExpireObsolete says. When that state cannot be read, the
// profile waits for the next pass.
func (instance *Instance) startDueRefresh(
	ctx context.Context,
	profileID string,
	service *app.Service,
) {
	supervisor, err := instance.supervisor(profileID)
	if err == nil {
		err = supervisor.ExpireObsolete(ctx, service)
	}
	if err != nil {
		return
	}
	_, _ = supervisor.StartRefresh(mcp.RefreshDue,
		func(worker context.Context) (app.ProviderRefreshResult, error) {
			return instance.refresh(worker, profileID, app.ProviderRefreshRequest{
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
				Window: app.WindowRequest{Limit: 1},
			})
		})
}

// refreshAvailable reports whether the service can refresh now, as
// app.Service.ProviderRefreshCapability says.
func refreshAvailable(ctx context.Context, service *app.Service) bool {
	refresh, err := service.ProviderRefreshCapability(ctx)
	return err == nil && refresh.Available
}
