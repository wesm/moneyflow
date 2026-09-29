package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/exporter"
	"github.com/wesm/moneyflow/internal/version"
)

// ExportInput selects a file encoding, never a caller-controlled output path.
type ExportInput struct {
	ExportSelectionInput
	Format exporter.Format `json:"format,omitempty" jsonschema:"parquet (default), csv, or sqlite"`
}

// ExportSelectionInput selects the full profile or an unpaginated committed subset.
type ExportSelectionInput struct {
	Scope  app.ExportScope         `json:"scope,omitempty" jsonschema:"full (default) or filtered"`
	Filter *TransactionFilterInput `json:"filter,omitempty" jsonschema:"required for filtered scope; same predicates as get_transactions without pagination"`
}

// ExportPreviewDocument describes the committed export, not the effective view.
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
	registerTool(server, "preview_export", "Preview full (default) or filtered committed export counts. Filters match get_transactions without pagination, applied to committed values. Pending edits are excluded. Does not create a file or contact providers.", true,
		func(ctx context.Context, input ExportSelectionInput) (any, error) {
			request, err := exportSelectionRequest(input)
			if err != nil {
				return nil, newAppInputError(dependencies.Service.Revision(), err)
			}
			var preview app.ExportPreview
			if request.TransactionFilter != nil {
				preview, err = dependencies.Service.PreviewTransactionExport(ctx, *request.TransactionFilter)
			} else {
				preview, err = dependencies.Service.PreviewExport(ctx, request.State)
			}
			if err != nil {
				return nil, err
			}
			count := preview.FullCount
			if request.Scope == app.ExportScopeFiltered {
				count = preview.FilteredCount
			}
			return ExportPreviewDocument{
				Header: NewHeader(StatusOK, preview.Revision), Scope: string(request.Scope),
				TransactionCount: count, ExcludedPendingOperations: preview.ActiveOperations,
				InactiveRedoOperations: preview.InactiveOperations,
			}, nil
		})
	// Export creates a file, so its protocol hint is not read-only even though
	// it is available without permission to edit financial state.
	registerTool(server, "export_transactions", "Export full (default) or filtered committed transactions as parquet (default), csv, or sqlite to the profile's server-side exports directory. Filters match get_transactions without pagination, applied to committed values. Returns a server path, not a download. Excludes pending edits; does not commit or contact providers. Retrying creates another file.", false,
		func(ctx context.Context, input ExportInput) (any, error) {
			return exportTransactionsDocument(ctx, dependencies, input)
		})
}

func exportTransactionsDocument(ctx context.Context, dependencies Dependencies, input ExportInput) (any, error) {
	request, err := exportSelectionRequest(input.ExportSelectionInput)
	if err != nil {
		return nil, newAppInputError(dependencies.Service.Revision(), err)
	}
	format := input.Format
	if format == "" {
		format = exporter.FormatParquet
	}
	var metadata app.ExportMetadata
	result, err := exporter.WriteFile(ctx, exporter.Request{
		ProfileRoot: dependencies.ProfileRoot, Format: format, Scope: request.Scope, Now: dependencies.Clock,
		Capture: func(captureContext context.Context, exportedAt time.Time) (app.ExportDocument, error) {
			request.ExportedAt, request.AppVersion = exportedAt, version.Version
			document, captureErr := dependencies.Service.CaptureExport(captureContext, request)
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

func exportSelectionRequest(input ExportSelectionInput) (app.ExportRequest, error) {
	scope := input.Scope
	if scope == "" {
		scope = app.ExportScopeFull
	}
	request := app.ExportRequest{Scope: scope, State: app.DefaultViewState()}
	switch scope {
	case app.ExportScopeFull:
		if input.Filter != nil {
			return request, errors.New("export filters require filtered scope")
		}
	case app.ExportScopeFiltered:
		if input.Filter == nil {
			return request, errors.New("filtered export requires a filter object")
		}
		filter, err := transactionFilter(*input.Filter)
		if err != nil {
			return request, err
		}
		request.TransactionFilter = &filter
		// Tag this deterministic JSON separately from the analytical URL codec.
		// It records the requested predicates, never a caller-supplied query string.
		recorded := *input.Filter
		recorded.IncludeHidden = new(filter.IncludeHidden)
		encoded, err := json.Marshal(struct {
			Kind   string                 `json:"kind"`
			Filter TransactionFilterInput `json:"filter"`
		}{Kind: "mcp_transactions_v1", Filter: recorded})
		if err != nil {
			return request, err
		}
		request.CanonicalQuery = string(encoded)
	default:
		return request, errors.New("export scope is invalid")
	}
	return request, nil
}
