package amazonimport

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

func TestCoordinatorAcquiresImportLockBeforeDirectoryTraversalAndReusesIt(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	var imported uint64
	coordinator := newTestCoordinator(t, root, now, func(_ context.Context, directory string, _ amazon.Limits) ([]amazon.SourceFile, error) {
		_, err := home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
		assert.ErrorIs(t, err, home.ErrLockBusy)
		return []amazon.SourceFile{{RelativeName: "Retail.OrderHistory.1.csv", Path: filepath.Join(directory, "Retail.OrderHistory.1.csv")}}, nil
	}, func(context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc) (amazon.Candidate, error) {
		return amazon.Candidate{Digest: "candidate", ObservedOrderIDs: []string{"order"}}, nil
	}, func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
		imported++
		return app.AmazonImportResult{Revision: imported, Inserted: 1}, nil
	})

	request := DirectoryRequest{ProfileID: "profile-a", Directory: t.TempDir(), Settings: amazon.Settings{Currency: "USD", Scale: 2}}
	first, err := coordinator.ImportDirectory(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, StateComplete, first.State)
	second, err := coordinator.ImportDirectory(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), second.Result.Revision)
}

func TestCoordinatorReturnsBusyBeforeDirectoryTraversal(t *testing.T) {
	root := t.TempDir()
	lock, err := home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })
	traversed := false
	coordinator := newTestCoordinator(t, root, time.Now(), func(context.Context, string, amazon.Limits) ([]amazon.SourceFile, error) {
		traversed = true
		return nil, errors.New("unexpected traversal")
	}, nil, nil)

	_, err = coordinator.ImportDirectory(context.Background(), DirectoryRequest{ProfileID: "profile-a", Directory: t.TempDir(), Settings: amazon.Settings{Currency: "USD", Scale: 2}})
	assert.Equal(t, CodeImportBusy, CodeOf(err))
	assert.False(t, traversed)
}

func TestCoordinatorAttemptVersionAndCoordinatePrivacy(t *testing.T) {
	root := t.TempDir()
	coordinate := amazon.Coordinate{RelativeFilename: "Retail.OrderHistory.1.csv", Record: 7, Column: "Total Owed", Reason: "invalid_money"}
	coordinator := newTestCoordinator(t, root, time.Now(), nil, func(context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc) (amazon.Candidate, error) {
		return amazon.Candidate{}, &amazon.Error{Code: amazon.CodeInvalid, Coordinate: coordinate}
	}, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2}})
	require.NoError(t, err)
	_, err = coordinator.Stage(context.Background(), StageRequest{ProfileID: "profile-a", AttemptID: started.AttemptID, ExpectedStateVersion: started.StateVersion - 1, Files: []Upload{{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("x")}}})
	assert.Equal(t, CodeAttemptStale, CodeOf(err))

	staged, err := coordinator.Stage(context.Background(), StageRequest{ProfileID: "profile-a", AttemptID: started.AttemptID, ExpectedStateVersion: started.StateVersion, Files: []Upload{{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("x")}}})
	require.NoError(t, err)
	_, err = coordinator.Execute(context.Background(), ExecuteRequest{ProfileID: "profile-a", AttemptID: staged.AttemptID, ExpectedStateVersion: staged.StateVersion})
	assert.Equal(t, CodeImportInvalid, CodeOf(err))
	assert.Equal(t, coordinate, CoordinateOf(err))
	status, statusErr := coordinator.Status(context.Background(), StatusRequest{ProfileID: "profile-a", AttemptID: staged.AttemptID})
	require.NoError(t, statusErr)
	assert.Equal(t, StateFailed, status.State)
	assert.NotContains(t, status.Failure.Detail, "Retail.OrderHistory")
}

func TestCoordinatorStageUsesPrivateFilesAndCleansAfterCancel(t *testing.T) {
	root := t.TempDir()
	coordinator := newTestCoordinator(t, root, time.Now(), nil, nil, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2}})
	require.NoError(t, err)
	staged, err := coordinator.Stage(context.Background(), StageRequest{ProfileID: "profile-a", AttemptID: started.AttemptID, ExpectedStateVersion: started.StateVersion, Files: []Upload{{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n")}}})
	require.NoError(t, err)
	coordinator.mu.Lock()
	stagedPath := coordinator.attempts["profile-a"].files[0].Path
	coordinator.mu.Unlock()
	info, err := os.Stat(stagedPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	canceled, err := coordinator.Cancel(context.Background(), CancelRequest{ProfileID: "profile-a", AttemptID: staged.AttemptID, ExpectedStateVersion: staged.StateVersion})
	require.NoError(t, err)
	assert.Equal(t, StateCanceled, canceled.State)
	_, err = os.Stat(stagedPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestCoordinatorStageRejectsDuplicateNamesWithoutConsumingSecondBody(t *testing.T) {
	root := t.TempDir()
	coordinator := newTestCoordinator(t, root, time.Now(), nil, nil, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2}})
	require.NoError(t, err)
	second := &countingReader{Reader: strings.NewReader("secret")}
	_, err = coordinator.Stage(context.Background(), StageRequest{ProfileID: "profile-a", AttemptID: started.AttemptID, ExpectedStateVersion: started.StateVersion, Files: []Upload{
		{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("one")},
		{RelativeName: "Retail.OrderHistory.1.csv", Reader: second},
	}})
	assert.Equal(t, CodeImportInvalid, CodeOf(err))
	assert.Zero(t, second.read)
}

func TestCoordinatorStageRejectsDuplicateContent(t *testing.T) {
	root := t.TempDir()
	coordinator := newTestCoordinator(t, root, time.Now(), nil, nil, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2}})
	require.NoError(t, err)
	_, err = coordinator.Stage(context.Background(), StageRequest{ProfileID: "profile-a", AttemptID: started.AttemptID, ExpectedStateVersion: started.StateVersion, Files: []Upload{
		{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("same")},
		{RelativeName: "Retail.OrderHistory.2.csv", Reader: strings.NewReader("same")},
	}})
	assert.Equal(t, CodeImportInvalid, CodeOf(err))
}

type countingReader struct {
	io.Reader
	read int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	count, err := reader.Reader.Read(buffer)
	reader.read += count
	return count, err
}

func newTestCoordinator(
	t *testing.T,
	root string,
	now time.Time,
	discover DiscoverFunc,
	parse ParseFunc,
	importProfile ImportFunc,
) *Coordinator {
	t.Helper()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	if discover == nil {
		discover = amazon.DiscoverDirectory
	}
	if parse == nil {
		parse = amazon.Parse
	}
	if importProfile == nil {
		importProfile = func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
			return app.AmazonImportResult{}, nil
		}
	}
	coordinator, err := New(Config{
		InstanceID: "server-a", Now: func() time.Time { return now },
		Random: strings.NewReader(strings.Repeat("a", 256)), Limits: amazon.ProductionLimits,
		ResolveTarget: func(context.Context, string) (Target, error) {
			return Target{ProfileID: "profile-a", Root: canonicalRoot, Import: importProfile}, nil
		},
		Discover: discover, Parse: parse,
	})
	require.NoError(t, err)
	return coordinator
}
