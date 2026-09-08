package mcp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/exporter"
	"github.com/wesm/moneyflow/internal/version"
)

// ExportInput selects a file encoding, never a caller-controlled output path.
type ExportInput struct {
	Format exporter.Format `json:"format,omitempty" jsonschema:"parquet (default), csv, or sqlite"`
}

// ExportPreviewDocument describes the committed full-profile export, not the effective view.
type ExportPreviewDocument struct {
	Header
	Scope                     string `json:"scope"`
	TransactionCount          int    `json:"transaction_count"`
	ExcludedPendingOperations int    `json:"excluded_pending_operations"`
	InactiveRedoOperations    int    `json:"inactive_redo_operations"`
}

// ExportResultDocument identifies a file on the server, not a client download.
type ExportResultDocument struct {
	ExportPreviewDocument
	Location  string          `json:"location"`
	Path      string          `json:"path"`
	Filename  string          `json:"filename"`
	Format    exporter.Format `json:"format"`
	SizeBytes string          `json:"size_bytes"`
}

func registerExportTools(server *Server, dependencies Dependencies) {
	registerTool(server, "preview_export", "Preview full committed-profile export counts. Pending edits are excluded. Does not create a file or contact providers.", true,
		func(ctx context.Context, _ EmptyInput) (any, error) {
			preview, err := dependencies.Service.PreviewExport(ctx, app.DefaultViewState())
			if err != nil {
				return nil, err
			}
			return ExportPreviewDocument{
				Header: NewHeader(StatusOK, preview.Revision), Scope: string(app.ExportScopeFull),
				TransactionCount: preview.FullCount, ExcludedPendingOperations: preview.ActiveOperations,
				InactiveRedoOperations: preview.InactiveOperations,
			}, nil
		})
	// Export creates a file, so its protocol hint is not read-only even though
	// it is available without permission to edit financial state.
	registerTool(server, "export_transactions", "Export all committed transactions as parquet (default), csv, or sqlite to the profile's server-side exports directory. Returns a server path, not a download. Excludes pending edits; does not commit or contact providers. Retrying creates another file.", false,
		func(ctx context.Context, input ExportInput) (any, error) {
			return exportTransactionsDocument(ctx, dependencies, input)
		})
}

func exportTransactionsDocument(ctx context.Context, dependencies Dependencies, input ExportInput) (any, error) {
	format := input.Format
	if format == "" {
		format = exporter.FormatParquet
	}
	var metadata app.ExportMetadata
	result, err := exporter.WriteFile(ctx, exporter.Request{
		ProfileRoot: dependencies.ProfileRoot, Format: format, Scope: app.ExportScopeFull, Now: dependencies.Clock,
		Capture: func(captureContext context.Context, exportedAt time.Time) (app.ExportDocument, error) {
			document, captureErr := dependencies.Service.CaptureExport(captureContext, app.ExportRequest{
				Scope: app.ExportScopeFull, State: app.DefaultViewState(), ExportedAt: exportedAt, AppVersion: version.Version,
			})
			metadata = document.Metadata
			return document, captureErr
		},
	})
	if err != nil {
		if failure, ok := errors.AsType[*exporter.Error](err); ok {
			return ErrorDocument{Header: NewHeader(StatusError, dependencies.Service.Revision()), Code: string(failure.Code), Detail: failure.Detail}, nil
		}
		return nil, err
	}
	return ExportResultDocument{
		ExportPreviewDocument: ExportPreviewDocument{
			Header: NewHeader(StatusOK, metadata.ProfileRevision), Scope: string(metadata.Scope),
			TransactionCount: result.Count, ExcludedPendingOperations: metadata.ExcludedActiveOperations,
			InactiveRedoOperations: metadata.InactiveRedoOperations,
		},
		Location: "server", Path: result.Path, Filename: result.Filename, Format: format, SizeBytes: strconv.FormatInt(result.Size, 10),
	}, nil
}
