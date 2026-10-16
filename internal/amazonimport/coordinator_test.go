package amazonimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
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
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

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

func TestCoordinatorStageFailureLeavesAttemptRetryableAndCancelable(t *testing.T) {
	root := t.TempDir()
	coordinator := newTestCoordinator(t, root, time.Now(), nil, nil, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)

	_, err = coordinator.Stage(context.Background(), StageRequest{
		ProfileID: started.ProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files: []Upload{{
			RelativeName: "Retail.OrderHistory.1.csv",
			Reader:       iotest.ErrReader(errors.New("upload failed")),
		}},
	})
	assert.Equal(t, CodeImportInvalid, CodeOf(err))
	status, statusErr := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: started.ProfileID, AttemptID: started.AttemptID,
	})
	require.NoError(t, statusErr)
	assert.Equal(t, started.StateVersion, status.StateVersion)

	staged, err := coordinator.Stage(context.Background(), StageRequest{
		ProfileID: started.ProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: status.StateVersion,
		Files: []Upload{{
			RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
		}},
	})
	require.NoError(t, err)
	_, err = coordinator.Cancel(context.Background(), CancelRequest{
		ProfileID: started.ProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: staged.StateVersion,
	})
	require.NoError(t, err)
}

func TestCoordinatorStartReapsAnExpiredIdleAttempt(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closes := 0
	coordinator, err := New(Config{
		InstanceID: "server-a", Now: func() time.Time { return now },
		Random: strings.NewReader(strings.Repeat("a", 256)), Limits: amazon.ProductionLimits,
		ResolveTarget: func(context.Context, string) (Target, error) {
			return Target{ProfileID: "profile-a", Root: root, Close: func() error { closes++; return nil }}, nil
		},
		Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
	})
	require.NoError(t, err)
	_, err = coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	now = now.Add(attemptIdleLimit)

	second, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, second.AttemptID)
	assert.Equal(t, 1, closes)
}

func TestCoordinatorCancelInterruptsParsingBeforeInstall(t *testing.T) {
	root := t.TempDir()
	parsing := make(chan struct{})
	coordinator := newTestCoordinator(t, root, time.Now(), nil, func(
		ctx context.Context, _ []amazon.SourceFile, _ amazon.Settings, _ amazon.Limits, _ amazon.ObserveFunc,
	) (amazon.Candidate, error) {
		close(parsing)
		<-ctx.Done()
		return amazon.Candidate{}, ctx.Err()
	}, func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
		t.Fatal("install must not begin after cancellation")
		return app.AmazonImportResult{}, nil
	})
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	staged, err := coordinator.Stage(context.Background(), StageRequest{
		ProfileID: "profile-a", AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files: []Upload{{
			RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
		}},
	})
	require.NoError(t, err)
	type outcome struct {
		snapshot Snapshot
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		snapshot, executeErr := coordinator.Execute(context.Background(), ExecuteRequest{
			ProfileID: "profile-a", AttemptID: staged.AttemptID,
			ExpectedStateVersion: staged.StateVersion,
		})
		done <- outcome{snapshot: snapshot, err: executeErr}
	}()
	<-parsing
	status, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: "profile-a", AttemptID: staged.AttemptID,
	})
	require.NoError(t, err)
	canceled, err := coordinator.Cancel(context.Background(), CancelRequest{
		ProfileID: "profile-a", AttemptID: staged.AttemptID,
		ExpectedStateVersion: status.StateVersion,
	})
	require.NoError(t, err)
	assert.Equal(t, StateCanceled, canceled.State)
	completed := <-done
	assert.Equal(t, CodeImportCanceled, CodeOf(completed.err))
	assert.Equal(t, StateCanceled, completed.snapshot.State)
}

func TestCoordinatorClientDisconnectAfterInstallBeginsDoesNotAbortFold(t *testing.T) {
	root := t.TempDir()
	installing := make(chan struct{})
	release := make(chan struct{})
	coordinator := newTestCoordinator(t, root, time.Now(), nil, func(
		context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc,
	) (amazon.Candidate, error) {
		return amazon.Candidate{Digest: "candidate", ObservedOrderIDs: []string{"order"}}, nil
	}, func(ctx context.Context, _ app.AmazonImportRequest) (app.AmazonImportResult, error) {
		close(installing)
		<-release
		assert.NoError(t, ctx.Err(), "install must be detached from the client connection")
		return app.AmazonImportResult{Revision: 1, Inserted: 1}, nil
	})
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	staged, err := coordinator.Stage(context.Background(), StageRequest{
		ProfileID: "profile-a", AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files:                []Upload{{RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n")}},
	})
	require.NoError(t, err)
	requestContext, disconnect := context.WithCancel(context.Background())
	type outcome struct {
		snapshot Snapshot
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		snapshot, executeErr := coordinator.Execute(requestContext, ExecuteRequest{
			ProfileID: "profile-a", AttemptID: staged.AttemptID,
			ExpectedStateVersion: staged.StateVersion,
		})
		done <- outcome{snapshot: snapshot, err: executeErr}
	}()
	<-installing
	disconnect()
	close(release)

	completed := <-done
	require.NoError(t, completed.err)
	assert.Equal(t, StateComplete, completed.snapshot.State)
	status, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: "profile-a", AttemptID: staged.AttemptID,
	})
	require.NoError(t, err)
	assert.Equal(t, StateComplete, status.State)
	assert.Equal(t, uint64(1), status.Result.Revision)
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

func TestCoordinatorStageReportsADiskFailureAsStorage(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, managedStageDirectory), nil, 0o600))
	coordinator := newTestCoordinator(t, root, time.Now(), nil, nil, nil)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)

	_, err = coordinator.Stage(context.Background(), StageRequest{
		ProfileID: started.ProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files: []Upload{{
			RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
		}},
	})

	assert.Equal(t, CodeImportStorage, CodeOf(err))
}

func TestCoordinatorExecuteNamesWhyTheInstallFailed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code Code
	}{
		{
			name: "currency differs from the profile",
			err:  fmt.Errorf("plan: %w", app.ErrAmazonSettingsMismatch),
			code: CodeCurrencyMismatch,
		},
		{
			name: "profile changed during the import",
			err:  &app.AppError{Code: app.AppStoreBusy, Detail: "The profile is busy."},
			code: CodeProfileChanged,
		},
		{
			name: "profile changed revision",
			err:  &app.AppError{Code: app.AppRevisionConflict, Detail: "The profile changed."},
			code: CodeProfileChanged,
		},
		{
			name: "profile could not be written",
			err:  &app.AppError{Code: app.AppStoreError, Detail: "The profile could not be saved."},
			code: CodeImportStorage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator := newTestCoordinator(t, t.TempDir(), time.Now(), nil, func(
				context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits,
				amazon.ObserveFunc,
			) (amazon.Candidate, error) {
				return amazon.Candidate{Digest: "candidate", ObservedOrderIDs: []string{"order"}}, nil
			}, func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
				return app.AmazonImportResult{}, test.err
			})
			started, err := coordinator.Start(context.Background(), StartRequest{
				ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
			})
			require.NoError(t, err)
			staged, err := coordinator.Stage(context.Background(), StageRequest{
				ProfileID: "profile-a", AttemptID: started.AttemptID,
				ExpectedStateVersion: started.StateVersion,
				Files: []Upload{{
					RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
				}},
			})
			require.NoError(t, err)

			snapshot, err := coordinator.Execute(context.Background(), ExecuteRequest{
				ProfileID: "profile-a", AttemptID: staged.AttemptID,
				ExpectedStateVersion: staged.StateVersion,
			})

			assert.Equal(t, test.code, CodeOf(err))
			assert.Equal(t, StateFailed, snapshot.State)
			assert.Equal(t, test.code, snapshot.Failure.Code)
		})
	}
}

func TestCoordinatorCloseReleasesIdleAttemptsAndRefusesNewWork(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	closes := 0
	coordinator := closingTestCoordinator(t, root, &closes)
	settings := amazon.Settings{Currency: "USD", Scale: 2}
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: settings,
	})
	require.NoError(t, err)
	staged, err := coordinator.Stage(context.Background(), StageRequest{
		ProfileID: "profile-a", AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files: []Upload{{
			RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
		}},
	})
	require.NoError(t, err)

	coordinator.Close()

	assert.Equal(t, 1, closes, "the attempt's target is closed")
	assert.NoDirExists(t, filepath.Join(root, managedStageDirectory))
	lock, err := home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
	require.NoError(t, err, "the import lock is released")
	require.NoError(t, lock.Release())
	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: "profile-a", AttemptID: staged.AttemptID,
	})
	assert.Equal(t, CodeAttemptInvalid, CodeOf(err), "Close removed the attempt")
	_, err = coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: settings,
	})
	assert.Equal(t, CodeAttemptInvalid, CodeOf(err))
	assert.Equal(t, 2, closes, "a refused start closes its target")
	_, err = coordinator.Execute(context.Background(), ExecuteRequest{
		ProfileID: "profile-a", AttemptID: staged.AttemptID,
		ExpectedStateVersion: staged.StateVersion,
	})
	assert.Equal(t, CodeAttemptInvalid, CodeOf(err))
	_, err = coordinator.ImportDirectory(context.Background(), DirectoryRequest{
		ProfileID: "profile-a", Directory: t.TempDir(), Settings: settings,
	})
	assert.Equal(t, CodeAttemptInvalid, CodeOf(err))
	assert.Equal(t, 2, closes, "a refused directory import opens no target")
}

func TestCoordinatorCloseLetsARunningStageCleanUpWhenItStops(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	closes := 0
	coordinator := closingTestCoordinator(t, root, &closes)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	upload := &gatedReader{
		reading: make(chan struct{}), release: make(chan struct{}),
		body: strings.NewReader("header\n"),
	}
	staged := make(chan error, 1)
	go func() {
		_, stageErr := coordinator.Stage(context.Background(), StageRequest{
			ProfileID: "profile-a", AttemptID: started.AttemptID,
			ExpectedStateVersion: started.StateVersion,
			Files:                []Upload{{RelativeName: "Retail.OrderHistory.1.csv", Reader: upload}},
		})
		staged <- stageErr
	}()
	select {
	case <-upload.reading:
	case stageErr := <-staged:
		t.Fatalf("Stage returned before it read the upload: %v (%v)", stageErr, errors.Unwrap(stageErr))
	}

	coordinator.Close()
	_, err = home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
	require.ErrorIs(t, err, home.ErrLockBusy, "Close leaves a running attempt alone")
	close(upload.release)

	assert.Equal(t, CodeAttemptInvalid, CodeOf(<-staged))
	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: "profile-a", AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeAttemptInvalid, CodeOf(err), "the stopped attempt is removed")
	assert.Equal(t, 1, closes, "the attempt's target is closed")
	assert.NoDirExists(t, filepath.Join(root, managedStageDirectory))
	lock, err := home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
	require.NoError(t, err, "the import lock is released")
	require.NoError(t, lock.Release())
}

func TestCoordinatorExecutePanicStopsTheAttemptSoCloseReleasesIt(t *testing.T) {
	tests := []struct {
		name    string
		parse   ParseFunc
		install ImportFunc
	}{
		{
			name: "parse",
			parse: func(
				context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc,
			) (amazon.Candidate, error) {
				panic("synthetic parse failure")
			},
		},
		{
			name: "install",
			parse: func(
				context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc,
			) (amazon.Candidate, error) {
				return amazon.Candidate{Digest: "candidate", ObservedOrderIDs: []string{"order"}}, nil
			},
			install: func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error) {
				panic("synthetic install failure")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			closes := 0
			coordinator, err := New(Config{
				InstanceID: "server-a", Now: time.Now,
				Random: strings.NewReader(strings.Repeat("a", 256)), Limits: amazon.ProductionLimits,
				ResolveTarget: func(context.Context, string) (Target, error) {
					return Target{
						ProfileID: "profile-a", Root: root, Import: test.install,
						Close: func() error { closes++; return nil },
					}, nil
				},
				Discover: amazon.DiscoverDirectory, Parse: test.parse,
			})
			require.NoError(t, err)
			started, err := coordinator.Start(context.Background(), StartRequest{
				ProfileID: "profile-a", Settings: amazon.Settings{Currency: "USD", Scale: 2},
			})
			require.NoError(t, err)
			staged, err := coordinator.Stage(context.Background(), StageRequest{
				ProfileID: "profile-a", AttemptID: started.AttemptID,
				ExpectedStateVersion: started.StateVersion,
				Files: []Upload{{
					RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
				}},
			})
			require.NoError(t, err)

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _ = coordinator.Execute(context.Background(), ExecuteRequest{
					ProfileID: "profile-a", AttemptID: staged.AttemptID,
					ExpectedStateVersion: staged.StateVersion,
				})
			}()

			assert.Equal(t, "synthetic "+test.name+" failure", recovered, "the panic reaches the caller")
			status, err := coordinator.Status(context.Background(), StatusRequest{
				ProfileID: "profile-a", AttemptID: staged.AttemptID,
			})
			require.NoError(t, err)
			assert.Equal(t, StateFailed, status.State, "the attempt is no longer running")
			assert.NotContains(t, status.Failure.Detail, "synthetic")
			coordinator.Close()
			assert.Equal(t, 1, closes, "the attempt's target is closed")
			lock, err := home.TryLockExisting(root, home.LockAmazonImport, home.LockExclusive)
			require.NoError(t, err, "the import lock is released")
			require.NoError(t, lock.Release())
		})
	}
}

// closingTestCoordinator returns a coordinator whose targets count their closes in closes.
func closingTestCoordinator(t *testing.T, root string, closes *int) *Coordinator {
	t.Helper()
	coordinator, err := New(Config{
		InstanceID: "server-a", Now: time.Now,
		Random: strings.NewReader(strings.Repeat("a", 256)), Limits: amazon.ProductionLimits,
		ResolveTarget: func(context.Context, string) (Target, error) {
			return Target{
				ProfileID: "profile-a", Root: root, Close: func() error { *closes++; return nil },
			}, nil
		},
		Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
	})
	require.NoError(t, err)
	return coordinator
}

// gatedReader closes reading on its first read, then waits for release before reading body.
type gatedReader struct {
	reading chan struct{}
	release chan struct{}
	once    sync.Once
	body    io.Reader
}

func (reader *gatedReader) Read(buffer []byte) (int, error) {
	reader.once.Do(func() {
		close(reader.reading)
		<-reader.release
	})
	return reader.body.Read(buffer)
}
