package domain

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollisionKeyNormalizesUnicodeCaseAndWhitespace(t *testing.T) {
	t.Parallel()

	key, err := CollisionKey("  Ａｍａｚｏｎ\u2003  CAFÉ  ")
	require.NoError(t, err)
	require.Equal(t, "amazon café", key)
}

func TestNormalizeDisplayLabelTrimsButPreservesDisplayCharacters(t *testing.T) {
	t.Parallel()

	label, err := NormalizeDisplayLabel("\u2003Example  Merchant\u00a0")
	require.NoError(t, err)
	require.Equal(t, "Example  Merchant", label)
}

func TestNormalizeDisplayLabelRejectsEmptyAndControlCharacters(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"", " \t ", "Example\nMerchant", "Example\u007fMerchant"} {
		_, err := NormalizeDisplayLabel(label)
		require.Error(t, err, label)
	}
}

func TestNewEntityIDUsesInjected128BitRandomness(t *testing.T) {
	t.Parallel()

	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))
	id, err := NewEntityID(EntityKindMerchant, random)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(id), "merchant_"))
	require.Len(t, strings.TrimPrefix(string(id), "merchant_"), 26)
	require.Equal(t, 0, random.Len())
}

func TestNewEntityIDRejectsUnknownKindAndShortRandomness(t *testing.T) {
	t.Parallel()

	_, err := NewEntityID(EntityKind("unknown"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	require.Error(t, err)

	_, err = NewEntityID(EntityKindMerchant, bytes.NewReader([]byte{1, 2, 3}))
	require.Error(t, err)
}

func TestNewOperationIDUsesSeparatePrefix(t *testing.T) {
	t.Parallel()

	id, err := NewOperationID(bytes.NewReader(bytes.Repeat([]byte{0x24}, 16)))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(id, "operation_"))
	require.Len(t, strings.TrimPrefix(id, "operation_"), 26)
}
