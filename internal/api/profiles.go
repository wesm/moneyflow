package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type resolvedProfileContextKey struct{}
type legacyProfileContextKey struct{}

type resolvedProfile struct {
	id      string
	service *app.Service
}

// ProfileLease keeps one profile service alive for the duration of a request.
type ProfileLease interface {
	Service() *app.Service
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

func resolveProfileRequests(
	next http.Handler,
	basePath string,
	resolver ProfileResolver,
) http.Handler {
	prefix := basePath + "api/v1/profiles/"
	resolvedEndpoints := map[string]struct{}{
		"bootstrap": {}, "health": {}, "view": {}, "view/transition": {},
		"mutations": {}, "undo": {}, "redo": {}, "commit": {}, "review": {},
		"review/targets": {}, "editor-catalog": {}, "provider/status": {},
		"provider/refresh": {}, "provider/refresh/confirm": {},
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
		if err != nil || lease == nil || lease.Service() == nil {
			writeProblem(response, newProblem(
				http.StatusNotFound, "not_found", "The requested profile was not found.",
			))
			return
		}
		defer func() { _ = lease.Release() }()
		ctx := context.WithValue(request.Context(), resolvedProfileContextKey{}, resolvedProfile{
			id: profileID, service: lease.Service(),
		})
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func profileService(ctx context.Context) *app.Service {
	profile, _ := ctx.Value(resolvedProfileContextKey{}).(resolvedProfile)
	return profile.service
}

func legacyProfileRoutes(next http.Handler, basePath string, profileID string) http.Handler {
	if profileID == "" {
		return next
	}
	endpoints := map[string]struct{}{
		"health": {}, "view": {}, "view/transition": {}, "mutations": {},
		"undo": {}, "redo": {}, "commit": {}, "review": {}, "review/targets": {},
		"editor-catalog": {}, "provider/status": {}, "provider/refresh": {},
		"provider/refresh/confirm": {},
	}
	prefix := basePath + "api/v1/"
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		endpoint := strings.TrimPrefix(request.URL.Path, prefix)
		if request.URL.Path == prefix+endpoint {
			if _, ok := endpoints[endpoint]; ok {
				path, err := ProfileAPIPath(basePath, profileID, endpoint)
				if err != nil {
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
