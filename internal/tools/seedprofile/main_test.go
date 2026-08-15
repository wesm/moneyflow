package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestRunSeedsOnlyOneExplicitEmptyProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, run(ctx, []string{root}))
	assert.Error(t, run(ctx, []string{root}), "seeding must refuse to overwrite a profile")

	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, profile.Close()) }()
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), snapshot.Revision)
	assert.NotEmpty(t, snapshot.Committed.Transactions)
}

func TestRunRequiresExactExplicitRoot(t *testing.T) {
	t.Parallel()
	assert.Error(t, run(context.Background(), nil))
	assert.Error(t, run(context.Background(), []string{"", "extra"}))
}
