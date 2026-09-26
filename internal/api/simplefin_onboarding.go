package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// SimpleFINOnboardingStartBody starts one profile-bound process-local SimpleFIN attempt.
type SimpleFINOnboardingStartBody struct {
	ProtocolVersion uint16 `json:"protocol_version"`
}

// SimpleFINOnboardingSubmitBody applies one exact versioned SimpleFIN transition.
type SimpleFINOnboardingSubmitBody struct {
	ProtocolVersion      uint16                              `json:"protocol_version"`
	ExpectedStateVersion uint64                              `json:"expected_state_version"`
	Action               simplefinonboarding.ActionType      `json:"action"`
	Input                string                              `json:"input,omitempty" maxLength:"8192"`
	Settings             SimpleFINOnboardingSettingsResponse `json:"settings"`
}

// SimpleFINOnboardingCancelBody cancels one exact versioned SimpleFIN state.
type SimpleFINOnboardingCancelBody struct {
	ProtocolVersion      uint16 `json:"protocol_version"`
	ExpectedStateVersion uint64 `json:"expected_state_version"`
}

// SimpleFINOnboardingSettingsResponse reports the selected plan's exact money interpretation.
type SimpleFINOnboardingSettingsResponse struct {
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
}

// SimpleFINOnboardingFailureResponse is one sanitized presenter outcome.
type SimpleFINOnboardingFailureResponse struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	CanRetry   bool   `json:"can_retry"`
	CanReenter bool   `json:"can_reenter"`
}

// SimpleFINOnboardingStatusResponse is the complete credential-blind browser state.
type SimpleFINOnboardingStatusResponse struct {
	ProtocolVersion      uint16                               `json:"protocol_version"`
	AttemptID            string                               `json:"attempt_id"`
	ProfileID            string                               `json:"profile_id"`
	StateVersion         uint64                               `json:"state_version"`
	State                simplefinonboarding.State            `json:"state"`
	ProviderKind         string                               `json:"provider_kind"`
	Progress             ProviderProgress                     `json:"progress"`
	NextEligible         string                               `json:"next_eligible,omitempty" format:"date-time"`
	Settings             *SimpleFINOnboardingSettingsResponse `json:"settings,omitempty"`
	ImportedTransactions int                                  `json:"imported_transactions"`
	Failure              *SimpleFINOnboardingFailureResponse  `json:"failure,omitempty"`
}

type simplefinOnboardingStartInput struct {
	ProfileID string `path:"profile_id"`
	Body      SimpleFINOnboardingStartBody
}
type simplefinOnboardingAttemptInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
}
type simplefinOnboardingSubmitInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
	Body      SimpleFINOnboardingSubmitBody
}
type simplefinOnboardingCancelInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
	Body      SimpleFINOnboardingCancelBody
}
type simplefinOnboardingOutput struct {
	Body SimpleFINOnboardingStatusResponse
}

func (server *Server) registerSimpleFINOnboardingEndpoints(config Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "startSimpleFINOnboarding", Method: http.MethodPost,
		Path: server.profilePath("simplefin-onboarding/start"), Summary: "Start SimpleFIN onboarding",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *simplefinOnboardingStartInput) (*simplefinOnboardingOutput, error) {
		if config.SimpleFINOnboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != simplefinonboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		if err := config.SimpleFINOnboarding.CancelProfile(ctx, input.ProfileID); err != nil {
			return nil, problemFromSimpleFINOnboardingError(err)
		}
		snapshot, err := config.SimpleFINOnboarding.Start(ctx, simplefinonboarding.StartRequest{
			ProfileID: input.ProfileID, Renderer: "web",
		})
		if err != nil {
			return nil, problemFromSimpleFINOnboardingError(err)
		}
		return server.simplefinOnboardingOutput(ctx, config.SimpleFINOnboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "submitSimpleFINOnboarding", Method: http.MethodPost,
		Path:    server.profilePath("simplefin-onboarding/{attempt_id}/submit"),
		Summary: "Submit one SimpleFIN onboarding transition",
		Errors:  []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *simplefinOnboardingSubmitInput) (*simplefinOnboardingOutput, error) {
		if config.SimpleFINOnboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != simplefinonboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		token := []byte(input.Body.Input)
		input.Body.Input = ""
		defer clear(token)
		snapshot, err := config.SimpleFINOnboarding.Submit(ctx, simplefinonboarding.SubmitRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
			ExpectedStateVersion: input.Body.ExpectedStateVersion, Action: input.Body.Action,
			Input: token, Settings: simplefin.ImportConfig{Currency: domain.Currency(input.Body.Settings.Currency), Scale: input.Body.Settings.Scale},
		})
		if err != nil {
			return nil, problemFromSimpleFINOnboardingError(err)
		}
		return server.simplefinOnboardingOutput(ctx, config.SimpleFINOnboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "cancelSimpleFINOnboarding", Method: http.MethodPost,
		Path:    server.profilePath("simplefin-onboarding/{attempt_id}/cancel"),
		Summary: "Cancel SimpleFIN onboarding",
		Errors:  []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *simplefinOnboardingCancelInput) (*simplefinOnboardingOutput, error) {
		if config.SimpleFINOnboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != simplefinonboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		snapshot, err := config.SimpleFINOnboarding.Cancel(ctx, simplefinonboarding.CancelRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
			ExpectedStateVersion: input.Body.ExpectedStateVersion,
		})
		if err != nil {
			return nil, problemFromSimpleFINOnboardingError(err)
		}
		return server.simplefinOnboardingOutput(ctx, config.SimpleFINOnboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "readSimpleFINOnboardingStatus", Method: http.MethodGet,
		Path:    server.profilePath("simplefin-onboarding/{attempt_id}"),
		Summary: "Read credential-blind SimpleFIN onboarding status", Errors: []int{404, 409, 500, 503},
	}, func(ctx context.Context, input *simplefinOnboardingAttemptInput) (*simplefinOnboardingOutput, error) {
		if config.SimpleFINOnboarding == nil {
			return nil, onboardingUnavailable()
		}
		snapshot, err := config.SimpleFINOnboarding.Status(ctx, simplefinonboarding.StatusRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
		})
		if err != nil {
			return nil, problemFromSimpleFINOnboardingError(err)
		}
		return server.simplefinOnboardingOutput(ctx, config.SimpleFINOnboarding, snapshot)
	})

}

func (server *Server) simplefinOnboardingOutput(
	ctx context.Context,
	coordinator SimpleFINOnboardingCoordinator,
	snapshot simplefinonboarding.Snapshot,
) (*simplefinOnboardingOutput, error) {
	if snapshot.State == simplefinonboarding.StateComplete {
		key := "simplefin\x00" + snapshot.ProfileID + "\x00" + snapshot.AttemptID
		if problem := server.releaseCompletedProfile(ctx, key, func(takeContext context.Context) (func() error, error) {
			opened, err := coordinator.TakeOpenedProfile(takeContext, simplefinonboarding.StatusRequest{
				ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
			})
			return opened.Close, err
		}, problemFromSimpleFINOnboardingError); problem != nil {
			return nil, problem
		}
	}
	return &simplefinOnboardingOutput{Body: simplefinOnboardingSnapshotToWire(snapshot)}, nil
}

func simplefinOnboardingSnapshotToWire(snapshot simplefinonboarding.Snapshot) SimpleFINOnboardingStatusResponse {
	response := SimpleFINOnboardingStatusResponse{
		ProtocolVersion: snapshot.ProtocolVersion, AttemptID: snapshot.AttemptID,
		ProfileID: snapshot.ProfileID, StateVersion: snapshot.StateVersion,
		State: snapshot.State, ProviderKind: snapshot.ProviderKind,
		Progress:             ProviderProgress{Fetched: snapshot.Progress.Fetched, Total: snapshot.Progress.Total},
		ImportedTransactions: snapshot.ImportedTransactions,
	}
	if !snapshot.NextEligible.IsZero() {
		response.NextEligible = snapshot.NextEligible.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.Settings != nil {
		response.Settings = &SimpleFINOnboardingSettingsResponse{
			Currency: string(snapshot.Settings.Currency), Scale: snapshot.Settings.Scale,
		}
	}
	if snapshot.Failure != nil {
		response.Failure = &SimpleFINOnboardingFailureResponse{
			Code: snapshot.Failure.Code, Message: snapshot.Failure.Message,
			CanRetry: snapshot.Failure.CanRetry, CanReenter: snapshot.Failure.CanReenter,
		}
	}
	return response
}

func problemFromSimpleFINOnboardingError(err error) *Problem {
	code := simplefinonboarding.CodeOf(err)
	status := http.StatusInternalServerError
	detail := "The SimpleFIN onboarding request could not be completed."
	switch code {
	case "onboarding_stale":
		status, detail = http.StatusConflict, "The onboarding state changed. Refresh and try again."
	case "onboarding_expired":
		status, detail = http.StatusNotFound, "The onboarding attempt expired."
	case "onboarding_canceled":
		status, detail = http.StatusConflict, "The onboarding attempt was canceled."
	case "input_invalid":
		status, detail = http.StatusUnprocessableEntity, "The submitted SimpleFIN onboarding input is invalid."
	case "local_only":
		status, detail = http.StatusConflict, "This local-only profile cannot be connected."
	case "store_error":
		return newProblem(status, "internal_error", detail)
	default:
		return newProblem(status, "internal_error", detail)
	}
	return newProblem(status, string(code), detail)
}
