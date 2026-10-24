package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/diagnostic"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// ToolOptions selects the tools RegisterTools adds.
type ToolOptions = mcp.RegisterOptions

// RegisterTools adds moneyflow's MCP tools for the selected classes to server. Build a fresh
// server per MCP request and call RegisterTools on it; every server shares the instance's
// refresh, reconcile and write attempts, and the instance's API shares the same refresh and
// reconcile attempts. It adds no resources and no middleware.
func (instance *Instance) RegisterTools(server *mcpsdk.Server, options ToolOptions) error {
	if err := instance.checkOpen(); err != nil {
		return err
	}
	return mcp.Register(server, instanceTools{instance: instance}, options)
}

// supervisor returns the one supervisor that owns a profile's refresh, reconcile and write
// attempts, so the scheduler and every MCP server share them.
func (instance *Instance) supervisor(profileID string) (*mcp.Supervisor, error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.closed {
		return nil, ErrClosed
	}
	supervisor, ok := instance.supervisors[profileID]
	if !ok {
		supervisor = mcp.NewSupervisor(instance.ctx, instance.seams.now, cryptorand.Reader,
			func(err error) { instance.recordFailure(profileID, err) }, instance.logger)
		instance.supervisors[profileID] = supervisor
	}
	return supervisor, nil
}

// dropSupervisor stops a profile's supervised work, waits for it and forgets the supervisor, so
// the profile's next refresh, reconcile or write starts a new one.
func (instance *Instance) dropSupervisor(ctx context.Context, profileID string) error {
	instance.mu.Lock()
	supervisor := instance.supervisors[profileID]
	delete(instance.supervisors, profileID)
	instance.mu.Unlock()
	return supervisor.Close(ctx)
}

// supervisorEvictor drops a profile's supervisor before next closes the profile's service, so
// removal and recovery never close a service that supervised work still uses. Work no supervisor
// runs, such as an API provider write, can keep the profile; after evictionTimeout the eviction
// stops waiting and fails with profile_busy, and the profile keeps serving.
type supervisorEvictor struct {
	next     api.ProfileEvictor
	instance *Instance
}

func (evictor supervisorEvictor) Evict(ctx context.Context, profileID string) error {
	if err := evictor.instance.dropSupervisor(ctx, profileID); err != nil {
		return fmt.Errorf("stop supervised work for profile %s: %w", profileID, err)
	}
	return evictor.instance.evictWithin(ctx, evictor.next, profileID)
}

// evictWithin closes a profile's service through next once no work holds it. Work that still holds
// it after evictionTimeout makes the eviction fail with profile_busy, and the profile keeps serving.
func (instance *Instance) evictWithin(
	ctx context.Context,
	next api.ProfileEvictor,
	profileID string,
) error {
	timeout := evictionTimeout
	if instance.seams.evictionTimeout > 0 {
		timeout = instance.seams.evictionTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := next.Evict(bounded, profileID)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return fmt.Errorf("profile %s is still in use: %w", profileID,
			&profilecatalog.Error{Code: profilecatalog.CodeProfileBusy})
	}
	return err
}

// instanceTools resolves each tool call's profile argument against the instance's profiles.
type instanceTools struct{ instance *Instance }

// busyReason tells an MCP client that a profile is busy, in the words a survey uses for a profile
// that another process holds.
const busyReason = string(profilecatalog.CodeProfileBusy) + ": " + profilecatalog.BusyDetail

// The instance's log records these tool call failures, whose causes the client never sees.
const (
	listFailed = "moneyflow could not list profiles for a tool call"
	openFailed = "moneyflow could not open a profile for a tool call"
)

// profileBusy reports a failure that ends once another holder lets go: a lock that another
// process holds, or a profile that is being removed or closed.
func profileBusy(err error) bool {
	return errors.Is(err, home.ErrLockBusy) || errors.Is(err, api.ErrProfileUnavailable)
}

// unavailable answers a tool call whose profile cannot be listed or opened. A busy profile tells
// the client why; the instance's log records any other cause under message.
func (tools instanceTools) unavailable(ctx context.Context, message string, err error) error {
	if profileBusy(err) {
		return mcp.UnavailableError{Reason: busyReason}
	}
	tools.logCause(ctx, message, err)
	return fmt.Errorf("%w: %w", mcp.ErrProfileUnavailable, err)
}

// logCause records why a tool call failed, unless the call itself ended or the instance closed.
func (tools instanceTools) logCause(ctx context.Context, message string, err error) {
	if ctx.Err() == nil && !errors.Is(err, ErrClosed) {
		tools.instance.logger.Error(message, "cause", diagnostic.DescribeCause(err))
	}
}

// recoverPanic turns a panic while a tool call lists or acquires profiles into
// mcp.ErrToolPanicked and logs it, because the MCP server that would otherwise recover the panic
// has no logger. Defer it directly, so that it can recover.
func (tools instanceTools) recoverPanic(ctx context.Context, err *error) {
	if value := recover(); value != nil {
		mcp.LogToolPanic(ctx, tools.instance.logger, value)
		*err = mcp.ErrToolPanicked
	}
}

func (tools instanceTools) List(ctx context.Context) (_ []mcp.ProfileSummary, err error) {
	defer tools.recoverPanic(ctx, &err)
	profiles, err := tools.instance.Profiles(ctx)
	if err != nil {
		tools.logCause(ctx, listFailed, err)
		return nil, err
	}
	summaries := make([]mcp.ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		summaries = append(summaries, mcp.ProfileSummary{
			ID: profile.ID, Name: profile.Name, Provider: profile.ProviderKind,
			State: string(profile.State),
		})
	}
	return summaries, nil
}

func (tools instanceTools) Acquire(
	ctx context.Context,
	selector string,
) (_ mcp.Dependencies, _ func(), err error) {
	defer tools.recoverPanic(ctx, &err)
	instance := tools.instance
	entry, err := tools.choose(ctx, selector)
	if err != nil {
		return mcp.Dependencies{}, nil, err
	}
	if problem := entryProblem(entry); problem != "" {
		return mcp.Dependencies{}, nil, mcp.UnavailableError{Reason: problem}
	}
	supervisor, err := instance.supervisor(entry.ID)
	if err != nil {
		return mcp.Dependencies{}, nil, tools.unavailable(ctx, openFailed, err)
	}
	lease, err := instance.registry.Acquire(ctx, entry.ID)
	if err != nil {
		err = fmt.Errorf("acquire profile %s: %w", entry.ID, err)
		return mcp.Dependencies{}, nil, tools.unavailable(ctx, openFailed, err)
	}
	return mcp.Dependencies{
		Service: lease.Service(), ProfileID: entry.ID, ProfileName: entry.DisplayName,
		ProfileRoot: lease.ProfileRoot(), Clock: time.Now, Random: cryptorand.Reader,
		Logger: instance.logger, Supervisor: supervisor,
	}, func() { _ = lease.Release() }, nil
}

// choose selects the profile a tool call names, or the only profile when it names none.
func (tools instanceTools) choose(
	ctx context.Context,
	selector string,
) (profilecatalog.Entry, error) {
	entries, err := tools.instance.list(ctx)
	if err != nil {
		return profilecatalog.Entry{}, tools.unavailable(ctx, listFailed, err)
	}
	if selector == "" {
		switch len(entries) {
		case 0:
			return profilecatalog.Entry{}, mcp.ErrProfileNotFound
		case 1:
			return entries[0], nil
		default:
			return profilecatalog.Entry{}, mcp.ErrProfileRequired
		}
	}
	for _, entry := range entries {
		if entry.ID == selector {
			return entry, nil
		}
	}
	return profilecatalog.Entry{}, mcp.ErrProfileNotFound
}
