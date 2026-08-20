package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

// AmazonImportWireVersion identifies the browser attempt contract.
const AmazonImportWireVersion = "1"

// AmazonImportStartBody selects immutable import settings for one attempt.
type AmazonImportStartBody struct {
	Version          string `json:"version"`
	Currency         string `json:"currency" minLength:"3" maxLength:"3"`
	Scale            uint8  `json:"scale" maximum:"9"`
	TaxonomySourceID string `json:"taxonomy_source_id,omitempty" maxLength:"128"`
}

// AmazonImportAttemptBody advances an existing versioned import attempt.
type AmazonImportAttemptBody struct {
	Version              string `json:"version"`
	ExpectedStateVersion string `json:"expected_state_version" pattern:"^[0-9]+$" maxLength:"20"`
}

// AmazonImportCoordinate identifies one actionable CSV validation failure.
type AmazonImportCoordinate struct {
	RelativeFilename string `json:"relative_filename" maxLength:"1024"`
	Record           int    `json:"record"`
	Column           string `json:"column" maxLength:"128"`
	Reason           string `json:"reason" maxLength:"128"`
}

// AmazonImportStatusResponse is the credential-blind state of one import attempt.
type AmazonImportStatusResponse struct {
	Version      string                     `json:"version"`
	AttemptID    string                     `json:"attempt_id" maxLength:"128"`
	ProfileID    string                     `json:"profile_id" maxLength:"128"`
	State        amazonimport.State         `json:"state"`
	StateVersion string                     `json:"state_version" pattern:"^[0-9]+$"`
	Progress     amazonimport.Progress      `json:"progress"`
	Result       AmazonImportResultResponse `json:"result"`
	FailureCode  string                     `json:"failure_code,omitempty" maxLength:"128"`
	Coordinate   *AmazonImportCoordinate    `json:"coordinate,omitempty"`
}

// AmazonImportResultResponse summarizes a completed committed import.
type AmazonImportResultResponse struct {
	Revision  string `json:"revision" pattern:"^[0-9]+$"`
	Inserted  int    `json:"inserted"`
	Updated   int    `json:"updated"`
	Restored  int    `json:"restored"`
	Retired   int    `json:"retired"`
	Unchanged int    `json:"unchanged"`
	NoOp      bool   `json:"no_op"`
}

type amazonStartInput struct {
	ProfileID string `path:"profile_id"`
	Body      AmazonImportStartBody
}
type amazonAttemptInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id"`
	Body      AmazonImportAttemptBody
}
type amazonStatusInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id"`
}
type amazonImportOutput struct{ Body AmazonImportStatusResponse }

type amazonUploadForm struct {
	Version              string          `form:"version" required:"true"`
	ExpectedStateVersion string          `form:"expected_state_version" required:"true"`
	Files                []huma.FormFile `form:"files" contentType:"text/csv,application/vnd.ms-excel,text/plain,application/octet-stream" required:"true"`
}
type amazonUploadInput struct {
	ProfileID string `path:"profile_id"`
	AttemptID string `path:"attempt_id"`
	RawBody   huma.MultipartFormFiles[amazonUploadForm]
}

func (server *Server) registerAmazonImportEndpoints(config Config) {
	huma.Register(server.api, huma.Operation{OperationID: "startAmazonImport", Method: http.MethodPost, Path: server.profilePath("amazon-import/start"), Summary: "Start Amazon import", Errors: []int{400, 404, 409, 413, 422, 500}}, func(ctx context.Context, input *amazonStartInput) (*amazonImportOutput, error) {
		if config.AmazonImports == nil {
			return nil, amazonImportUnavailable()
		}
		if input.Body.Version != AmazonImportWireVersion ||
			!domain.IsValidCurrency(domain.Currency(input.Body.Currency)) || input.Body.Scale > 9 {
			return nil, amazonImportProblem(errors.New("invalid start request"))
		}
		var clone *app.TaxonomyClone
		var err error
		if input.Body.TaxonomySourceID != "" {
			if config.LoadAmazonTaxonomy == nil {
				return nil, amazonImportProblem(errors.New("taxonomy source unavailable"))
			}
			clone, err = config.LoadAmazonTaxonomy(ctx, input.Body.TaxonomySourceID)
			if err != nil {
				return nil, amazonImportProblem(err)
			}
		}
		snapshot, err := config.AmazonImports.Start(ctx, amazonimport.StartRequest{ProfileID: input.ProfileID, Settings: amazon.Settings{Currency: domain.Currency(input.Body.Currency), Scale: input.Body.Scale}, TaxonomyClone: clone})
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		return amazonSnapshotOutput(snapshot, nil), nil
	})
	huma.Register(server.api, huma.Operation{OperationID: "stageAmazonImportFiles", Method: http.MethodPost, Path: server.profilePath("amazon-import/{attempt_id}/files"), Summary: "Stage Amazon import files", Errors: []int{400, 404, 409, 413, 422, 500}}, func(ctx context.Context, input *amazonUploadInput) (*amazonImportOutput, error) {
		if config.AmazonImports == nil {
			return nil, amazonImportUnavailable()
		}
		if input.RawBody.Form != nil {
			defer input.RawBody.Form.RemoveAll() //nolint:errcheck // Huma owns these temporary files.
		}
		form := input.RawBody.Data()
		if form == nil || form.Version != AmazonImportWireVersion {
			return nil, amazonImportProblem(errors.New("invalid upload request"))
		}
		expected, err := strconv.ParseUint(form.ExpectedStateVersion, 10, 64)
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		uploads := make([]amazonimport.Upload, 0, len(form.Files))
		for _, file := range form.Files {
			defer file.Close() //nolint:errcheck // Stage makes the authoritative private copy.
			uploads = append(uploads, amazonimport.Upload{RelativeName: file.Filename, Reader: file})
		}
		snapshot, err := config.AmazonImports.Stage(ctx, amazonimport.StageRequest{ProfileID: input.ProfileID, AttemptID: input.AttemptID, ExpectedStateVersion: expected, Files: uploads})
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		return amazonSnapshotOutput(snapshot, nil), nil
	})
	huma.Register(server.api, huma.Operation{OperationID: "executeAmazonImport", Method: http.MethodPost, Path: server.profilePath("amazon-import/{attempt_id}/execute"), Summary: "Execute Amazon import", Errors: []int{400, 404, 409, 413, 422, 500}}, func(ctx context.Context, input *amazonAttemptInput) (*amazonImportOutput, error) {
		if config.AmazonImports == nil {
			return nil, amazonImportUnavailable()
		}
		expected, err := amazonAttemptVersion(input.Body)
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		snapshot, err := config.AmazonImports.Execute(ctx, amazonimport.ExecuteRequest{ProfileID: input.ProfileID, AttemptID: input.AttemptID, ExpectedStateVersion: expected})
		if err != nil {
			coordinate := amazonimport.CoordinateOf(err)
			if coordinate.RelativeFilename != "" {
				return amazonSnapshotOutput(snapshot, &coordinate), nil
			}
			return nil, amazonImportProblem(err)
		}
		return amazonSnapshotOutput(snapshot, nil), nil
	})
	huma.Register(server.api, huma.Operation{OperationID: "readAmazonImportStatus", Method: http.MethodGet, Path: server.profilePath("amazon-import/{attempt_id}/status"), Summary: "Read Amazon import status", Errors: []int{404, 500}}, func(ctx context.Context, input *amazonStatusInput) (*amazonImportOutput, error) {
		if config.AmazonImports == nil {
			return nil, amazonImportUnavailable()
		}
		snapshot, err := config.AmazonImports.Status(ctx, amazonimport.StatusRequest{ProfileID: input.ProfileID, AttemptID: input.AttemptID})
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		return amazonSnapshotOutput(snapshot, nil), nil
	})
	huma.Register(server.api, huma.Operation{OperationID: "cancelAmazonImport", Method: http.MethodPost, Path: server.profilePath("amazon-import/{attempt_id}/cancel"), Summary: "Cancel Amazon import", Errors: []int{400, 404, 409, 500}}, func(ctx context.Context, input *amazonAttemptInput) (*amazonImportOutput, error) {
		if config.AmazonImports == nil {
			return nil, amazonImportUnavailable()
		}
		expected, err := amazonAttemptVersion(input.Body)
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		snapshot, err := config.AmazonImports.Cancel(ctx, amazonimport.CancelRequest{ProfileID: input.ProfileID, AttemptID: input.AttemptID, ExpectedStateVersion: expected})
		if err != nil {
			return nil, amazonImportProblem(err)
		}
		return amazonSnapshotOutput(snapshot, nil), nil
	})
}

func amazonImportUnavailable() *Problem {
	return newProblem(
		http.StatusServiceUnavailable,
		string(CodeAmazonProfileInvalid),
		"Amazon import is unavailable in this process.",
	)
}

func amazonAttemptVersion(body AmazonImportAttemptBody) (uint64, error) {
	if body.Version != AmazonImportWireVersion {
		return 0, errors.New("unsupported Amazon import version")
	}
	return strconv.ParseUint(body.ExpectedStateVersion, 10, 64)
}

func amazonSnapshotOutput(snapshot amazonimport.Snapshot, coordinate *amazon.Coordinate) *amazonImportOutput {
	response := AmazonImportStatusResponse{Version: AmazonImportWireVersion, AttemptID: snapshot.AttemptID, ProfileID: snapshot.ProfileID, State: snapshot.State, StateVersion: strconv.FormatUint(snapshot.StateVersion, 10), Progress: snapshot.Progress, FailureCode: string(snapshot.Failure.Code), Result: AmazonImportResultResponse{Revision: strconv.FormatUint(snapshot.Result.Revision, 10), Inserted: snapshot.Result.Inserted, Updated: snapshot.Result.Updated, Restored: snapshot.Result.Restored, Retired: snapshot.Result.Retired, Unchanged: snapshot.Result.Unchanged, NoOp: snapshot.Result.NoOp}}
	if coordinate != nil {
		response.Coordinate = &AmazonImportCoordinate{RelativeFilename: coordinate.RelativeFilename, Record: coordinate.Record, Column: coordinate.Column, Reason: coordinate.Reason}
	}
	return &amazonImportOutput{Body: response}
}

func amazonImportProblem(err error) *Problem {
	code := amazonimport.CodeOf(err)
	status := http.StatusUnprocessableEntity
	apiCode := CodeAmazonImportInvalid
	detail := "The Amazon import request is invalid."
	switch code {
	case amazonimport.CodeImportBusy:
		status = http.StatusConflict
		apiCode = CodeAmazonImportBusy
		detail = "Another Amazon import is in progress."
	case amazonimport.CodeImportTooLarge:
		status = http.StatusRequestEntityTooLarge
		apiCode = CodeAmazonImportTooLarge
		detail = "The Amazon import is too large."
	case amazonimport.CodeImportEmpty:
		apiCode = CodeAmazonImportEmpty
		detail = "The Amazon import contains no orders."
	case amazonimport.CodeCurrencyMismatch:
		apiCode = CodeAmazonCurrencyMismatch
		detail = "The Amazon import currency does not match the profile."
	case amazonimport.CodeProfileInvalid:
		apiCode = CodeAmazonProfileInvalid
		detail = "The Amazon profile is unavailable."
	case amazonimport.CodeAttemptStale:
		status = http.StatusConflict
		apiCode = CodeAmazonImportStale
		detail = "The Amazon import state changed."
	case amazonimport.CodeAttemptInvalid:
		status = http.StatusNotFound
		apiCode = CodeAmazonImportExpired
		detail = "The Amazon import attempt is unavailable."
	case amazonimport.CodeImportCanceled:
		status = http.StatusConflict
		apiCode = CodeImportCancelled
		detail = "The Amazon import was canceled."
	}
	return newProblem(status, string(apiCode), detail)
}
