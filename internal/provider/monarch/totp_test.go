package monarch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTOTPCodeMatchesRFC6238AndNormalizesTheSecret(t *testing.T) {
	t.Parallel()
	code, err := GenerateTOTPCode(
		"gez dgnbv gy3t qojq gezd gnbv gy3t qojq",
		time.Unix(59, 0).UTC(),
	)
	require.NoError(t, err)
	assert.Equal(t, "287082", code)
}

func TestGenerateTOTPCodeRejectsInvalidSecret(t *testing.T) {
	t.Parallel()
	_, err := GenerateTOTPCode("not a base32 secret!", time.Unix(59, 0).UTC())
	assert.Error(t, err)
}
