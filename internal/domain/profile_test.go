package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileSnapshotValidatesCursorJournalAndKnownDrills(t *testing.T) {
	t.Parallel()

	operation := validOperations()["merchant label"]
	operation.Sequence = 4
	snapshot := ProfileSnapshot{
		Revision: 7, Cursor: 1, Committed: validCommittedProfile(t), Journal: []Operation{operation},
		KnownDrills: []DrillIdentity{{Dimension: DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_example"}},
	}
	require.NoError(t, snapshot.Validate())

	clone := snapshot.Clone()
	clone.Journal[0].Targets[0] = "changed"
	clone.KnownDrills[0].Key = "changed"
	assert.Equal(t, EntityID("merchant_a"), snapshot.Journal[0].Targets[0])
	assert.Equal(t, "merchant_example", snapshot.KnownDrills[0].Key)
}

func TestProfileSnapshotRejectsInvalidCursorSequenceAndDrills(t *testing.T) {
	t.Parallel()

	operation := validOperations()["merchant label"]
	operation.Sequence = 4
	base := ProfileSnapshot{
		Revision: 7, Cursor: 1, Committed: validCommittedProfile(t), Journal: []Operation{operation},
		KnownDrills: []DrillIdentity{{Dimension: DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_example"}},
	}

	tests := map[string]func(*ProfileSnapshot){
		"negative cursor":    func(snapshot *ProfileSnapshot) { snapshot.Cursor = -1 },
		"cursor beyond head": func(snapshot *ProfileSnapshot) { snapshot.Cursor = 2 },
		"sequence order": func(snapshot *ProfileSnapshot) {
			second := snapshot.Journal[0].Clone()
			second.ID = "operation_bbbbbbbbbbbbbbbbbbbbbbbbbb"
			snapshot.Journal = append(snapshot.Journal, second)
		},
		"invalid drill": func(snapshot *ProfileSnapshot) { snapshot.KnownDrills[0].Key = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := base.Clone()
			mutate(&snapshot)
			assert.Error(t, snapshot.Validate())
		})
	}
}

func TestDrillIdentityCanonicalKeyValidatesCompletePartition(t *testing.T) {
	t.Parallel()

	identity := DrillIdentity{
		Dimension: DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_example",
	}
	key, err := identity.CanonicalKey()
	require.NoError(t, err)
	assert.Equal(t, "merchant\x00USD\x00002\x00merchant_example", key)

	tests := []DrillIdentity{
		{Dimension: DimensionTime, Currency: "USD", Scale: 2, Key: "2026"},
		{Dimension: DimensionMerchant, Currency: "usd", Scale: 2, Key: "merchant_example"},
		{Dimension: DimensionMerchant, Currency: "USD", Scale: 10, Key: "merchant_example"},
		{Dimension: DimensionMerchant, Currency: "USD", Scale: 2},
	}
	for _, candidate := range tests {
		_, err = candidate.CanonicalKey()
		assert.Error(t, err)
	}
}
