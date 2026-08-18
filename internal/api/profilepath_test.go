package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testProfileID  = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherProfileID = "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestProfileAPIPathPreservesBasePathAndCanonicalID(t *testing.T) {
	t.Parallel()

	path, err := ProfileAPIPath("/moneyflow/", testProfileID, "view")
	require.NoError(t, err)
	assert.Equal(t, "/moneyflow/api/v1/profiles/"+testProfileID+"/view", path)

	path, err = ProfileAPIPath("/", testProfileID, "onboarding/attempt/status")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/profiles/"+testProfileID+"/onboarding/attempt/status", path)
}

func TestProfileAPIPathRejectsNoncanonicalInputs(t *testing.T) {
	t.Parallel()

	for _, profileID := range []string{
		"", "legacy", "profile_AAAAAAAAAAAAAAAAAAAAAAAAAA", "profile_aaaaaaaaaaaaaaaaaaaaaaaaa/",
		"profile_aaaaaaaaaaaaaaaaaaaaaaaaa%2f", ".", "..",
	} {
		_, err := ProfileAPIPath("/moneyflow/", profileID, "view")
		assert.Error(t, err, profileID)
	}
	for _, endpoint := range []string{"", "/view", "view/", "../view", "view/./rows", "view%2frows"} {
		_, err := ProfileAPIPath("/moneyflow/", testProfileID, endpoint)
		assert.Error(t, err, endpoint)
	}
}

func TestParseProfileAPIPathRejectsEncodedOrDotSegments(t *testing.T) {
	t.Parallel()

	profileID, endpoint, err := ParseProfileAPIPath(
		"/moneyflow/", "/moneyflow/api/v1/profiles/"+testProfileID+"/view/transition",
	)
	require.NoError(t, err)
	assert.Equal(t, testProfileID, profileID)
	assert.Equal(t, "view/transition", endpoint)

	for _, path := range []string{
		"/moneyflow/api/v1/profiles/" + testProfileID + "%2fother/view",
		"/moneyflow/api/v1/profiles/" + testProfileID + "/../view",
		"/moneyflow/api/v1/profiles/" + testProfileID + "/view/./rows",
		"/moneyflow/api/v1/profiles/legacy/view",
		"/other/api/v1/profiles/" + testProfileID + "/view",
	} {
		_, _, err = ParseProfileAPIPath("/moneyflow/", path)
		assert.Error(t, err, path)
	}
}
