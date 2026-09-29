package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
)

func TestWriteFilePublishesPrivateCSVAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 19, 14, 15, 16, 123_000_000, time.UTC)
	captured := false
	request := Request{
		ProfileRoot: root, Format: FormatCSV, Scope: app.ExportScopeFull, Now: func() time.Time { return now },
		Capture: func(_ context.Context, capturedAt time.Time) (app.ExportDocument, error) {
			captured = true
			assert.Equal(t, now, capturedAt)
			_, lockErr := home.TryLockExisting(root, home.LockExport, home.LockExclusive)
			assert.ErrorIs(t, lockErr, home.ErrLockBusy)
			return testDocument(t), nil
		},
	}

	result, err := WriteFile(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, captured)
	assert.Equal(t, 2, result.Count)
	assert.Equal(t, "2026-08-19_141516_123000-full-export.csv", result.Filename)
	assert.Equal(t, filepath.Join(result.Path), result.Path)
	assert.Positive(t, result.Size)
	assert.FileExists(t, result.Path)
	info, err := os.Stat(result.Path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	opened, err := home.OpenPrivateFile(result.Path)
	require.NoError(t, err)
	require.NoError(t, opened.Close())

	lock, err := home.TryLockExisting(root, home.LockExport, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestWriteFileUsesCounterSuffixWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	request := testRequest(t, root, FormatCSV)
	first, err := WriteFile(context.Background(), request)
	require.NoError(t, err)
	firstContents, err := os.ReadFile(first.Path) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	second, err := WriteFile(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-19_141516_123000-full-export-2.csv", second.Filename)
	after, err := os.ReadFile(first.Path) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, firstContents, after)
}

func TestWriteFileRefusesExhaustedFilenameSpace(t *testing.T) {
	root := t.TempDir()
	exportsDir, _, err := home.EnsureExportDirectories(root)
	require.NoError(t, err)
	base := "2026-08-19_141516_123000-full-export"
	for attempt := 1; attempt <= maximumExportFilenameAttempts; attempt++ {
		suffix := ""
		if attempt > 1 {
			suffix = fmt.Sprintf("-%d", attempt)
		}
		require.NoError(t, os.WriteFile(
			filepath.Join(exportsDir, base+suffix+".csv"), []byte("existing"), 0o600,
		))
	}
	_, err = WriteFile(context.Background(), testRequest(t, root, FormatCSV))
	assertExportCode(t, err, CodeFailed)
	contents, readErr := os.ReadFile(filepath.Join(exportsDir, base+".csv")) //nolint:gosec
	require.NoError(t, readErr)
	assert.Equal(t, "existing", string(contents))
}

func TestWriteFileBusyAndFailureLeaveNoVisibleExport(t *testing.T) {
	root := t.TempDir()
	lock, err := home.TryLock(root, home.LockExport, home.LockExclusive)
	require.NoError(t, err)
	_, err = WriteFile(context.Background(), testRequest(t, root, FormatCSV))
	assertExportCode(t, err, CodeBusy)
	require.NoError(t, lock.Release())

	request := testRequest(t, root, FormatCSV)
	request.Capture = func(context.Context, time.Time) (app.ExportDocument, error) {
		return app.ExportDocument{}, errors.New("private row data must not escape")
	}
	_, err = WriteFile(context.Background(), request)
	assertExportCode(t, err, CodeFailed)
	assert.NotContains(t, err.Error(), "private row")
	assert.NoFileExists(t, filepath.Join(root, "exports", "2026-08-19_141516_123000-full-export.csv"))
}

func TestWriteFileSQLiteEncodeFailureLeavesNoVisibleOrStagedFile(t *testing.T) {
	root := t.TempDir()
	request := testRequest(t, root, FormatSQLite)
	request.Capture = func(_ context.Context, capturedAt time.Time) (app.ExportDocument, error) {
		document := testDocument(t)
		document.Metadata.ExportedAt = capturedAt
		document.Rows[0].Currency = "usd"
		return document, nil
	}
	_, err := WriteFile(context.Background(), request)
	assertExportCode(t, err, CodeFailed)
	assert.NoFileExists(t, filepath.Join(root, "exports", "2026-08-19_141516_123000-full-export.db"))
	_, stageDir, err := home.EnsureExportDirectories(root)
	require.NoError(t, err)
	entries, err := os.ReadDir(stageDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestPrepareDownloadRetainsLockUntilCloseAndRemovesStage(t *testing.T) {
	root := t.TempDir()
	download, err := PrepareDownload(context.Background(), testRequest(t, root, FormatCSV))
	require.NoError(t, err)
	assert.Equal(t, "2026-08-19_141516_123000-full-export.csv", download.Filename)
	assert.Equal(t, "text/csv; charset=utf-8", download.ContentType)
	assert.Positive(t, download.Size)
	assert.Equal(t, 2, download.Count)
	contents, err := io.ReadAll(download.Reader)
	require.NoError(t, err)
	assert.NotEmpty(t, contents)
	_, err = home.TryLockExisting(root, home.LockExport, home.LockExclusive)
	assert.ErrorIs(t, err, home.ErrLockBusy)
	stagePath := download.stagePath
	assert.FileExists(t, stagePath)

	require.NoError(t, download.Close())
	assert.NoFileExists(t, stagePath)
	require.NoError(t, download.Close())
	lock, err := home.TryLockExisting(root, home.LockExport, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestPrepareDownloadCancellationIsTypedAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareDownload(ctx, testRequest(t, root, FormatCSV))
	assertExportCode(t, err, CodeCancelled)
	lock, lockErr := home.TryLockExisting(root, home.LockExport, home.LockExclusive)
	require.NoError(t, lockErr)
	require.NoError(t, lock.Release())
}

func TestWriteFileCancellationAfterCaptureDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	request := testRequest(t, root, FormatParquet)
	request.Capture = func(_ context.Context, capturedAt time.Time) (app.ExportDocument, error) {
		document := testDocument(t)
		document.Metadata.ExportedAt = capturedAt
		cancel()
		return document, nil
	}

	_, err := WriteFile(ctx, request)
	assertExportCode(t, err, CodeCancelled)
	assert.NoFileExists(t, filepath.Join(root, "exports", "2026-08-19_141516_123000-full-export.parquet"))
}

func testRequest(t *testing.T, root string, format Format) Request {
	t.Helper()
	now := time.Date(2026, 8, 19, 14, 15, 16, 123_000_000, time.UTC)
	return Request{
		ProfileRoot: root, Format: format, Scope: app.ExportScopeFull,
		Now: func() time.Time { return now },
		Capture: func(_ context.Context, capturedAt time.Time) (app.ExportDocument, error) {
			document := testDocument(t)
			document.Metadata.ExportedAt = capturedAt
			return document, nil
		},
	}
}

func assertExportCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var failure *Error
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, code, failure.Code)
}
