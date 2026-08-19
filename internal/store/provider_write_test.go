package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteItemUnionValidation(t *testing.T) {
	t.Parallel()

	hidden := true
	base := WriteItem{
		ID: "item-a", Position: 0, TransactionID: "transaction-a",
		TransactionExternalID: "provider-a", OriginatingOperationIDs: []string{"operation-a"},
		State: WriteItemPending,
	}
	inputs := PrepareProviderWriteInputs{
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a"},
	}
	validate := func(item WriteItem) error {
		return (PrepareProviderWritePlan{
			FrozenOperationIDs: []string{"operation-a"}, FrozenPrefixDigest: "digest-a",
			Items: []WriteItem{item},
		}).Validate(inputs)
	}

	update := base.Clone()
	update.Kind = WriteItemUpdate
	update.RequestedHidden = &hidden
	require.NoError(t, validate(update))

	deletion := base.Clone()
	deletion.Kind = WriteItemDelete
	require.NoError(t, validate(deletion))

	assert.Error(t, validate(base), "zero kind is never inferred")
	invalidDelete := deletion.Clone()
	invalidDelete.RequestedHidden = &hidden
	assert.Error(t, validate(invalidDelete))
	invalidUpdate := update.Clone()
	invalidUpdate.RequestedHidden = nil
	assert.Error(t, validate(invalidUpdate))
}

func TestWriteResultKindValidation(t *testing.T) {
	t.Parallel()

	update := WriteResult{
		Kind: WriteItemUpdate, ItemID: "item-a", TransactionExternalID: "provider-a",
	}
	require.NoError(t, update.Validate())
	update.AlreadyAbsent = true
	assert.Error(t, update.Validate())

	deletion := WriteResult{
		Kind: WriteItemDelete, ItemID: "item-a", TransactionExternalID: "provider-a",
		AlreadyAbsent: true,
	}
	require.NoError(t, deletion.Validate())
	hidden := true
	deletion.Hidden = &hidden
	assert.Error(t, deletion.Validate())

	assert.Error(t, (WriteResult{
		ItemID: "item-a", TransactionExternalID: "provider-a",
	}).Validate())
}
