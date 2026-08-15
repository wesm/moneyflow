package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginResolution(t *testing.T) {
	t.Parallel()
	loopback, err := ResolveOrigin("127.0.0.1:8080", "/moneyflow", "")
	require.NoError(t, err)
	assert.Equal(t, "/moneyflow/", loopback.BasePath)
	assert.Equal(t, "http://127.0.0.1:8080/moneyflow/", loopback.Canonical.String())
	assert.Equal(t, "http://127.0.0.1:8080", loopback.Origin())

	external, err := ResolveOrigin(
		"127.0.0.1:8080", "/moneyflow/", "https://moneyflow.example/moneyflow",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://moneyflow.example/moneyflow/", external.Canonical.String())

	canonical, err := ResolveOrigin(
		"127.0.0.1:8080", "/moneyflow/", "https://Moneyflow.Example:443/moneyflow",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://moneyflow.example", canonical.Origin())
	assert.Equal(t, "https://moneyflow.example/moneyflow/", canonical.Canonical.String())

	for _, value := range []string{
		"https://user@moneyflow.example/moneyflow",
		"https://moneyflow.example/moneyflow?q=private",
		"https://moneyflow.example/moneyflow#fragment",
		"ftp://moneyflow.example/moneyflow",
		"https://0.0.0.0/moneyflow",
		"https://moneyflow.example/other",
		"//moneyflow.example/moneyflow",
	} {
		_, err = ResolveOrigin("127.0.0.1:8080", "/moneyflow", value)
		assert.Error(t, err, value)
	}
}

func TestMutationTokenRoundTripAndBinding(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	issued, err := security.Issue()
	require.NoError(t, err)
	assert.NotContains(t, issued.Value, "moneyflow.example")
	assert.Equal(t, now.Add(time.Hour), issued.ExpiresAt)
	require.NoError(t, security.Verify(issued.Value))

	otherOrigin := mustOriginConfig(t, "https://other.example/moneyflow")
	other, err := NewMutationSecurity(otherOrigin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	assert.ErrorIs(t, other.Verify(issued.Value), ErrInvalidMutationToken)

	otherPath := mustOriginConfig(t, "https://moneyflow.example/other")
	other, err = NewMutationSecurity(otherPath, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	assert.ErrorIs(t, other.Verify(issued.Value), ErrInvalidMutationToken)

	encoded, _, ok := strings.Cut(issued.Value, ".")
	require.True(t, ok)
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var claims mutationTokenClaims
	require.NoError(t, json.Unmarshal(payload, &claims))
	claims.Version = "2"
	payload, err = json.Marshal(claims)
	require.NoError(t, err)
	encoded = base64.RawURLEncoding.EncodeToString(payload)
	wrongVersion := encoded + "." + base64.RawURLEncoding.EncodeToString(security.sign(encoded))
	assert.ErrorIs(t, security.Verify(wrongVersion), ErrInvalidMutationToken)

	otherInstance, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x24}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	assert.ErrorIs(t, otherInstance.Verify(issued.Value), ErrTokenExpired)
}

func TestMutationTokenRejectsExpiryClockSkewAndMalformedValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	clock := now
	security, err := NewMutationSecurity(
		mustOriginConfig(t, "https://moneyflow.example/moneyflow"),
		bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return clock },
	)
	require.NoError(t, err)
	issued, err := security.Issue()
	require.NoError(t, err)

	clock = now.Add(time.Hour)
	err = security.Verify(issued.Value)
	assert.ErrorIs(t, err, ErrTokenExpired)
	clock = now.Add(time.Hour + time.Nanosecond)
	assert.ErrorIs(t, security.Verify(issued.Value), ErrTokenExpired)
	clock = now.Add(-5*time.Minute - time.Nanosecond)
	assert.Error(t, security.Verify(issued.Value))

	for _, token := range []string{"", "missing-dot", ".", "bad.bad", strings.Repeat("x", MaxMutationTokenBytes+1)} {
		assert.Error(t, security.Verify(token), token[:min(len(token), 20)])
	}
}

func TestMutationSecurityMiddlewareRejectsBeforeEvaluation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	issued, err := security.Issue()
	require.NoError(t, err)
	evaluated := 0
	handler := security.Protect(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		evaluated++
		response.WriteHeader(http.StatusNoContent)
	}))

	valid := httptest.NewRequest(http.MethodPost, "/moneyflow/api/v1/mutations", http.NoBody)
	valid.Header.Set("Origin", origin.Origin())
	valid.Header.Set("Sec-Fetch-Site", "same-origin")
	valid.Header.Set(MutationTokenHeader, issued.Value)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, 1, evaluated)

	for name, mutate := range map[string]func(*http.Request){
		"missing token": func(request *http.Request) { request.Header.Del(MutationTokenHeader) },
		"wrong origin":  func(request *http.Request) { request.Header.Set("Origin", "https://other.example") },
		"cross site":    func(request *http.Request) { request.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid.Clone(valid.Context())
			request.Header = valid.Header.Clone()
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assert.NotEqual(t, http.StatusNoContent, response.Code)
			assert.Equal(t, 1, evaluated)
		})
	}
}

func mustOriginConfig(t testing.TB, value string) OriginConfig {
	t.Helper()
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	basePath, err := NormalizeBasePath(parsed.Path)
	require.NoError(t, err)
	parsed.Path = basePath
	return OriginConfig{Canonical: parsed, BasePath: basePath}
}
