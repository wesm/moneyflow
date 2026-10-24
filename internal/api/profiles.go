package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/diagnostic"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store"
)

type resolvedProfileContextKey struct{}
type legacyProfileContextKey struct{}

type resolvedProfile struct {
	id        string
	service   *app.Service
	root      string
	temporary bool
}

// ProfileLease keeps one profile service alive for the duration of a request.
type ProfileLease interface {
	Service() *app.Service
	ProfileRoot() string
	Temporary() bool
	Release() error
}

// ProfileResolver acquires profile services by canonical persistent identity.
type ProfileResolver interface {
	Acquire(context.Context, string) (ProfileLease, error)
}

// ProfileCatalog is the exact local lifecycle surface exposed to the browser.
type ProfileCatalog interface {
	List(context.Context) ([]profilecatalog.Entry, error)
	Create(context.Context, profilecatalog.CreateRequest) (profilecatalog.Entry, error)
	CancelNewProfile(context.Context, string) (bool, error)
	ActivateForProvider(context.Context, string, string) (profilecatalog.Entry, error)
	RecoveryPlan(context.Context, string) (profilecatalog.RecoveryPlan, error)
	Recreate(context.Context, profilecatalog.RecoveryRequest) (profilecatalog.RecoveryResult, error)
}

// ProfileEvictor closes the server-owned service before destructive recovery.
type ProfileEvictor interface {
	Evict(context.Context, string) error
}

// OnboardingCoordinator is the credential-blind renderer-neutral setup surface.
type OnboardingCoordinator interface {
	Start(context.Context, onboarding.StartRequest) (onboarding.Snapshot, error)
	Status(context.Context, onboarding.StatusRequest) (onboarding.Snapshot, error)
	Submit(context.Context, onboarding.SubmitRequest) (onboarding.Snapshot, error)
	Cancel(context.Context, onboarding.CancelRequest) (onboarding.Snapshot, error)
	CancelProfile(context.Context, string) error
	TakeOpenedProfile(context.Context, onboarding.StatusRequest) (onboarding.OpenedProfile, error)
}

// SimpleFINOnboardingCoordinator supplies the read-only provider setup lifecycle.
type SimpleFINOnboardingCoordinator interface {
	Start(context.Context, simplefinonboarding.StartRequest) (simplefinonboarding.Snapshot, error)
	Status(context.Context, simplefinonboarding.StatusRequest) (simplefinonboarding.Snapshot, error)
	Submit(context.Context, simplefinonboarding.SubmitRequest) (simplefinonboarding.Snapshot, error)
	Cancel(context.Context, simplefinonboarding.CancelRequest) (simplefinonboarding.Snapshot, error)
	CancelProfile(context.Context, string) error
	Retains(string) bool
	TakeOpenedProfile(context.Context, simplefinonboarding.StatusRequest) (simplefinonboarding.OpenedProfile, error)
}

// AmazonImportCoordinator is the protected browser import attempt surface.
type AmazonImportCoordinator interface {
	Start(context.Context, amazonimport.StartRequest) (amazonimport.Snapshot, error)
	Stage(context.Context, amazonimport.StageRequest) (amazonimport.Snapshot, error)
	Execute(context.Context, amazonimport.ExecuteRequest) (amazonimport.Snapshot, error)
	Status(context.Context, amazonimport.StatusRequest) (amazonimport.Snapshot, error)
	Cancel(context.Context, amazonimport.CancelRequest) (amazonimport.Snapshot, error)
}

// Profile acquisition failures that a resolver reports through ordinary error wrapping.
var (
	// ErrProfileNotFound reports a profile ID that names no openable profile.
	ErrProfileNotFound = errors.New("profile was not found")
	// ErrProfileUnavailable reports a profile that is closing or being recovered.
	ErrProfileUnavailable = errors.New("profile is closing or being recovered")
)

func resolveProfileRequests(
	next http.Handler,
	basePath string,
	resolver ProfileResolver,
	logger *log.Logger,
	hostAdmission bool,
) http.Handler {
	prefix := basePath + "api/v1/profiles/"
	resolvedEndpoints := map[string]struct{}{
		"health": {}, "view": {}, "view/transition": {}, "view/insights": {},
		"mutations": {}, "undo": {}, "redo": {}, "commit": {}, "review": {},
		"review/targets": {}, "duplicates": {}, "editor-catalog": {}, "merchant-preview": {}, "provider/status": {},
		"export/preview": {}, "export": {},
		"provider/refresh": {}, "provider/refresh/confirm": {},
		"provider/write-status": {}, "provider/write/pause": {},
		"provider/write/resume": {}, "provider/write/reconcile": {},
		"provider/write/reconcile/confirm": {},
		"transaction-information":          {},
	}
	if !hostAdmission {
		// Host admission registers no bootstrap route, so a request for one must not open a profile.
		resolvedEndpoints["bootstrap"] = struct{}{}
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.EscapedPath(), prefix) {
			next.ServeHTTP(response, request)
			return
		}
		profileID, endpoint, err := ParseProfileAPIPath(basePath, request.URL.EscapedPath())
		if err != nil {
			next.ServeHTTP(response, request)
			return
		}
		if _, ok := resolvedEndpoints[endpoint]; !ok {
			next.ServeHTTP(response, request)
			return
		}
		lease, err := resolver.Acquire(request.Context(), profileID)
		if err != nil {
			// A profile that is closing or being recovered answers busy until it can open again,
			// which is no failure to log.
			if !errors.Is(err, ErrProfileUnavailable) {
				logger.Printf(
					"%s %s: the profile could not be opened: %s",
					request.Method, request.URL.Path, diagnostic.DescribeCause(err),
				)
			}
			writeProblem(response, problemFromProfileOpen(err))
			return
		}
		if lease == nil || lease.Service() == nil {
			writeProblem(response, problemFromProfileOpen(ErrProfileNotFound))
			return
		}
		defer func() { _ = lease.Release() }()
		ctx := context.WithValue(request.Context(), resolvedProfileContextKey{}, resolvedProfile{
			id: profileID, service: lease.Service(), root: lease.ProfileRoot(),
			temporary: lease.Temporary(),
		})
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

// problemFromProfileOpen says why a profile could not be opened without exposing the cause.
func problemFromProfileOpen(err error) *Problem {
	var storageFailure *store.Error
	isStorageFailure := errors.As(err, &storageFailure)
	switch {
	case errors.Is(err, ErrProfileNotFound),
		profilecatalog.CodeOf(err) == profilecatalog.CodeProfileNotFound:
		return newProblem(http.StatusNotFound, "not_found", "The requested profile was not found.")
	case errors.Is(err, ErrProfileUnavailable) || errors.Is(err, home.ErrLockBusy):
		return newProblem(http.StatusServiceUnavailable, "profile_busy",
			"The profile is being recovered, closed, or used by another Moneyflow command. "+
				"Wait a moment, then retry.")
	case profilecatalog.CodeOf(err) != "":
		return problemFromCatalogError(err)
	case isStorageFailure && storageFailure.Code == store.CodeSchemaIncompatible:
		return newProblem(http.StatusConflict, "schema_incompatible",
			"This profile uses a preview format Moneyflow no longer opens. Run moneyflow tui "+
				"and choose Add profile to start fresh; the existing profile files are kept.")
	case isStorageFailure && storageFailure.Code == store.CodeSchemaNewer:
		return newProblem(http.StatusConflict, "schema_newer",
			"This profile was saved by a newer Moneyflow. Install that version to open it.")
	default:
		return newProblem(http.StatusInternalServerError, "internal_error",
			"The profile could not be opened.")
	}
}

func profileRoot(ctx context.Context) string {
	profile, _ := ctx.Value(resolvedProfileContextKey{}).(resolvedProfile)
	return profile.root
}

func temporaryProfile(ctx context.Context) bool {
	profile, _ := ctx.Value(resolvedProfileContextKey{}).(resolvedProfile)
	return profile.temporary
}

func profileService(ctx context.Context) *app.Service {
	profile, _ := ctx.Value(resolvedProfileContextKey{}).(resolvedProfile)
	return profile.service
}

var legacyProfileEndpoints = map[string]struct{}{
	"health": {}, "view": {}, "view/transition": {}, "view/insights": {}, "mutations": {},
	"undo": {}, "redo": {}, "commit": {}, "review": {}, "review/targets": {},
	"duplicates": {}, "editor-catalog": {}, "merchant-preview": {}, "provider/status": {}, "provider/refresh": {},
	"export/preview": {}, "export": {},
	"provider/refresh/confirm": {},
	"provider/write-status":    {}, "provider/write/pause": {},
	"provider/write/resume": {}, "provider/write/reconcile": {},
	"provider/write/reconcile/confirm": {},
	"transaction-information":          {},
}

func legacyProfileRoutes(next http.Handler, basePath string, profileID string) http.Handler {
	if profileID == "" {
		return next
	}
	prefix := basePath + "api/v1/"
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		endpoint := strings.TrimPrefix(request.URL.Path, prefix)
		if request.URL.Path == prefix+endpoint {
			if _, ok := legacyProfileEndpoints[endpoint]; ok {
				path, err := ProfileAPIPath(basePath, profileID, endpoint)
				if err != nil {
					noteProblemCause(request.Context(), err)
					writeProblem(response, newProblem(
						http.StatusInternalServerError, "internal_error",
						"The request could not be completed.",
					))
					return
				}
				clone := request.Clone(context.WithValue(
					request.Context(), legacyProfileContextKey{}, true,
				))
				clone.URL.Path = path
				clone.URL.RawPath = ""
				next.ServeHTTP(response, clone)
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func legacyProfileRequest(ctx context.Context) bool {
	legacy, _ := ctx.Value(legacyProfileContextKey{}).(bool)
	return legacy
}

func (server *Server) profilePath(endpoint string) string {
	return server.basePath + "api/v1/profiles/{profile_id}/" + endpoint
}
