package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
)

// ProfileCatalogSchemaVersion identifies the browser catalog wire contract.
const ProfileCatalogSchemaVersion = "1"

// ProfileSummary contains selector-safe local metadata and no filesystem path.
type ProfileSummary struct {
	Key          string `json:"key"`
	ID           string `json:"id,omitempty"`
	DisplayName  string `json:"display_name"`
	ProviderKind string `json:"provider_kind"`
	Status       string `json:"status"`
}

// ProfileCatalogResponse lists locally discovered profiles in catalog order.
type ProfileCatalogResponse struct {
	Version  string           `json:"version"`
	Profiles []ProfileSummary `json:"profiles"`
}

// ProfileCreateBody creates one pristine persistent profile.
type ProfileCreateBody struct {
	Version      string `json:"version"`
	DisplayName  string `json:"display_name" minLength:"1" maxLength:"320"`
	ProviderKind string `json:"provider_kind" enum:"monarch,local"`
}

// ProfileActivateBody turns a catalog-only key into a durable canonical profile identity.
type ProfileActivateBody struct {
	Version      string `json:"version"`
	Key          string `json:"key" minLength:"1" maxLength:"128"`
	ProviderKind string `json:"provider_kind,omitempty" enum:"monarch,local"`
}

// ProfileResponse returns one selector-safe profile.
type ProfileResponse struct {
	Version string         `json:"version"`
	Profile ProfileSummary `json:"profile"`
}

// ProfileCancelBody requests rollback of one newly created artifact-free profile.
type ProfileCancelBody struct {
	Version string `json:"version"`
}

// ProfileCancelResponse reports whether the pristine profile was removed.
type ProfileCancelResponse struct {
	Version string `json:"version"`
	Removed bool   `json:"removed"`
}

// RecoveryPlan is the exact destructive action previewed before confirmation.
type RecoveryPlan struct {
	ProfileKey   string `json:"profile_key"`
	ProfileID    string `json:"profile_id"`
	BackupPath   string `json:"backup_path"`
	StartedAt    string `json:"started_at" format:"date-time"`
	OriginalCode string `json:"original_code"`
	InProgress   bool   `json:"in_progress"`
}

// RecoveryBody previews or confirms one exact recovery plan.
type RecoveryBody struct {
	Version   string        `json:"version"`
	Confirmed bool          `json:"confirmed"`
	Plan      *RecoveryPlan `json:"plan,omitempty"`
}

// RecoveryResponse returns either a preview or the retained backup after completion.
type RecoveryResponse struct {
	Version    string       `json:"version"`
	Plan       RecoveryPlan `json:"plan"`
	Recreated  bool         `json:"recreated"`
	BackupPath string       `json:"backup_path,omitempty"`
}

type profileCatalogOutput struct{ Body ProfileCatalogResponse }
type profileCreateInput struct{ Body ProfileCreateBody }
type profileActivateInput struct{ Body ProfileActivateBody }
type profileOutput struct{ Body ProfileResponse }
type profileCancelInput struct {
	ProfileID string `path:"profile_id"`
	Body      ProfileCancelBody
}
type profileCancelOutput struct{ Body ProfileCancelResponse }
type recoveryInput struct {
	ProfileID string `path:"profile_id"`
	Body      RecoveryBody
}
type recoveryOutput struct{ Body RecoveryResponse }

func (server *Server) registerProfileCatalogEndpoints(config Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "listProfiles", Method: http.MethodGet,
		Path: server.basePath + "api/v1/profiles", Summary: "List local profiles",
		Errors: []int{500, 503},
	}, func(ctx context.Context, _ *struct{}) (*profileCatalogOutput, error) {
		if config.Catalog == nil {
			return nil, profileCatalogUnavailable()
		}
		entries, err := config.Catalog.List(ctx)
		if err != nil {
			return nil, problemFromCatalogError(err)
		}
		profiles := make([]ProfileSummary, 0, len(entries))
		for _, entry := range entries {
			profiles = append(profiles, profileSummaryToWire(entry))
		}
		return &profileCatalogOutput{Body: ProfileCatalogResponse{
			Version: ProfileCatalogSchemaVersion, Profiles: profiles,
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "activateProfile", Method: http.MethodPost,
		Path:    server.basePath + "api/v1/profiles/activate",
		Summary: "Activate a catalog profile and return its durable identity",
		Errors:  []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *profileActivateInput) (*profileOutput, error) {
		if config.Catalog == nil {
			return nil, profileCatalogUnavailable()
		}
		if input.Body.Version != ProfileCatalogSchemaVersion {
			return nil, newProblem(
				http.StatusUnprocessableEntity, string(CodeProfileInvalid),
				"The profile request version is unsupported.",
			)
		}
		entry, err := config.Catalog.ActivateForProvider(
			ctx, input.Body.Key, input.Body.ProviderKind,
		)
		if err != nil {
			return nil, problemFromCatalogError(err)
		}
		return &profileOutput{Body: ProfileResponse{
			Version: ProfileCatalogSchemaVersion, Profile: profileSummaryToWire(entry),
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "createProfile", Method: http.MethodPost,
		Path: server.basePath + "api/v1/profiles", Summary: "Create a pristine local profile",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *profileCreateInput) (*profileOutput, error) {
		if config.Catalog == nil {
			return nil, profileCatalogUnavailable()
		}
		if input.Body.Version != ProfileCatalogSchemaVersion {
			return nil, newProblem(
				http.StatusUnprocessableEntity, string(CodeProfileInvalid),
				"The profile request version is unsupported.",
			)
		}
		entry, err := config.Catalog.Create(ctx, profilecatalog.CreateRequest{
			DisplayName: input.Body.DisplayName, ProviderKind: input.Body.ProviderKind,
		})
		if err != nil {
			return nil, problemFromCatalogError(err)
		}
		return &profileOutput{Body: ProfileResponse{
			Version: ProfileCatalogSchemaVersion, Profile: profileSummaryToWire(entry),
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "cancelNewProfile", Method: http.MethodPost,
		Path: server.profilePath("cancel"), Summary: "Cancel one newly created pristine profile",
		Errors: []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *profileCancelInput) (*profileCancelOutput, error) {
		if config.Catalog == nil || config.Evictor == nil {
			return nil, profileCatalogUnavailable()
		}
		if input.Body.Version != ProfileCatalogSchemaVersion {
			return nil, newProblem(
				http.StatusUnprocessableEntity, string(CodeProfileInvalid),
				"The profile request version is unsupported.",
			)
		}
		if err := releaseProfileForRollback(ctx, config, input.ProfileID); err != nil {
			return nil, err
		}
		removed, err := config.Catalog.CancelNewProfile(ctx, input.ProfileID)
		if err != nil {
			return nil, problemFromCatalogError(err)
		}
		return &profileCancelOutput{Body: ProfileCancelResponse{
			Version: ProfileCatalogSchemaVersion, Removed: removed,
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "recoverProfile", Method: http.MethodPost,
		Path: server.profilePath("recovery"), Summary: "Preview or confirm profile recovery",
		Errors: []int{400, 403, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *recoveryInput) (*recoveryOutput, error) {
		if config.Catalog == nil || config.Evictor == nil {
			return nil, profileCatalogUnavailable()
		}
		if input.Body.Version != ProfileCatalogSchemaVersion {
			return nil, newProblem(
				http.StatusUnprocessableEntity, string(CodeProfileInvalid),
				"The profile request version is unsupported.",
			)
		}
		if !input.Body.Confirmed {
			plan, err := config.Catalog.RecoveryPlan(ctx, input.ProfileID)
			if err != nil {
				return nil, problemFromCatalogError(err)
			}
			return &recoveryOutput{Body: RecoveryResponse{
				Version: ProfileCatalogSchemaVersion, Plan: recoveryPlanToWire(plan),
			}}, nil
		}
		if input.Body.Plan == nil {
			return nil, newProblem(
				http.StatusUnprocessableEntity, string(CodeProfileRecoveryUnavailable),
				"The recovery plan must be previewed before confirmation.",
			)
		}
		plan, err := recoveryPlanFromWire(*input.Body.Plan)
		if err != nil || plan.ProfileID != input.ProfileID {
			return nil, newProblem(
				http.StatusConflict, string(CodeProfileRecoveryIncomplete),
				"The recovery plan no longer matches this profile.",
			)
		}
		if err = releaseProfileForRollback(ctx, config, input.ProfileID); err != nil {
			return nil, err
		}
		result, err := config.Catalog.Recreate(ctx, profilecatalog.RecoveryRequest{
			Plan: plan, Confirmed: true,
		})
		if err != nil {
			return nil, problemFromCatalogError(err)
		}
		return &recoveryOutput{Body: RecoveryResponse{
			Version: ProfileCatalogSchemaVersion, Plan: *input.Body.Plan,
			Recreated: true, BackupPath: result.BackupPath,
		}}, nil
	})
}

// releaseProfileForRollback cancels every onboarding attempt for a profile, including attempts
// whose IDs the browser lost, and then evicts the cached service before destructive catalog work.
func releaseProfileForRollback(ctx context.Context, config Config, profileID string) error {
	if config.Onboarding != nil {
		if err := config.Onboarding.CancelProfile(ctx, profileID); err != nil {
			return problemFromOnboardingError(err)
		}
	}
	if err := config.Evictor.Evict(ctx, profileID); err != nil {
		return problemFromCatalogError(err)
	}
	return nil
}

func profileCatalogUnavailable() *Problem {
	return newProblem(
		http.StatusServiceUnavailable, "service_unavailable",
		"Profile management is unavailable.",
	)
}

func profileSummaryToWire(entry profilecatalog.Entry) ProfileSummary {
	return ProfileSummary{
		Key: entry.Key, ID: entry.ID, DisplayName: entry.DisplayName,
		ProviderKind: entry.ProviderKind, Status: string(entry.Status),
	}
}

func recoveryPlanToWire(plan profilecatalog.RecoveryPlan) RecoveryPlan {
	return RecoveryPlan{
		ProfileKey: plan.ProfileKey, ProfileID: plan.ProfileID, BackupPath: plan.BackupPath,
		StartedAt:    plan.StartedAt.UTC().Format(time.RFC3339Nano),
		OriginalCode: string(plan.OriginalCode), InProgress: plan.InProgress,
	}
}

func recoveryPlanFromWire(plan RecoveryPlan) (profilecatalog.RecoveryPlan, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, plan.StartedAt)
	if err != nil || plan.StartedAt != startedAt.UTC().Format(time.RFC3339Nano) {
		return profilecatalog.RecoveryPlan{}, errors.New("recovery time is invalid")
	}
	return profilecatalog.RecoveryPlan{
		ProfileKey: plan.ProfileKey, ProfileID: plan.ProfileID, BackupPath: plan.BackupPath,
		StartedAt: startedAt, OriginalCode: store.ErrorCode(plan.OriginalCode),
		InProgress: plan.InProgress,
	}, nil
}

func problemFromCatalogError(err error) *Problem {
	code := profilecatalog.CodeOf(err)
	status := http.StatusInternalServerError
	detail := "The profile request could not be completed."
	switch code {
	case profilecatalog.CodeProfileNotFound:
		status, detail = http.StatusNotFound, "The requested profile was not found."
	case profilecatalog.CodeProfileAmbiguous:
		status, detail = http.StatusConflict, "The profile selection is ambiguous."
	case profilecatalog.CodeProfileNameConflict:
		status, detail = http.StatusConflict, "The profile name is already in use."
	case profilecatalog.CodeProfileInvalid:
		status, detail = http.StatusUnprocessableEntity, "The profile metadata is invalid."
	case profilecatalog.CodeManifestUnsupported:
		status, detail = http.StatusConflict, "The profile requires another Moneyflow version."
	case profilecatalog.CodeProfileBusy:
		status, detail = http.StatusServiceUnavailable, "The profile is currently in use."
	case profilecatalog.CodeRecoveryIncomplete:
		status, detail = http.StatusConflict, "The profile recovery state is incomplete."
	case profilecatalog.CodeRecoveryUnavailable:
		status, detail = http.StatusConflict, "This profile cannot be recovered by this Moneyflow version."
	default:
		return newProblem(status, "internal_error", detail)
	}
	return newProblem(status, string(code), detail)
}
