package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestMutationUndoAndRedoReturnTheOperationSummary(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	mutated := appendMerchantMutation(t, server, "Renamed Merchant")
	want := &OperationSummary{
		Kind: "rename", Label: "Rename merchant", Affected: 1, Undoable: true,
	}
	assert.Equal(t, want, mutated.Operation)

	undone := requestRevisionAction(t, server, "/api/v1/undo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: mutated.Revision,
		Query: mutated.CanonicalQuery, Selection: mutated.Projection.Selection,
		Window: Window{Limit: 200},
	})
	assert.Equal(t, want, undone.Operation)
	redone := requestRevisionAction(t, server, "/api/v1/redo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: undone.Revision,
		Query: undone.CanonicalQuery, Selection: undone.Projection.Selection,
		Window: Window{Limit: 200},
	})
	assert.Equal(t, want, redone.Operation)

	committed := commitHTTP(t, server, redone)
	assert.Nil(t, committed.Operation)
}

func TestMutationHideTwiceReportsHideThenShow(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	initial := projectQuery(t, server, query)
	require.NotEmpty(t, initial.DetailRows)
	toggle := func(previous Projection, revision string) MutationResponse {
		return mutateHTTP(t, server, MutationBody{
			Version: MutationSchemaVersion, ExpectedRevision: revision,
			Query: query, Selection: previous.Selection, Action: app.ActionToggleHidden,
			Target: &TransitionTarget{
				Kind: app.IdentityTransaction, Identity: initial.DetailRows[0].Identity,
			},
			Window: Window{Limit: 200},
		})
	}
	hidden := toggle(initial, initial.Revision)
	assert.Equal(t, &OperationSummary{
		Kind: "hide", Label: "Hide transactions", Affected: 1, Undoable: true,
	}, hidden.Operation)
	shown := toggle(hidden.Projection, hidden.Revision)
	assert.Equal(t, &OperationSummary{
		Kind: "show", Label: "Show transactions", Affected: 1, Undoable: false,
	}, shown.Operation, "cancelling a pending hide rewrites the journal, so u cannot undo it")
	assert.Zero(t, shown.Pending.ActiveOperations, "the second toggle cancels the first")
}
