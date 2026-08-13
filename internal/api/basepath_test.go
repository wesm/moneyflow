package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBasePath(t *testing.T) {
	t.Parallel()

	accepted := map[string]string{
		"":            "/",
		"/":           "/",
		"moneyflow":   "/moneyflow/",
		"/moneyflow":  "/moneyflow/",
		"/moneyflow/": "/moneyflow/",
		"a/b":         "/a/b/",
	}
	for input, expected := range accepted {
		t.Run("accept "+input, func(t *testing.T) {
			t.Parallel()
			actual, err := NormalizeBasePath(input)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}

	for _, input := range []string{
		".",
		"..",
		"/a/../b",
		"/a/./b",
		"/a//b",
		"/a?x=1",
		"/a#fragment",
		`/a\b`,
		"/a/%2f/b",
		"/a/%2F/b",
		"/a/%5c/b",
		"https://example.com/a",
		"/a%zz",
	} {
		t.Run("reject "+input, func(t *testing.T) {
			t.Parallel()
			_, err := NormalizeBasePath(input)
			assert.Error(t, err)
		})
	}
}
