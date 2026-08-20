// Package exporter writes renderer-neutral committed export documents.
package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
)

const (
	staleStageAge                 = 24 * time.Hour
	maximumExportFilenameAttempts = 1000
	cleanupAttempts               = 4
	cleanupRetryDelay             = 10 * time.Millisecond
)

// Format selects a lossless file encoding.
type Format string

const (
	// FormatCSV writes Python-compatible guarded comma-separated values.
	FormatCSV Format = "csv"
	// FormatSQLite writes a standalone lossless SQLite database.
	FormatSQLite Format = "sqlite"
	// FormatParquet writes a typed Snappy-compressed Parquet file.
	FormatParquet Format = "parquet"
)

// CaptureFunc captures one committed frame at the supplied execution time.
type CaptureFunc func(context.Context, time.Time) (app.ExportDocument, error)

// Request supplies renderer intent and the already-open profile root.
type Request struct {
	ProfileRoot string
	Format      Format
	Scope       app.ExportScope
	Now         func() time.Time
	Capture     CaptureFunc
}

// Result identifies one persistent export without exposing it to logs automatically.
type Result struct {
	Path     string
	Filename string
	Size     int64
	Count    int
}

// Download owns a private temporary export and its cross-process lock.
type Download struct {
	Reader      io.ReadSeeker
	Filename    string
	ContentType string
	Size        int64
	Count       int

	file      *os.File
	stagePath string
	lock      *home.Lock
	closeOnce sync.Once
	closeErr  error
}

// ErrorCode is a stable filesystem/export failure classification.
type ErrorCode string

const (
	// CodeInvalid reports an invalid export request.
	CodeInvalid ErrorCode = "export_invalid"
	// CodeBusy reports cross-process export-lock contention.
	CodeBusy ErrorCode = "export_busy"
	// CodeCancelled reports cancellation observed by the executing process.
	CodeCancelled ErrorCode = "export_cancelled"
	// CodeFailed reports a filesystem or encoding failure.
	CodeFailed ErrorCode = "export_failed"
)

var errorDetails = map[ErrorCode]string{
	CodeInvalid:   "The export request is invalid.",
	CodeBusy:      "Another process is exporting this profile.",
	CodeCancelled: "The export was cancelled.",
	CodeFailed:    "The export could not be created.",
}

// Error carries only allowlisted renderer-safe detail.
type Error struct {
	Code   ErrorCode
	Detail string
	cause  error
}

func (failure *Error) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return failure.Detail
}

func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// WriteFile atomically publishes a persistent export beneath the profile root.
func WriteFile(ctx context.Context, request Request) (Result, error) {
	prepared, err := prepare(ctx, request, true)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = prepared.lock.Release() }()
	defer func() { _ = os.Remove(prepared.stagePath) }()
	if err = ctx.Err(); err != nil {
		return Result{}, exportError(CodeCancelled, err)
	}
	if err = home.PublishPrivateNoReplace(prepared.stagePath, prepared.finalPath); err != nil {
		return Result{}, exportError(CodeFailed, err)
	}
	info, err := os.Stat(prepared.finalPath)
	if err != nil {
		return Result{}, exportError(CodeFailed, err)
	}
	return Result{
		Path: prepared.finalPath, Filename: prepared.filename, Size: info.Size(), Count: prepared.count,
	}, nil
}

// PrepareDownload creates a private seekable export and retains export.lock until Close.
func PrepareDownload(ctx context.Context, request Request) (*Download, error) {
	prepared, err := prepare(ctx, request, false)
	if err != nil {
		return nil, err
	}
	file, err := home.OpenPrivateFile(prepared.stagePath)
	if err != nil {
		_ = os.Remove(prepared.stagePath)
		_ = prepared.lock.Release()
		return nil, exportError(CodeFailed, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(prepared.stagePath)
		_ = prepared.lock.Release()
		return nil, exportError(CodeFailed, err)
	}
	return &Download{
		Reader: file, Filename: prepared.filename, ContentType: contentType(request.Format),
		Size: info.Size(), Count: prepared.count, file: file, stagePath: prepared.stagePath, lock: prepared.lock,
	}, nil
}

// Close removes the private stage before releasing the cross-process lock.
func (download *Download) Close() error {
	if download == nil {
		return nil
	}
	download.closeOnce.Do(func() {
		closeErr := download.file.Close()
		removeErr := removeWithRetry(download.stagePath)
		releaseErr := download.lock.Release()
		download.closeErr = errors.Join(closeErr, removeErr, releaseErr)
	})
	return download.closeErr
}

type preparedExport struct {
	lock      *home.Lock
	stagePath string
	finalPath string
	filename  string
	count     int
}

func prepare(ctx context.Context, request Request, persistent bool) (preparedExport, error) {
	if err := validateRequest(request); err != nil {
		return preparedExport{}, exportError(CodeInvalid, err)
	}
	lock, err := home.TryLockExisting(request.ProfileRoot, home.LockExport, home.LockExclusive)
	if errors.Is(err, home.ErrLockBusy) {
		return preparedExport{}, exportError(CodeBusy, err)
	}
	if err != nil {
		return preparedExport{}, exportError(CodeFailed, err)
	}
	failed := true
	var stagePath string
	defer func() {
		if failed {
			if stagePath != "" {
				_ = os.Remove(stagePath)
			}
			_ = lock.Release()
		}
	}()
	now := request.Now().UTC().Truncate(time.Millisecond)
	if err = ctx.Err(); err != nil {
		return preparedExport{}, exportError(CodeCancelled, err)
	}
	exportsDir, stageDir, err := home.EnsureExportDirectories(request.ProfileRoot)
	if err != nil {
		return preparedExport{}, exportError(CodeFailed, err)
	}
	if err = home.RemoveManagedExportStages(stageDir, now.Add(-staleStageAge)); err != nil {
		return preparedExport{}, exportError(CodeFailed, err)
	}
	document, err := request.Capture(ctx, now)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return preparedExport{}, exportError(CodeCancelled, ctxErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return preparedExport{}, exportError(CodeCancelled, err)
		}
		var applicationError *app.AppError
		if errors.As(err, &applicationError) {
			return preparedExport{}, err
		}
		return preparedExport{}, exportError(CodeFailed, err)
	}
	if document.Metadata.Scope != request.Scope || len(document.Rows) == 0 {
		return preparedExport{}, exportError(CodeInvalid, errors.New("captured document does not match request"))
	}
	filename, finalPath, err := allocateFilename(exportsDir, request, now, persistent)
	if err != nil {
		return preparedExport{}, err
	}
	stage, createdPath, err := home.CreatePrivateStage(
		stageDir, home.ManagedExportStagePrefix+string(request.Format)+"-",
	)
	if err != nil {
		return preparedExport{}, exportError(CodeFailed, err)
	}
	stagePath = createdPath
	if err = encodeStage(ctx, request.Format, stage, stagePath, document); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return preparedExport{}, exportError(CodeCancelled, err)
		}
		return preparedExport{}, exportError(CodeFailed, err)
	}
	failed = false
	return preparedExport{
		lock: lock, stagePath: stagePath, finalPath: finalPath, filename: filename,
		count: len(document.Rows),
	}, nil
}

func validateRequest(request Request) error {
	if request.ProfileRoot == "" || !filepath.IsAbs(request.ProfileRoot) {
		return errors.New("profile root is invalid")
	}
	if request.Capture == nil || request.Now == nil {
		return errors.New("export dependency is missing")
	}
	if request.Scope != app.ExportScopeFull && request.Scope != app.ExportScopeFiltered {
		return errors.New("export scope is invalid")
	}
	switch request.Format {
	case FormatCSV, FormatSQLite, FormatParquet:
		return nil
	default:
		return errors.New("export format is invalid")
	}
}

func allocateFilename(
	exportsDir string,
	request Request,
	now time.Time,
	persistent bool,
) (string, string, error) {
	base := now.Format("2006-01-02_150405") +
		fmt.Sprintf("_%06d", now.Nanosecond()/int(time.Microsecond)) +
		"-" + string(request.Scope) + "-export"
	extension := extension(request.Format)
	for attempt := 1; attempt <= maximumExportFilenameAttempts; attempt++ {
		suffix := ""
		if attempt > 1 {
			suffix = fmt.Sprintf("-%d", attempt)
		}
		filename := base + suffix + extension
		if !safeFilename(filename) {
			return "", "", exportError(CodeFailed, errors.New("generated export filename is unsafe"))
		}
		path := filepath.Join(exportsDir, filename)
		if !persistent {
			return filename, "", nil
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return filename, path, nil
		} else if err != nil {
			return "", "", exportError(CodeFailed, err)
		}
	}
	return "", "", exportError(CodeFailed, errors.New("export filename space is exhausted"))
}

func encodeStage(
	ctx context.Context,
	format Format,
	stage *os.File,
	stagePath string,
	document app.ExportDocument,
) error {
	switch format {
	case FormatCSV:
		if err := writeCSVContext(ctx, stage, document); err != nil {
			_ = stage.Close()
			return err
		}
		if err := stage.Sync(); err != nil {
			_ = stage.Close()
			return err
		}
		return stage.Close()
	case FormatSQLite:
		if err := stage.Close(); err != nil {
			return err
		}
		if err := writeSQLite(ctx, stagePath, document); err != nil {
			return err
		}
		file, err := os.OpenFile(stagePath, os.O_RDWR, 0) //nolint:gosec // fixed private stage path.
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		return file.Sync()
	case FormatParquet:
		if err := writeParquetContext(ctx, stage, document); err != nil {
			_ = stage.Close()
			return err
		}
		if err := stage.Sync(); err != nil {
			_ = stage.Close()
			return err
		}
		return stage.Close()
	default:
		_ = stage.Close()
		return errors.New("export format is invalid")
	}
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (writer contextWriter) Write(data []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		return 0, err
	}
	return writer.writer.Write(data)
}

func extension(format Format) string {
	switch format {
	case FormatCSV:
		return ".csv"
	case FormatSQLite:
		return ".db"
	case FormatParquet:
		return ".parquet"
	default:
		return ""
	}
}

func contentType(format Format) string {
	switch format {
	case FormatCSV:
		return "text/csv; charset=utf-8"
	case FormatSQLite:
		return "application/vnd.sqlite3"
	case FormatParquet:
		return "application/vnd.apache.parquet"
	default:
		return "application/octet-stream"
	}
}

func removeWithRetry(path string) error {
	var err error
	for attempt := 0; attempt < cleanupAttempts; attempt++ {
		err = os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if attempt+1 < cleanupAttempts {
			time.Sleep(cleanupRetryDelay)
		}
	}
	return fmt.Errorf("remove export stage: %w", err)
}

func exportError(code ErrorCode, cause error) *Error {
	return &Error{Code: code, Detail: errorDetails[code], cause: cause}
}

func safeFilename(filename string) bool {
	return filename != "" && filepath.Base(filename) == filename &&
		!strings.ContainsAny(filename, "\r\n\x00")
}
