package sqlite

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

func TestRevisionCASAcrossHandlesAllowsExactlyOneAppend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	_, err = first.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for index, handle := range []*profile{first, second} {
		waitGroup.Add(1)
		go func(index int, handle *profile) {
			defer waitGroup.Done()
			<-start
			_, appendErr := handle.Append(ctx, 1, draftHideOperation(
				[]string{"operation_first", "operation_second"}[index],
				1,
				"transaction_a",
			))
			results <- appendErr
		}(index, handle)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var successes, conflicts int
	for appendErr := range results {
		if appendErr == nil {
			successes++
			continue
		}
		var failure *store.Error
		require.ErrorAs(t, appendErr, &failure)
		if failure.Code == store.CodeRevisionConflict {
			conflicts++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
	loaded, err := first.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), loaded.Revision)
	assert.Len(t, loaded.Journal, 1)
}

func TestStoreBusyDoesNotApplyDelayedMutation(t *testing.T) {
	t.Parallel()

	options := DefaultOptions
	options.MutationBusyTimeout = 20 * time.Millisecond
	ctx := context.Background()
	profile := openSeededProfile(t, options)
	connection, err := profile.database.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	_, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") })

	_, err = profile.Append(ctx, 1, draftHideOperation(
		"operation_blocked",
		1,
		"transaction_a",
	))
	assertStoreCode(t, err, store.CodeStoreBusy)
	_, err = connection.ExecContext(ctx, "ROLLBACK")
	require.NoError(t, err)

	assertJournalState(t, profile, 1, 0, 0)
}

func TestRevisionCASAcrossProcessesAllowsExactlyOneAppend(t *testing.T) {
	ctx := context.Background()
	paths := temporaryPaths(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	seeded := profileStore.(*profile)
	_, err = seeded.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	require.NoError(t, seeded.Close())

	type processResult struct {
		output string
		err    error
	}
	results := make(chan processResult, 2)
	commands := []*exec.Cmd{
		revisionHelperCommand(ctx, paths.Root, "operation_process_first"),
		revisionHelperCommand(ctx, paths.Root, "operation_process_second"),
	}
	for _, command := range commands {
		go func(command *exec.Cmd) {
			output, runErr := command.Output()
			results <- processResult{output: string(output), err: runErr}
		}(command)
	}
	var successes, conflicts int
	for range commands {
		result := <-results
		require.NoError(t, result.err)
		switch {
		case strings.Contains(result.output, "RESULT success"):
			successes++
		case strings.Contains(result.output, "RESULT revision_conflict"):
			conflicts++
		default:
			t.Fatalf("helper returned no recognized result: %s", result.output)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)

	reopenedStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	reopened := reopenedStore.(*profile)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	loaded, err := reopened.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), loaded.Revision)
	assert.Len(t, loaded.Journal, 1)
}

func TestRevisionCASProcessHelper(t *testing.T) {
	if os.Getenv("MONEYFLOW_SQLITE_REVISION_HELPER") != "1" {
		return
	}
	paths, err := home.ResolveRoot(os.Getenv("MONEYFLOW_SQLITE_REVISION_ROOT"), nil, "")
	require.NoError(t, err)
	profileStore, err := Open(context.Background(), paths, DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileStore.Close()) })
	_, err = profileStore.Append(
		context.Background(),
		1,
		draftHideOperation(os.Getenv("MONEYFLOW_SQLITE_REVISION_OPERATION"), 1, "transaction_a"),
	)
	if err == nil {
		_, _ = fmt.Fprintln(os.Stdout, "RESULT success")
		return
	}
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, store.CodeRevisionConflict, failure.Code)
	_, _ = fmt.Fprintln(os.Stdout, "RESULT revision_conflict")
}

func revisionHelperCommand(ctx context.Context, root, operationID string) *exec.Cmd {
	// Re-execute this exact test binary with fixed arguments; only environment data reaches the helper.
	//nolint:gosec
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRevisionCASProcessHelper$")
	command.Env = append(
		os.Environ(),
		"MONEYFLOW_SQLITE_REVISION_HELPER=1",
		"MONEYFLOW_SQLITE_REVISION_ROOT="+root,
		"MONEYFLOW_SQLITE_REVISION_OPERATION="+operationID,
	)
	return command
}

func assertJournalState(
	t *testing.T,
	profile *profile,
	revision uint64,
	cursor, count int,
) {
	t.Helper()
	var actualRevision uint64
	var actualCursor, actualCount int
	require.NoError(t, profile.database.QueryRowContext(context.Background(), `
		SELECT revision, journal_cursor,
			(SELECT count(*) FROM journal_operations)
		FROM profile_state WHERE singleton = 1`).Scan(
		&actualRevision,
		&actualCursor,
		&actualCount,
	))
	assert.Equal(t, revision, actualRevision)
	assert.Equal(t, cursor, actualCursor)
	assert.Equal(t, count, actualCount)
}
