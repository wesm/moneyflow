package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/onboarding"
)

// OnboardingStartBody starts one profile-bound process-local attempt.
type OnboardingStartBody struct {
	ProtocolVersion uint16                   `json:"protocol_version"`
	Settings        *OnboardingSettingsInput `json:"settings,omitempty"`
	MonthToDate     bool                     `json:"month_to_date"`
}

// OnboardingSettingsInput confirms the exact money interpretation.
type OnboardingSettingsInput struct {
	Currency string `json:"currency" minLength:"3" maxLength:"3"`
	Scale    uint8  `json:"scale" maximum:"9"`
}

// OnboardingUnlockInput contains one transient vault password.
type OnboardingUnlockInput struct {
	AccountPassword string `json:"account_password" maxLength:"4096"`
}

// OnboardingCredentialsInput contains transient provider and vault setup secrets.
type OnboardingCredentialsInput struct {
	Email           string `json:"email" maxLength:"4096"`
	Password        string `json:"password" maxLength:"4096"`
	TOTPSecret      string `json:"totp_secret" maxLength:"4096"`
	AccountPassword string `json:"account_password" maxLength:"4096"`
	Confirmation    string `json:"confirmation" maxLength:"4096"`
}

// OnboardingSubmitBody applies one exact versioned coordinator transition.
type OnboardingSubmitBody struct {
	ProtocolVersion      uint16                      `json:"protocol_version"`
	ExpectedStateVersion uint64                      `json:"expected_state_version"`
	Action               onboarding.ActionType       `json:"action"`
	Settings             *OnboardingSettingsInput    `json:"settings,omitempty"`
	Unlock               *OnboardingUnlockInput      `json:"unlock,omitempty"`
	Credentials          *OnboardingCredentialsInput `json:"credentials,omitempty"`
}

// OnboardingCancelBody cancels one exact versioned coordinator state.
type OnboardingCancelBody struct {
	ProtocolVersion      uint16 `json:"protocol_version"`
	ExpectedStateVersion uint64 `json:"expected_state_version"`
}

// OnboardingProgressResponse contains only allowlisted counts and timings.
type OnboardingProgressResponse struct {
	Phase     string `json:"phase"`
	Partition string `json:"partition"`
	Fetched   int    `json:"fetched"`
	Total     int    `json:"total"`
	Attempt   int    `json:"attempt"`
	Pass      int    `json:"pass"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// OnboardingFailureResponse is one sanitized presenter outcome.
type OnboardingFailureResponse struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	CanRetry   bool   `json:"can_retry"`
	CanReenter bool   `json:"can_reenter"`
}

// OnboardingStatusResponse is the complete credential-blind browser state.
type OnboardingStatusResponse struct {
	ProtocolVersion uint16                      `json:"protocol_version"`
	AttemptID       string                      `json:"attempt_id"`
	ProfileID       string                      `json:"profile_id"`
	StateVersion    uint64                      `json:"state_version"`
	State           onboarding.State            `json:"state"`
	ProviderKind    string                      `json:"provider_kind"`
	Settings        *OnboardingSettingsInput    `json:"settings,omitempty"`
	Progress        *OnboardingProgressResponse `json:"progress,omitempty"`
	Failure         *OnboardingFailureResponse  `json:"failure,omitempty"`
}

type onboardingStartInput struct {
	ProfileID string `path:"profile_id"`
	Body      OnboardingStartBody
}
type onboardingAttemptInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
}
type onboardingSubmitInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
	Body      OnboardingSubmitBody
}
type onboardingCancelInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id" maxLength:"128"`
	Body      OnboardingCancelBody
}
type onboardingOutput struct{ Body OnboardingStatusResponse }

func (server *Server) registerOnboardingEndpoints(config Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "startProfileOnboarding", Method: http.MethodPost,
		Path: server.profilePath("onboarding/start"), Summary: "Start profile onboarding",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *onboardingStartInput) (*onboardingOutput, error) {
		if config.Onboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != onboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		request := onboarding.StartRequest{
			ProfileID: input.ProfileID, Renderer: "web", MonthToDate: input.Body.MonthToDate,
		}
		if input.Body.Settings != nil {
			request.Settings = &onboarding.SettingsInput{
				Currency: domain.Currency(input.Body.Settings.Currency), Scale: input.Body.Settings.Scale,
			}
		}
		snapshot, err := config.Onboarding.Start(ctx, request)
		if err != nil {
			return nil, problemFromOnboardingError(err)
		}
		return server.onboardingOutput(ctx, config.Onboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "submitProfileOnboarding", Method: http.MethodPost,
		Path:    server.profilePath("onboarding/{attempt_id}/submit"),
		Summary: "Submit one onboarding transition",
		Errors:  []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *onboardingSubmitInput) (*onboardingOutput, error) {
		if config.Onboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != onboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		request := onboarding.SubmitRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
			ExpectedStateVersion: input.Body.ExpectedStateVersion, Action: input.Body.Action,
		}
		if input.Body.Settings != nil {
			request.Settings = &onboarding.SettingsInput{
				Currency: domain.Currency(input.Body.Settings.Currency), Scale: input.Body.Settings.Scale,
			}
		}
		if input.Body.Unlock != nil {
			request.Unlock = &onboarding.UnlockInput{
				AccountPassword: []byte(input.Body.Unlock.AccountPassword),
			}
		}
		if input.Body.Credentials != nil {
			request.Credentials = &onboarding.CredentialInput{
				Email:           []byte(input.Body.Credentials.Email),
				Password:        []byte(input.Body.Credentials.Password),
				TOTPSecret:      []byte(input.Body.Credentials.TOTPSecret),
				AccountPassword: []byte(input.Body.Credentials.AccountPassword),
				Confirmation:    []byte(input.Body.Credentials.Confirmation),
			}
		}
		defer input.Body.clearSecrets()
		snapshot, err := config.Onboarding.Submit(ctx, request)
		if err != nil {
			return nil, problemFromOnboardingError(err)
		}
		return server.onboardingOutput(ctx, config.Onboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "cancelProfileOnboarding", Method: http.MethodPost,
		Path:    server.profilePath("onboarding/{attempt_id}/cancel"),
		Summary: "Cancel profile onboarding",
		Errors:  []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *onboardingCancelInput) (*onboardingOutput, error) {
		if config.Onboarding == nil {
			return nil, onboardingUnavailable()
		}
		if input.Body.ProtocolVersion != onboarding.ProtocolVersion {
			return nil, invalidOnboardingVersion()
		}
		snapshot, err := config.Onboarding.Cancel(ctx, onboarding.CancelRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
			ExpectedStateVersion: input.Body.ExpectedStateVersion,
		})
		if err != nil {
			return nil, problemFromOnboardingError(err)
		}
		return server.onboardingOutput(ctx, config.Onboarding, snapshot)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "profileOnboardingStatus", Method: http.MethodGet,
		Path:    server.profilePath("onboarding/{attempt_id}/status"),
		Summary: "Read credential-blind onboarding status",
		Errors:  []int{404, 409, 500, 503},
	}, func(ctx context.Context, input *onboardingAttemptInput) (*onboardingOutput, error) {
		if config.Onboarding == nil {
			return nil, onboardingUnavailable()
		}
		snapshot, err := config.Onboarding.Status(ctx, onboarding.StatusRequest{
			ProfileID: input.ProfileID, AttemptID: input.AttemptID,
		})
		if err != nil {
			return nil, problemFromOnboardingError(err)
		}
		return server.onboardingOutput(ctx, config.Onboarding, snapshot)
	})
}

func onboardingUnavailable() *Problem {
	return newProblem(
		http.StatusServiceUnavailable, "service_unavailable",
		"Profile onboarding is unavailable.",
	)
}

func (body *OnboardingSubmitBody) clearSecrets() {
	if body == nil {
		return
	}
	if body.Unlock != nil {
		body.Unlock.AccountPassword = ""
	}
	if body.Credentials != nil {
		body.Credentials.Email = ""
		body.Credentials.Password = ""
		body.Credentials.TOTPSecret = ""
		body.Credentials.AccountPassword = ""
		body.Credentials.Confirmation = ""
	}
}

func (server *Server) onboardingOutput(
	ctx context.Context,
	coordinator OnboardingCoordinator,
	snapshot onboarding.Snapshot,
) (*onboardingOutput, error) {
	if snapshot.State == onboarding.StateComplete {
		key := snapshot.ProfileID + "\x00" + snapshot.AttemptID
		completion := &onboardingCompletion{done: make(chan struct{})}
		actual, loaded := server.completedOnboarding.LoadOrStore(key, completion)
		if loaded {
			completion = actual.(*onboardingCompletion)
		} else {
			opened, err := coordinator.TakeOpenedProfile(ctx, onboarding.StatusRequest{
				ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
			})
			if err != nil {
				completion.problem = problemFromOnboardingError(err)
			} else if opened.Close != nil {
				if err = opened.Close(); err != nil {
					completion.problem = newProblem(
						http.StatusInternalServerError, "internal_error",
						"The completed profile could not be released.",
					)
				}
			}
			close(completion.done)
		}
		select {
		case <-completion.done:
			if completion.problem != nil {
				return nil, completion.problem
			}
		case <-ctx.Done():
			return nil, newProblem(
				http.StatusInternalServerError, "internal_error",
				"The completed profile could not be released.",
			)
		}
	}
	return &onboardingOutput{Body: onboardingSnapshotToWire(snapshot)}, nil
}

func onboardingSnapshotToWire(snapshot onboarding.Snapshot) OnboardingStatusResponse {
	response := OnboardingStatusResponse{
		ProtocolVersion: snapshot.ProtocolVersion, AttemptID: snapshot.AttemptID,
		ProfileID: snapshot.ProfileID, StateVersion: snapshot.StateVersion,
		State: snapshot.State, ProviderKind: snapshot.ProviderKind,
	}
	if snapshot.Settings != nil {
		response.Settings = &OnboardingSettingsInput{
			Currency: string(snapshot.Settings.Currency), Scale: snapshot.Settings.Scale,
		}
	}
	if snapshot.Progress != nil {
		response.Progress = &OnboardingProgressResponse{
			Phase: snapshot.Progress.Phase, Partition: snapshot.Progress.Partition,
			Fetched: snapshot.Progress.Fetched, Total: snapshot.Progress.Total,
			Attempt: snapshot.Progress.Attempt, Pass: snapshot.Progress.Pass,
			ElapsedMS: snapshot.Progress.ElapsedMS,
		}
	}
	if snapshot.Failure != nil {
		response.Failure = &OnboardingFailureResponse{
			Code: snapshot.Failure.Code, Message: snapshot.Failure.Message,
			CanRetry: snapshot.Failure.CanRetry, CanReenter: snapshot.Failure.CanReenter,
		}
	}
	return response
}

func invalidOnboardingVersion() *Problem {
	return newProblem(
		http.StatusUnprocessableEntity, string(CodeCredentialInputInvalid),
		"The onboarding protocol version is unsupported.",
	)
}

func problemFromOnboardingError(err error) *Problem {
	code := onboarding.CodeOf(err)
	status := http.StatusInternalServerError
	detail := "The onboarding request could not be completed."
	switch code {
	case onboarding.CodeOnboardingStale:
		status, detail = http.StatusConflict, "The onboarding state changed. Refresh and try again."
	case onboarding.CodeOnboardingExpired:
		status, detail = http.StatusNotFound, "The onboarding attempt expired."
	case onboarding.CodeOnboardingCanceled:
		status, detail = http.StatusConflict, "The onboarding attempt was canceled."
	case onboarding.CodeOnboardingLocalOnly:
		status, detail = http.StatusConflict, "This local-only profile cannot be connected."
	case onboarding.CodeCredentialUnlockFailed:
		status, detail = http.StatusUnprocessableEntity, "The saved credentials could not be unlocked."
	case onboarding.CodeCredentialInputInvalid:
		status, detail = http.StatusUnprocessableEntity, "The submitted onboarding input is invalid."
	case onboarding.CodeProviderConnectInProgress:
		status, detail = http.StatusServiceUnavailable, "Another provider connection is in progress."
	default:
		return newProblem(status, "internal_error", detail)
	}
	return newProblem(status, string(code), detail)
}
