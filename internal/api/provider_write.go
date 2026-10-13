package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store"
)

// ProviderWriteSchemaVersion identifies the credential-blind write-control contract.
const ProviderWriteSchemaVersion = "1"

// ProviderWriteStatusResponse contains no item, operation, entity, or provider identities.
type ProviderWriteStatusResponse struct {
	Version         string   `json:"version"`
	Revision        string   `json:"revision" pattern:"^[0-9]+$"`
	Generation      string   `json:"generation" pattern:"^[0-9]+$"`
	BatchVersion    string   `json:"batch_version,omitempty" pattern:"^[0-9]+$"`
	Phase           string   `json:"phase,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	AuditWarning    string   `json:"audit_warning,omitempty"`
	Total           int      `json:"total"`
	Completed       int      `json:"completed"`
	Failed          int      `json:"failed"`
	Remaining       int      `json:"remaining"`
	Overrides       int      `json:"overrides"`
	CompletedAt     string   `json:"completed_at,omitempty" format:"date-time"`
	NextEligible    string   `json:"next_eligible,omitempty" format:"date-time"`
	OwnerRenderer   string   `json:"owner_renderer,omitempty"`
	OwnerInstanceID string   `json:"owner_instance_id,omitempty" maxLength:"128"`
	Actions         []string `json:"actions"`
	// RunFailure says why the last background write stopped while the batch still reads as
	// writing or reconciling. The batch resumes once its lease expires.
	RunFailure *ProviderWriteRunFailure `json:"run_failure,omitempty"`
}

// ProviderWriteRunFailure is the problem code and detail of a stopped background write.
type ProviderWriteRunFailure struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// ProviderWriteControlBody applies one batch-versioned control.
type ProviderWriteControlBody struct {
	Version              string `json:"version"`
	ExpectedBatchVersion string `json:"expected_batch_version" pattern:"^[0-9]+$" maxLength:"20"`
}

// ProviderWriteReconcileBody preserves the exact browser projection during reconciliation.
type ProviderWriteReconcileBody struct {
	ProviderWriteControlBody
	Query     string `json:"query" maxLength:"65536"`
	Selection string `json:"selection,omitempty" maxLength:"1468006"`
	Window    Window `json:"window"`
}

// ProviderWriteConfirmationBody confirms one process-local reconcile candidate.
type ProviderWriteConfirmationBody struct {
	ProviderWriteReconcileBody
	ConfirmationToken string `json:"confirmation_token" minLength:"1" maxLength:"4096"`
}

// ProviderWriteResponse is either a counts-only control result or an authoritative reconciliation.
type ProviderWriteResponse struct {
	Status     ProviderWriteStatusResponse `json:"status"`
	Projection *Projection                 `json:"projection,omitempty"`
	Selection  *SelectionDisposition       `json:"selection,omitempty"`
}

type providerWriteStatusInput struct {
	ProfileID string `path:"profile_id"`
}
type providerWriteControlInput struct {
	ProfileID string `path:"profile_id"`
	Body      ProviderWriteControlBody
}
type providerWriteReconcileInput struct {
	ProfileID string `path:"profile_id"`
	Body      ProviderWriteReconcileBody
}
type providerWriteConfirmationInput struct {
	ProfileID string `path:"profile_id"`
	Body      ProviderWriteConfirmationBody
}
type providerWriteStatusOutput struct{ Body ProviderWriteStatusResponse }
type providerWriteOutput struct{ Body ProviderWriteResponse }

func (server *Server) registerProviderWriteEndpoints(_ Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "providerWriteStatus", Method: http.MethodGet,
		Path:    server.profilePath("provider/write-status"),
		Summary: "Report counts-only provider write status", Errors: []int{500, 503},
	}, func(ctx context.Context, _ *providerWriteStatusInput) (*providerWriteStatusOutput, error) {
		service := profileService(ctx)
		status, revision, generation, err := currentProviderWriteStatus(ctx, service)
		if err != nil {
			return nil, problemFromError(ctx, err)
		}
		server.logSessionCheck(ctx, status.SessionCheckErr)
		return &providerWriteStatusOutput{Body: providerWriteStatusToWire(
			revision, generation, status,
		)}, nil
	})

	registerControl := func(
		operationID string,
		path string,
		action func(context.Context, *app.Service, uint64) (app.ProviderWriteStatus, error),
	) {
		huma.Register(server.api, huma.Operation{
			OperationID: operationID, Method: http.MethodPost, Path: path,
			Summary: "Control one durable provider write batch",
			Errors:  []int{400, 403, 409, 413, 422, 500, 503},
		}, func(ctx context.Context, input *providerWriteControlInput) (*providerWriteOutput, error) {
			expected, err := providerWriteControlVersion(input.Body)
			if err != nil {
				return nil, problemFromError(ctx, err)
			}
			service := profileService(ctx)
			status, err := action(ctx, service, expected)
			if err != nil {
				return nil, problemFromProviderWriteError(ctx, service, err, app.ProviderWriteResult{Status: status})
			}
			_, revision, generation, err := currentProviderWriteStatus(ctx, service)
			if err != nil {
				return nil, problemFromError(ctx, err)
			}
			return &providerWriteOutput{Body: ProviderWriteResponse{Status: providerWriteStatusToWire(
				revision, generation, status,
			)}}, nil
		})
	}
	registerControl("pauseProviderWrite", server.profilePath("provider/write/pause"),
		func(ctx context.Context, service *app.Service, version uint64) (app.ProviderWriteStatus, error) {
			return service.PauseProviderWrite(ctx, version)
		})
	registerControl("resumeProviderWrite", server.profilePath("provider/write/resume"),
		func(ctx context.Context, service *app.Service, version uint64) (app.ProviderWriteStatus, error) {
			return service.ResumeProviderWrite(ctx, version)
		})

	registerReconcile := func(operationID, path string, confirm bool) {
		operation := huma.Operation{
			OperationID: operationID, Method: http.MethodPost, Path: path,
			Summary: "Reconcile one durable provider write batch",
			Errors:  []int{400, 403, 409, 413, 422, 500, 503},
		}
		if confirm {
			huma.Register(server.api, operation, func(
				ctx context.Context, input *providerWriteConfirmationInput,
			) (*providerWriteOutput, error) {
				request, err := providerWriteReconcileRequest(
					input.Body.ProviderWriteReconcileBody, input.Body.ConfirmationToken,
				)
				if err != nil {
					return nil, problemFromError(ctx, err)
				}
				return providerWriteReconcileOutput(ctx, profileService(ctx), request, true)
			})
			return
		}
		huma.Register(server.api, operation, func(
			ctx context.Context, input *providerWriteReconcileInput,
		) (*providerWriteOutput, error) {
			request, err := providerWriteReconcileRequest(input.Body, "")
			if err != nil {
				return nil, problemFromError(ctx, err)
			}
			return providerWriteReconcileOutput(ctx, profileService(ctx), request, false)
		})
	}
	registerReconcile("reconcileProviderWrite", server.profilePath("provider/write/reconcile"), false)
	registerReconcile(
		"confirmProviderWriteReconcile", server.profilePath("provider/write/reconcile/confirm"), true,
	)
}

func providerWriteControlVersion(body ProviderWriteControlBody) (uint64, error) {
	if body.Version != ProviderWriteSchemaVersion {
		return 0, invalidMutationRequest(errors.New("unsupported provider write version"))
	}
	version, err := parseRevision(body.ExpectedBatchVersion)
	if err != nil {
		return 0, invalidMutationRequest(err)
	}
	return version, nil
}

func providerWriteReconcileRequest(
	body ProviderWriteReconcileBody,
	confirmationToken string,
) (app.ProviderWriteReconcileRequest, error) {
	expected, err := providerWriteControlVersion(body.ProviderWriteControlBody)
	if err != nil {
		return app.ProviderWriteReconcileRequest{}, err
	}
	state, _, err := DecodeViewQuery(body.Query)
	if err != nil {
		return app.ProviderWriteReconcileRequest{}, err
	}
	selection := app.SelectionValue(body.Selection)
	if selection == "" {
		selection = app.EmptySelection()
	}
	return app.ProviderWriteReconcileRequest{
		ExpectedVersion: expected, ConfirmationToken: confirmationToken,
		State: state, Selection: selection,
		Window: app.WindowRequest{Offset: body.Window.Offset, Limit: body.Window.Limit},
	}, nil
}

func providerWriteReconcileOutput(
	ctx context.Context,
	service *app.Service,
	request app.ProviderWriteReconcileRequest,
	confirm bool,
) (*providerWriteOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, ProviderRefreshTimeout)
	defer cancel()
	var result app.ProviderWriteResult
	var err error
	if confirm {
		result, err = service.ConfirmProviderWriteReconcile(ctx, request)
	} else {
		result, err = service.StopAndReconcileProviderWrite(ctx, request)
	}
	if err != nil {
		return nil, problemFromProviderWriteError(ctx, service, err, result)
	}
	currentStatus, revision, generation, statusErr := currentProviderWriteStatus(ctx, service)
	if statusErr != nil {
		return nil, problemFromError(ctx, statusErr)
	}
	status := providerWriteStatusToWire(revision, generation, currentStatus)
	canonical, encodeErr := EncodeViewQuery(result.Projection.State)
	if encodeErr != nil {
		return nil, problemFromError(ctx, encodeErr)
	}
	projection := projectionToWire(result.Projection, canonical, nil)
	selection := SelectionDisposition{
		Kind: string(result.SelectionDisposition), Value: string(result.Selection),
	}
	return &providerWriteOutput{Body: ProviderWriteResponse{
		Status: status, Projection: &projection, Selection: &selection,
	}}, nil
}

// logSessionCheck logs why the saved provider session could not be inspected. Until it can be,
// a write waiting for a reconnect keeps asking for one even after the user reconnects.
func (server *Server) logSessionCheck(ctx context.Context, err error) {
	profile, _ := ctx.Value(resolvedProfileContextKey{}).(resolvedProfile)
	cause := ""
	if err != nil {
		cause = describeCause(err)
	}
	previous, _ := server.sessionChecks.Swap(profile.id, cause)
	if cause != "" && previous != cause {
		server.errorLog.Printf(
			"provider write status for %s: the saved provider session could not be inspected: %s",
			profile.id, cause,
		)
	}
}

func currentProviderWriteStatus(
	ctx context.Context,
	service *app.Service,
) (app.ProviderWriteStatus, uint64, uint64, error) {
	if _, err := service.Refresh(ctx); err != nil {
		return app.ProviderWriteStatus{}, service.Revision(), 0, err
	}
	status, err := service.ProviderWriteStatus(ctx)
	if err != nil {
		return app.ProviderWriteStatus{}, service.Revision(), 0, err
	}
	return status, service.Revision(), status.Generation, nil
}

func providerWriteStatusToWire(
	revision uint64,
	generation uint64,
	status app.ProviderWriteStatus,
) ProviderWriteStatusResponse {
	wire := ProviderWriteStatusResponse{
		Version: ProviderWriteSchemaVersion, Revision: strconv.FormatUint(revision, 10),
		Generation: strconv.FormatUint(generation, 10), Phase: string(status.Phase),
		Reason: string(status.AttentionReason), Total: status.Total, Completed: status.Completed,
		AuditWarning: status.AuditWarning,
		Failed:       status.Failed, Remaining: status.Remaining, Overrides: status.Overrides,
		OwnerRenderer: status.OwnerRenderer, OwnerInstanceID: status.OwnerInstanceID,
		Actions: providerWriteActions(status),
	}
	if status.Phase != "" {
		wire.BatchVersion = strconv.FormatUint(status.Version, 10)
	}
	if !status.NextEligible.IsZero() {
		wire.NextEligible = status.NextEligible.UTC().Format(time.RFC3339Nano)
	}
	if !status.CompletedAt.IsZero() {
		wire.CompletedAt = status.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if status.RunFailure != nil {
		wire.RunFailure = &ProviderWriteRunFailure{
			Code: status.RunFailure.Code, Detail: status.RunFailure.Detail,
		}
	}
	return wire
}

func providerWriteActions(status app.ProviderWriteStatus) []string {
	switch status.Phase {
	case store.WritePhaseWriting, store.WritePhaseReconciling:
		if status.OwnerInstanceID == "" {
			if status.Phase == store.WritePhaseReconciling &&
				status.ResumeTarget == store.WriteResumeReconciling {
				return []string{"reconcile"}
			}
			return []string{"resume"}
		}
		return []string{"pause"}
	case store.WritePhasePaused:
		return []string{"resume", "reconcile"}
	case store.WritePhaseAttentionRequired:
		if status.AttentionClass == store.WriteAttentionRetryable || status.CanCheckOutcome {
			return []string{"resume", "reconcile"}
		}
		return []string{"reconcile"}
	case store.WritePhaseRateLimited:
		return []string{"resume", "reconcile"}
	case store.WritePhaseReconnectRequired:
		if status.SessionChanged {
			if status.ResumeTarget == store.WriteResumeReconciling {
				return []string{"reconcile"}
			}
			return []string{"resume"}
		}
		return []string{"reconnect", "reconcile"}
	case store.WritePhaseReconcileConfirmationRequired:
		return []string{"confirm"}
	default:
		return []string{}
	}
}

func problemFromProviderWriteError(
	ctx context.Context,
	service *app.Service,
	err error,
	result app.ProviderWriteResult,
) *Problem {
	problem := problemFromError(ctx, err)
	problem.ProviderWriteConfirmationToken = result.ConfirmationToken
	status, revision, _, statusErr := currentProviderWriteStatus(ctx, service)
	if statusErr != nil {
		status = result.Status
		revision = result.Revision
	}
	if status.Phase != "" {
		wire := providerWriteStatusToWire(revision, status.Generation, status)
		problem.ProviderWrite = &wire
	}
	return problem
}

func (server *Server) startProviderWrite(profileID string) {
	go func() {
		lease, err := server.resolver.Acquire(context.Background(), profileID)
		if err != nil {
			server.errorLog.Printf(
				"provider write for %s could not start: %s", profileID, describeCause(err),
			)
			return
		}
		if lease == nil || lease.Service() == nil {
			return
		}
		defer func() { _ = lease.Release() }()
		if _, err = lease.Service().RunProviderWrite(context.Background()); err != nil {
			server.errorLog.Printf("provider write for %s stopped: %s", profileID, describeCause(err))
		}
	}()
}
