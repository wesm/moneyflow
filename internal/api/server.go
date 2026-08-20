package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/exporter"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// Config supplies immutable dependencies for a stateless API handler.
type Config struct {
	Resolver        ProfileResolver
	LegacyProfileID string
	BasePath        string
	Version         string
	Origin          OriginConfig
	Security        *MutationSecurity
	Catalog         ProfileCatalog
	Evictor         ProfileEvictor
	Onboarding      OnboardingCoordinator
}

// Health reports non-sensitive process and persistent-profile metadata.
type Health struct {
	Version          string         `json:"version"`
	APISchemaVersion string         `json:"api_schema_version"`
	ReadOnly         bool           `json:"read_only"`
	BasePath         string         `json:"base_path"`
	DataStatus       string         `json:"data_status"`
	Revision         string         `json:"revision" pattern:"^[0-9]+$"`
	Pending          PendingSummary `json:"pending"`
}

// Server owns a self-contained mux and its generated Huma contract.
type Server struct {
	basePath            string
	version             string
	handler             http.Handler
	api                 huma.API
	security            *MutationSecurity
	resolver            ProfileResolver
	completedOnboarding sync.Map
}

type onboardingCompletion struct {
	done    chan struct{}
	problem *Problem
}

type healthOutput struct {
	Body Health
}

type viewInput struct {
	ProfileID string `path:"profile_id"`
	Body      ViewBody
}

type transitionInput struct {
	ProfileID string `path:"profile_id"`
	Body      TransitionBody
}

type projectionOutput struct {
	Body Projection
}

type bootstrapOutput struct {
	Body Bootstrap
}

type profileBootstrapInput struct {
	ProfileID string `path:"profile_id"`
}

// New builds the API without binding a listener or retaining request state.
func New(config Config) (*Server, error) {
	if config.Resolver == nil {
		return nil, errors.New("new API server: profile resolver is required")
	}
	if config.LegacyProfileID != "" && !profilecatalog.ValidProfileID(config.LegacyProfileID) {
		return nil, errors.New("new API server: legacy profile ID is invalid")
	}
	if (config.Catalog == nil) != (config.Evictor == nil) {
		return nil, errors.New("new API server: catalog lifecycle dependencies are incomplete")
	}
	basePath, err := NormalizeBasePath(config.BasePath)
	if err != nil {
		return nil, fmt.Errorf("new API server: %w", err)
	}
	if config.Version == "" {
		config.Version = "dev"
	}
	if config.Origin.Canonical == nil {
		config.Origin, err = ResolveOrigin("127.0.0.1:8080", basePath, "")
		if err != nil {
			return nil, fmt.Errorf("new API server origin: %w", err)
		}
	}
	if config.Origin.BasePath != basePath {
		return nil, errors.New("new API server: origin base path differs from API base path")
	}
	if config.Security == nil {
		config.Security, err = NewMutationSecurity(config.Origin, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("new API server security: %w", err)
		}
	}

	mux := http.NewServeMux()
	humaConfig := huma.DefaultConfig("moneyflow", APISchemaVersion)
	humaConfig.OpenAPIPath = ""
	humaConfig.DocsPath = ""
	humaConfig.SchemasPath = ""
	humaConfig.CreateHooks = nil
	humaConfig.Transformers = nil
	humaConfig.Servers = []*huma.Server{{URL: basePath}}
	humaAPI := humago.New(mux, humaConfig)
	server := &Server{
		basePath: basePath, version: config.Version, api: humaAPI,
		security: config.Security, resolver: config.Resolver,
	}
	server.register(config, mux)
	server.registerMutationEndpoints(config)
	server.registerReviewEndpoints(config)
	server.registerDuplicateEndpoint(config)
	server.registerExportEndpoints()
	server.registerEditorCatalogEndpoint(config)
	server.registerProviderEndpoints(config)
	server.registerProviderWriteEndpoints(config)
	server.registerProfileCatalogEndpoints(config)
	server.registerOnboardingEndpoints(config)
	server.installProblemSchemas()

	var handler http.Handler = mux
	handler = requestBodyLimit(handler)
	handler = resolveProfileRequests(handler, basePath, config.Resolver)
	handler = profileMutationSecurity(handler, basePath, config.Security)
	handler = strictProfileAPIPaths(handler, basePath)
	handler = legacyProfileRoutes(handler, basePath, config.LegacyProfileID)
	handler = safeProblemResponses(handler)
	handler = recoverAPI(handler)
	handler = noStore(handler)
	server.handler = handler
	return server, nil
}

func strictProfileAPIPaths(next http.Handler, basePath string) http.Handler {
	prefix := basePath + "api/v1/profiles/"
	catalogPath := strings.TrimSuffix(prefix, "/")
	activatePath := catalogPath + "/activate"
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		escapedPath := request.URL.EscapedPath()
		decodedPath := request.URL.Path
		valid := true
		switch decodedPath {
		case catalogPath, activatePath:
			valid = escapedPath == decodedPath
		default:
			if strings.HasPrefix(decodedPath, prefix) {
				_, _, err := ParseProfileAPIPath(basePath, escapedPath)
				valid = err == nil
			}
		}
		if !valid {
			writeProblem(response, newProblem(
				http.StatusNotFound, "not_found", "The requested profile route was not found.",
			))
			return
		}
		next.ServeHTTP(response, request)
	})
}

func profileMutationSecurity(
	next http.Handler,
	basePath string,
	security *MutationSecurity,
) http.Handler {
	protected := map[string]struct{}{
		"mutations": {}, "undo": {}, "redo": {}, "commit": {},
		"review": {}, "review/targets": {}, "editor-catalog": {},
		"export":           {},
		"provider/refresh": {}, "provider/refresh/confirm": {},
		"provider/write/pause": {}, "provider/write/resume": {},
		"provider/write/reconcile": {}, "provider/write/reconcile/confirm": {},
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			if request.URL.Path == basePath+"api/v1/profiles" ||
				request.URL.Path == basePath+"api/v1/profiles/activate" {
				security.Protect(CatalogMutationScope, next).ServeHTTP(response, request)
				return
			}
			profileID, endpoint, err := ParseProfileAPIPath(basePath, request.URL.EscapedPath())
			if err == nil {
				scope := profileID
				_, requiresProtection := protected[endpoint]
				if endpoint == "recovery" || endpoint == "cancel" {
					requiresProtection = true
					scope = CatalogMutationScope
				}
				if endpoint == "onboarding/start" ||
					(strings.HasPrefix(endpoint, "onboarding/") &&
						(strings.HasSuffix(endpoint, "/submit") || strings.HasSuffix(endpoint, "/cancel"))) {
					requiresProtection = true
				}
				if requiresProtection {
					if legacyProfileRequest(request.Context()) {
						scope = CatalogMutationScope
					}
					security.Protect(scope, next).ServeHTTP(response, request)
					return
				}
			}
		}
		next.ServeHTTP(response, request)
	})
}

// Handler returns the complete API handler without exposing its mux.
func (server *Server) Handler() http.Handler {
	return server.handler
}

// OpenAPIJSON returns stable indented OpenAPI 3.1 JSON.
func (server *Server) OpenAPIJSON() ([]byte, error) {
	raw, err := server.api.OpenAPI().MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAPI JSON: %w", err)
	}
	var output bytes.Buffer
	if err := json.Indent(&output, raw, "", "  "); err != nil {
		return nil, fmt.Errorf("indent OpenAPI JSON: %w", err)
	}
	output.WriteByte('\n')
	return output.Bytes(), nil
}

// OpenAPIYAML returns stable OpenAPI 3.1 YAML.
func (server *Server) OpenAPIYAML() ([]byte, error) {
	output, err := server.api.OpenAPI().YAML()
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAPI YAML: %w", err)
	}
	return output, nil
}

func (server *Server) register(config Config, mux *http.ServeMux) {
	healthPath := server.profilePath("health")
	viewPath := server.profilePath("view")
	transitionPath := server.profilePath("view/transition")
	bootstrapPath := server.basePath + "api/v1/bootstrap"
	profileBootstrapPath := server.profilePath("bootstrap")

	huma.Register(server.api, huma.Operation{
		OperationID: "bootstrap", Method: http.MethodGet, Path: bootstrapPath,
		Summary: "Issue browser-memory mutation configuration", Errors: []int{500},
	}, func(_ context.Context, _ *struct{}) (*bootstrapOutput, error) {
		body, err := newBootstrap(
			config.Version, config.Origin, 0, config.Security,
			CatalogMutationScope,
		)
		if err != nil {
			return nil, newProblem(
				http.StatusInternalServerError, "internal_error",
				"The bootstrap configuration could not be issued.",
			)
		}
		return &bootstrapOutput{Body: body}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "profileBootstrap", Method: http.MethodGet, Path: profileBootstrapPath,
		Summary: "Issue profile-scoped browser mutation configuration", Errors: []int{404, 500},
	}, func(ctx context.Context, input *profileBootstrapInput) (*bootstrapOutput, error) {
		if _, _, parseErr := ParseProfileAPIPath(
			server.basePath,
			server.basePath+"api/v1/profiles/"+input.ProfileID+"/bootstrap",
		); parseErr != nil {
			return nil, newProblem(
				http.StatusNotFound, "not_found", "The requested profile route was not found.",
			)
		}
		service := profileService(ctx)
		if _, refreshErr := service.Refresh(ctx); refreshErr != nil {
			return nil, problemFromError(refreshErr)
		}
		body, bootstrapErr := newBootstrap(
			config.Version, config.Origin, service.Revision(), config.Security,
			input.ProfileID,
		)
		if bootstrapErr != nil {
			return nil, newProblem(
				http.StatusInternalServerError, "internal_error",
				"The bootstrap configuration could not be issued.",
			)
		}
		return &bootstrapOutput{Body: body}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "health", Method: http.MethodGet, Path: healthPath,
		Summary: "Report persistent profile service health",
	}, func(ctx context.Context, _ *profileBootstrapInput) (*healthOutput, error) {
		service := profileService(ctx)
		if _, err := service.Refresh(ctx); err != nil {
			return nil, problemFromError(err)
		}
		return &healthOutput{Body: Health{
			Version: config.Version, APISchemaVersion: APISchemaVersion,
			ReadOnly: false, BasePath: server.basePath, DataStatus: "profile",
			Revision: strconv.FormatUint(service.Revision(), 10),
			Pending:  pendingToWire(service.Pending()),
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "projectView", Method: http.MethodPost, Path: viewPath,
		Summary: "Project one stateless read-only view", Errors: []int{400, 413, 422, 500},
	}, func(ctx context.Context, input *viewInput) (*projectionOutput, error) {
		service := profileService(ctx)
		if _, err := service.Refresh(ctx); err != nil {
			return nil, problemFromError(err)
		}
		state, canonical, err := DecodeViewQuery(input.Body.Query)
		if err != nil {
			return nil, problemFromError(err)
		}
		selection := app.SelectionValue(input.Body.Selection)
		if selection == "" {
			selection = app.EmptySelection()
		}
		projection, err := service.ProjectView(
			state,
			selection,
			app.WindowRequest{Offset: input.Body.Window.Offset, Limit: input.Body.Window.Limit},
		)
		warnings := []Warning(nil)
		if isInvalidHydrationSelection(err) {
			selection = app.EmptySelection()
			projection, err = service.ProjectView(
				state,
				selection,
				app.WindowRequest{Offset: input.Body.Window.Offset, Limit: input.Body.Window.Limit},
			)
			warnings = []Warning{{
				Code:   string(app.SelectionReset),
				Detail: "The saved selection was invalid and has been reset.",
			}}
		}
		if err != nil {
			return nil, problemFromError(err)
		}
		return &projectionOutput{Body: projectionToWire(projection, canonical, warnings)}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "transitionView", Method: http.MethodPost, Path: transitionPath,
		Summary: "Apply one stateless read-only view transition",
		Errors:  []int{400, 409, 413, 422, 500},
	}, func(ctx context.Context, input *transitionInput) (*projectionOutput, error) {
		service := profileService(ctx)
		if _, err := service.Refresh(ctx); err != nil {
			return nil, problemFromError(err)
		}
		state, _, err := DecodeViewQuery(input.Body.Query)
		if err != nil {
			return nil, problemFromError(err)
		}
		selection := app.SelectionValue(input.Body.Selection)
		if selection == "" {
			selection = app.EmptySelection()
		}
		transition, err := transitionToApp(input.Body)
		if err != nil {
			return nil, problemFromError(err)
		}
		nextState, _, projection, err := service.TransitionView(
			state,
			selection,
			transition,
			app.WindowRequest{Offset: input.Body.Window.Offset, Limit: input.Body.Window.Limit},
		)
		if err != nil {
			return nil, problemFromError(err)
		}
		canonical, err := EncodeViewQuery(nextState)
		if err != nil {
			return nil, problemFromError(err)
		}
		return &projectionOutput{Body: projectionToWire(projection, canonical, nil)}, nil
	})

	mux.HandleFunc("GET "+server.basePath+"openapi.json", func(response http.ResponseWriter, _ *http.Request) {
		data, err := server.OpenAPIJSON()
		if err != nil {
			writeProblem(response, newProblem(500, "internal_error", "The API document is unavailable."))
			return
		}
		response.Header().Set("Content-Type", "application/openapi+json")
		_, _ = response.Write(data)
	})
	mux.HandleFunc("GET "+server.basePath+"openapi.yaml", func(response http.ResponseWriter, _ *http.Request) {
		data, err := server.OpenAPIYAML()
		if err != nil {
			writeProblem(response, newProblem(500, "internal_error", "The API document is unavailable."))
			return
		}
		response.Header().Set("Content-Type", "application/openapi+yaml")
		_, _ = response.Write(data)
	})
}

func transitionToApp(body TransitionBody) (app.TransitionRequest, error) {
	transition := app.TransitionRequest{Action: body.Action, Search: body.Search}
	if body.Target != nil {
		transition.Target = &app.RowTarget{
			Kind: body.Target.Kind, Identity: body.Target.Identity,
		}
	}
	if body.Filters == nil {
		return transition, nil
	}
	filters := &app.Filters{
		ShowHidden: body.Filters.ShowHidden, ShowTransfers: body.Filters.ShowTransfers,
	}
	if body.Filters.DateRange != nil {
		start, err := domain.ParseDate(body.Filters.DateRange.Start)
		if err != nil {
			return app.TransitionRequest{}, newSafeError(
				CodeInvalidViewState, "The filter values are invalid.", err,
			)
		}
		end, err := domain.ParseDate(body.Filters.DateRange.End)
		if err != nil {
			return app.TransitionRequest{}, newSafeError(
				CodeInvalidViewState, "The filter values are invalid.", err,
			)
		}
		filters.DateRange = &domain.DateRange{Start: start, End: end}
	}
	transition.Filters = filters
	return transition, nil
}

func (server *Server) installProblemSchemas() {
	document := server.api.OpenAPI()
	problemSchema := document.Components.Schemas.Schema(
		reflect.TypeFor[Problem](),
		true,
		"Problem",
	)
	for _, item := range document.Paths {
		if item == nil {
			continue
		}
		for _, operation := range []*huma.Operation{item.Get, item.Post} {
			if operation == nil {
				continue
			}
			for status, response := range operation.Responses {
				if status < "400" || response == nil {
					continue
				}
				response.Content = map[string]*huma.MediaType{
					"application/problem+json": {Schema: problemSchema},
				}
			}
		}
	}
}

func problemFromError(err error) *Problem {
	var safe *SafeError
	if errors.As(err, &safe) {
		status := http.StatusBadRequest
		switch safe.Code {
		case CodeViewStateTooLarge:
			status = http.StatusConflict
		case CodeInvalidOperation:
			status = http.StatusUnprocessableEntity
		}
		return newProblem(status, string(safe.Code), safe.Detail)
	}
	var application *app.AppError
	if errors.As(err, &application) {
		status := http.StatusInternalServerError
		code := ErrorCode(application.Code)
		switch application.Code {
		case app.AppExportInvalid:
			status = http.StatusBadRequest
		case app.AppExportEmpty:
			status = http.StatusConflict
		case app.AppExportFailed:
		case app.AppRevisionConflict, app.AppSelectionStale:
			status = http.StatusConflict
		case app.AppInvalidOperation:
			status = http.StatusUnprocessableEntity
		case app.AppInvalidTarget:
			status = http.StatusConflict
		case app.AppStoreBusy:
			status = http.StatusServiceUnavailable
		case app.AppJournalFull:
			status = http.StatusConflict
		case app.AppStoreError:
		case app.AppProviderReconnectRequired, app.AppProviderIdentityMismatch,
			app.AppProviderDeletionConfirmationRequired, app.AppProviderConfirmationInvalid,
			app.AppProviderRefreshStale:
			status = http.StatusConflict
		case app.AppProviderSnapshotUnstable, app.AppProviderRefreshInProgress,
			app.AppProviderRateLimited, app.AppProviderUnavailable:
			status = http.StatusServiceUnavailable
		case app.AppProviderDataInvalid:
			status = http.StatusUnprocessableEntity
		case app.AppProviderWriteInProgress, app.AppProviderWriteNotEligible:
			status = http.StatusServiceUnavailable
		case app.AppProviderWriteAttentionRequired, app.AppProviderWriteStale,
			app.AppProviderWritePaused:
			status = http.StatusConflict
		case app.AppProviderWriteUnsupported:
			status = http.StatusUnprocessableEntity
		case app.AppSchemaNewer, app.AppSchemaIncompatible, app.AppStoreCorrupt:
			code = CodeStoreError
		}
		problem := newProblem(status, string(code), application.Detail)
		problem.CurrentRevision = strconv.FormatUint(application.CurrentRevision, 10)
		if application.Code == app.AppSelectionStale {
			selection := application.Selection
			kind := "refreshed"
			if selection == "" || selection == app.EmptySelection() {
				selection = app.EmptySelection()
				kind = "cleared"
			}
			problem.Selection = &SelectionDisposition{Kind: kind, Value: string(selection)}
		}
		return problem
	}
	var exportFailure *exporter.Error
	if errors.As(err, &exportFailure) {
		status := http.StatusInternalServerError
		switch exportFailure.Code {
		case exporter.CodeInvalid:
			status = http.StatusBadRequest
		case exporter.CodeBusy:
			status = http.StatusConflict
		case exporter.CodeCancelled:
			status = http.StatusRequestTimeout
		case exporter.CodeFailed:
		}
		return newProblem(status, string(exportFailure.Code), exportFailure.Detail)
	}
	var selectionErr *app.SelectionError
	if errors.As(err, &selectionErr) {
		status := http.StatusBadRequest
		if selectionErr.Code == app.SelectionTooLarge {
			status = http.StatusConflict
		}
		return newProblem(status, string(selectionErr.Code), selectionErr.Detail)
	}
	var webErr *app.WebError
	if errors.As(err, &webErr) {
		status := http.StatusBadRequest
		if webErr.Code == app.WebNoChange || webErr.Code == app.WebStaleViewTarget {
			status = http.StatusConflict
		}
		return newProblem(status, string(webErr.Code), webErr.Detail)
	}
	return newProblem(500, "internal_error", "The request could not be completed.")
}

func problemFromProviderError(
	err error,
	result app.ProviderRefreshResult,
	capability Capability,
) *Problem {
	problem := problemFromError(err)
	if result.Status.Code != "" || result.Status.ConfirmationToken != "" {
		revision := result.Revision
		if revision == 0 {
			var failure *app.AppError
			if errors.As(err, &failure) {
				revision = failure.CurrentRevision
			}
		}
		status := providerStatusToWire(revision, result.Status)
		status.Capability = capability
		problem.Provider = &status
	}
	return problem
}

func isInvalidHydrationSelection(err error) bool {
	var selectionErr *app.SelectionError
	return errors.As(err, &selectionErr)
}

func requestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			if request.ContentLength > MaxViewBodyBytes {
				writeProblem(response, newProblem(
					http.StatusRequestEntityTooLarge,
					"request_too_large",
					"The request body is too large.",
				))
				return
			}
			body, err := io.ReadAll(io.LimitReader(request.Body, MaxViewBodyBytes+1))
			if err != nil {
				writeProblem(response, newProblem(
					http.StatusBadRequest,
					"invalid_request",
					"The request body is invalid.",
				))
				return
			}
			if len(body) > MaxViewBodyBytes {
				writeProblem(response, newProblem(
					http.StatusRequestEntityTooLarge,
					"request_too_large",
					"The request body is too large.",
				))
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
		}
		next.ServeHTTP(response, request)
	})
}

type bufferedResponse struct {
	target       http.ResponseWriter
	header       http.Header
	body         bytes.Buffer
	status       int
	passthrough  bool
	streaming    bool
	suppressBody bool
}

const maximumBufferedProblemBytes = 1 << 20

func (response *bufferedResponse) Header() http.Header {
	return response.header
}

func (response *bufferedResponse) WriteHeader(status int) {
	if response.status != 0 {
		return
	}
	response.status = status
	if status < 400 && response.streaming {
		copyHeaders(response.target.Header(), response.header)
		response.target.WriteHeader(status)
		response.passthrough = true
	}
}

func (response *bufferedResponse) Write(data []byte) (int, error) {
	if response.status == 0 {
		response.WriteHeader(http.StatusOK)
	}
	if response.passthrough {
		if response.suppressBody {
			return len(data), nil
		}
		return response.target.Write(data)
	}
	remaining := maximumBufferedProblemBytes - response.body.Len()
	if remaining > 0 {
		_, _ = response.body.Write(data[:min(len(data), remaining)])
	}
	return len(data), nil
}

func (response *bufferedResponse) Unwrap() http.ResponseWriter {
	return response.target
}

func safeProblemResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		buffered := &bufferedResponse{
			target: response, header: make(http.Header), suppressBody: request.Method == http.MethodHead,
			streaming: isExportStreamRequest(request),
		}
		next.ServeHTTP(buffered, request)
		status := buffered.status
		if status == 0 {
			status = http.StatusOK
		}
		if status < 400 {
			if !buffered.passthrough {
				copyHeaders(response.Header(), buffered.header)
				response.WriteHeader(status)
				if !buffered.suppressBody {
					_, _ = response.Write(buffered.body.Bytes())
				}
			}
			return
		}
		var problem Problem
		if json.Unmarshal(buffered.body.Bytes(), &problem) == nil && problem.Code != "" {
			writeProblem(response, &problem)
			return
		}
		if bytes.Contains(buffered.body.Bytes(), []byte("request body too large")) {
			status = http.StatusRequestEntityTooLarge
		}
		code := "invalid_request"
		detail := "The request body is invalid."
		if status >= 500 {
			code = "internal_error"
			detail = "The request could not be completed."
		}
		writeProblem(response, newProblem(status, code, detail))
	})
}

func isExportStreamRequest(request *http.Request) bool {
	return request.Method == http.MethodPost &&
		strings.Contains(request.URL.Path, "/api/v1/profiles/") &&
		strings.HasSuffix(request.URL.Path, "/export")
}

func recoverAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				writeProblem(response, newProblem(
					http.StatusInternalServerError,
					"internal_error",
					"The request could not be completed.",
				))
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func writeProblem(response http.ResponseWriter, problem *Problem) {
	response.Header().Set("Content-Type", "application/problem+json")
	response.WriteHeader(problem.Status)
	_ = json.NewEncoder(response).Encode(problem)
}

func copyHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}
