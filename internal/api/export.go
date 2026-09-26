package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/exporter"
)

// ExportWireVersion identifies the profile-scoped export request contract.
const ExportWireVersion = "2"

// ExportPreviewBody requests committed counts for one analytical view.
type ExportPreviewBody struct {
	Version string `json:"version"`
	Query   string `json:"query" maxLength:"65536"`
}

// ExportPreviewResponse is the count-only chooser input.
type ExportPreviewResponse struct {
	Version            string `json:"version"`
	Revision           string `json:"revision" pattern:"^[0-9]+$"`
	FullCount          int    `json:"full_count"`
	FilteredCount      int    `json:"filtered_count"`
	ActiveOperations   int    `json:"active_operations"`
	InactiveOperations int    `json:"inactive_operations"`
	CommitAvailable    bool   `json:"commit_available"`
	TemporaryProfile   bool   `json:"temporary_profile"`
	CanonicalQuery     string `json:"canonical_query"`
}

// ExportBody requests one complete streamed document.
type ExportBody struct {
	Version string          `json:"version"`
	Format  exporter.Format `json:"format" enum:"parquet,csv,sqlite"`
	Scope   app.ExportScope `json:"scope" enum:"full,filtered"`
	Query   string          `json:"query" maxLength:"65536"`
}

type exportPreviewInput struct {
	ProfileID string `path:"profile_id"`
	Body      ExportPreviewBody
}

type exportPreviewOutput struct{ Body ExportPreviewResponse }

type exportInput struct {
	ProfileID string `path:"profile_id"`
	Body      ExportBody
}

func (server *Server) registerExportEndpoints() {
	huma.Register(server.api, huma.Operation{
		OperationID: "previewProfileExport", Method: http.MethodPost,
		Path: server.profilePath("export/preview"), Summary: "Preview committed transaction export counts",
		Errors: []int{400, 404, 413, 422, 500},
	}, func(ctx context.Context, input *exportPreviewInput) (*exportPreviewOutput, error) {
		if input.Body.Version != ExportWireVersion {
			return nil, problemFromError(invalidExportRequest(
				errors.New("unsupported export preview version"),
			))
		}
		state, canonical, err := DecodeViewQuery(input.Body.Query)
		if err != nil {
			return nil, err
		}
		preview, err := profileService(ctx).PreviewExport(ctx, state)
		if err != nil {
			return nil, problemFromError(err)
		}
		return &exportPreviewOutput{Body: ExportPreviewResponse{
			Version: ExportWireVersion, Revision: strconv.FormatUint(preview.Revision, 10),
			FullCount: preview.FullCount, FilteredCount: preview.FilteredCount,
			ActiveOperations: preview.ActiveOperations, InactiveOperations: preview.InactiveOperations,
			CommitAvailable: preview.CommitAvailable, TemporaryProfile: temporaryProfile(ctx),
			CanonicalQuery: canonical,
		}}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "downloadProfileExport", Method: http.MethodPost,
		Path: server.profilePath("export"), Summary: "Download one committed transaction export",
		Errors: []int{400, 404, 408, 409, 413, 422, 500},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Complete committed transaction export",
				Headers: map[string]*huma.Param{
					"Content-Length":                {Schema: &huma.Schema{Type: "integer", Format: "int64"}},
					"Content-Disposition":           {Schema: &huma.Schema{Type: "string"}},
					"X-Moneyflow-Transaction-Count": {Schema: &huma.Schema{Type: "integer"}},
				},
				Content: map[string]*huma.MediaType{
					"text/csv":                       {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					"application/vnd.sqlite3":        {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					"application/vnd.apache.parquet": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		},
	}, func(ctx context.Context, input *exportInput) (*huma.StreamResponse, error) {
		body := input.Body
		if body.Version != ExportWireVersion || !validExportFormat(body.Format) ||
			(body.Scope != app.ExportScopeFull && body.Scope != app.ExportScopeFiltered) {
			return nil, problemFromError(invalidExportRequest(errors.New("unsupported export request")))
		}
		state, canonical, err := DecodeViewQuery(body.Query)
		if err != nil {
			return nil, err
		}
		if body.Scope == app.ExportScopeFull {
			canonical = ""
		}
		service := profileService(ctx)
		download, err := exporter.PrepareDownload(ctx, exporter.Request{
			ProfileRoot: profileRoot(ctx), Format: body.Format, Scope: body.Scope, Now: time.Now,
			Capture: func(captureContext context.Context, exportedAt time.Time) (app.ExportDocument, error) {
				return service.CaptureExport(captureContext, app.ExportRequest{
					Scope: body.Scope, State: state, CanonicalQuery: canonical,
					ExportedAt: exportedAt, AppVersion: server.version,
				})
			},
		})
		if err != nil {
			return nil, problemFromError(err)
		}
		return &huma.StreamResponse{Body: func(stream huma.Context) {
			defer func() { _ = download.Close() }()
			stream.SetHeader("Content-Type", download.ContentType)
			stream.SetHeader("Content-Length", strconv.FormatInt(download.Size, 10))
			stream.SetHeader("Content-Disposition", exportContentDisposition(download.Filename))
			stream.SetHeader("X-Moneyflow-Transaction-Count", strconv.Itoa(download.Count))
			stream.SetHeader("X-Content-Type-Options", "nosniff")
			if _, copyErr := io.Copy(
				stream.BodyWriter(),
				&exportStreamReader{ctx: stream.Context(), reader: download.Reader},
			); copyErr != nil {
				return
			}
		}}, nil
	})
}

type exportStreamReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *exportStreamReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func validExportFormat(format exporter.Format) bool {
	switch format {
	case exporter.FormatCSV, exporter.FormatSQLite, exporter.FormatParquet:
		return true
	default:
		return false
	}
}

func invalidExportRequest(cause error) *SafeError {
	return newSafeError(CodeExportInvalid, "The export request is invalid.", cause)
}

func exportContentDisposition(filename string) string {
	if filepath.Base(filename) != filename {
		filename = "moneyflow-export"
	}
	return fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.PathEscape(filename),
	)
}
