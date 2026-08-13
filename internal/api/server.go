package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// Config supplies immutable dependencies for a stateless API handler.
type Config struct {
	Service  *app.Service
	BasePath string
	Version  string
}

// Health reports non-sensitive process and fixture capability metadata.
type Health struct {
	Version          string `json:"version"`
	APISchemaVersion string `json:"api_schema_version"`
	ReadOnly         bool   `json:"read_only"`
	BasePath         string `json:"base_path"`
	DataStatus       string `json:"data_status"`
}

// Server owns a self-contained mux and its generated Huma contract.
type Server struct {
	basePath string
	handler  http.Handler
	api      huma.API
}

type healthOutput struct {
	Body Health
}

type viewInput struct {
	Body ViewBody
}

type transitionInput struct {
	Body TransitionBody
}

type projectionOutput struct {
	Body Projection
}

// New builds the API without binding a listener or retaining request state.
func New(config Config) (*Server, error) {
	if config.Service == nil {
		return nil, errors.New("new API server: service is required")
	}
	basePath, err := NormalizeBasePath(config.BasePath)
	if err != nil {
		return nil, fmt.Errorf("new API server: %w", err)
	}
	if config.Version == "" {
		config.Version = "dev"
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
	server := &Server{basePath: basePath, api: humaAPI}
	server.register(config, mux)
	server.installProblemSchemas()

	var handler http.Handler = mux
	handler = requestBodyLimit(handler)
	handler = safeProblemResponses(handler)
	handler = recoverAPI(handler)
	handler = noStore(handler)
	server.handler = handler
	return server, nil
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
	healthPath := server.basePath + "api/v1/health"
	viewPath := server.basePath + "api/v1/view"
	transitionPath := server.basePath + "api/v1/view/transition"

	huma.Register(server.api, huma.Operation{
		OperationID: "health", Method: http.MethodGet, Path: healthPath,
		Summary: "Report read-only fixture service health",
	}, func(_ context.Context, _ *struct{}) (*healthOutput, error) {
		return &healthOutput{Body: Health{
			Version: config.Version, APISchemaVersion: APISchemaVersion,
			ReadOnly: true, BasePath: server.basePath, DataStatus: "fixture",
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "projectView", Method: http.MethodPost, Path: viewPath,
		Summary: "Project one stateless read-only view", Errors: []int{400, 413, 422, 500},
	}, func(_ context.Context, input *viewInput) (*projectionOutput, error) {
		state, canonical, err := DecodeViewQuery(input.Body.Query)
		if err != nil {
			return nil, problemFromError(err)
		}
		selection := app.SelectionValue(input.Body.Selection)
		if selection == "" {
			selection = app.EmptySelection()
		}
		projection, err := config.Service.ProjectView(
			state,
			selection,
			app.WindowRequest{Offset: input.Body.Window.Offset, Limit: input.Body.Window.Limit},
		)
		warnings := []Warning(nil)
		if isInvalidHydrationSelection(err) {
			selection = app.EmptySelection()
			projection, err = config.Service.ProjectView(
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
	}, func(_ context.Context, input *transitionInput) (*projectionOutput, error) {
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
		nextState, _, projection, err := config.Service.TransitionView(
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
		if safe.Code == CodeViewStateTooLarge {
			status = http.StatusConflict
		}
		return newProblem(status, string(safe.Code), safe.Detail)
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
			request.Body = http.MaxBytesReader(response, request.Body, MaxViewBodyBytes)
		}
		next.ServeHTTP(response, request)
	})
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (response *bufferedResponse) Header() http.Header {
	return response.header
}

func (response *bufferedResponse) WriteHeader(status int) {
	if response.status == 0 {
		response.status = status
	}
}

func (response *bufferedResponse) Write(data []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return response.body.Write(data)
}

func safeProblemResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		buffered := &bufferedResponse{header: make(http.Header)}
		next.ServeHTTP(buffered, request)
		status := buffered.status
		if status == 0 {
			status = http.StatusOK
		}
		if status < 400 {
			copyHeaders(response.Header(), buffered.header)
			response.WriteHeader(status)
			if request.Method == http.MethodHead {
				return
			}
			_, _ = response.Write(buffered.body.Bytes())
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
