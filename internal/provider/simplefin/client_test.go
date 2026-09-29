package simplefin

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (run roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return run(request)
}

func TestClaimAndAccessURL(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "bridge.simplefin.org", request.URL.Host)
		require.Equal(t, "/simplefin/claim/example", request.URL.Path)
		require.Empty(t, request.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("https://user:synthetic-secret@bridge.simplefin.org/simplefin\n"))}, nil
	})}
	input := base64.RawURLEncoding.EncodeToString([]byte("https://bridge.simplefin.org/simplefin/claim/example"))
	access, err := Claim(t.Context(), input, client)
	require.NoError(t, err)
	require.Equal(t, "https://user:synthetic-secret@bridge.simplefin.org/simplefin", access)
	again, err := Claim(t.Context(), access+"/", client)
	require.NoError(t, err)
	require.Equal(t, access, again)
	require.Equal(t, 1, calls)
	for _, invalid := range []string{"invalid token", base64.StdEncoding.EncodeToString([]byte("https://example.com/claim/secret")), base64.StdEncoding.EncodeToString([]byte("http://bridge.simplefin.org/claim/secret"))} {
		_, err = Claim(t.Context(), invalid, client)
		require.Error(t, err)
	}
	require.Equal(t, 1, calls)
}

func TestClaimRejectionDoesNotRetryOrDisclose(t *testing.T) {
	for _, status := range []int{403, 302, 500} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://example.com/secret"}}, Body: io.NopCloser(strings.NewReader("synthetic-secret"))}, nil
		})}
		_, err := Claim(t.Context(), base64.StdEncoding.EncodeToString([]byte("https://bridge.simplefin.org/claim/example")), client)
		require.Error(t, err)
		require.Equal(t, 1, calls)
		require.NotContains(t, err.Error(), "synthetic-secret")
		if status == 403 {
			require.ErrorIs(t, err, ErrClaimRejected)
		}
	}
}

func TestFetchStatusMappingAndBounds(t *testing.T) {
	for _, test := range []struct {
		status int
		code   provider.ErrorCode
	}{
		{401, provider.CodeReconnectRequired}, {403, provider.CodeReconnectRequired},
		{402, provider.CodeUnavailable}, {429, provider.CodeRateLimited}, {503, provider.CodeUnavailable}, {302, provider.CodeUnavailable},
	} {
		client := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "https://example.com/secret")
			w.Header().Set("Retry-After", "7200")
			w.WriteHeader(test.status)
			_, _ = io.WriteString(w, "synthetic-secret")
		})
		_, err := client.FetchSnapshot(t.Context(), shortFetch(), nil)
		require.Error(t, err)
		code, ok := provider.CodeOf(err)
		require.True(t, ok)
		require.Equal(t, test.code, code)
		require.NotContains(t, err.Error(), "synthetic-secret")
		if test.status == 402 {
			require.ErrorIs(t, err, provider.ErrPaymentRequired)
		}
		if test.status == 429 {
			retry, _ := provider.RetryAfterOf(err)
			require.Equal(t, 2*time.Hour, retry)
		}
	}
	client := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat(" ", maxAccountsBytes+1))
	})
	_, err := client.FetchSnapshot(t.Context(), shortFetch(), nil)
	require.Error(t, err)
	code, _ := provider.CodeOf(err)
	require.Equal(t, provider.CodeDataInvalid, code)
}

func TestAccessURLValidationAndIdentity(t *testing.T) {
	for _, invalid := range []string{"http://user:password@example.com", "https://example.com", "https://user@example.com",
		"https://user:password@example.com?secret=value", "https://user:password@example.com#secret", "https://user:pass%0Aword@example.com"} {
		_, err := NewClient(ClientOptions{AccessURL: invalid, Import: ImportConfig{Currency: "USD", Scale: 2}})
		require.Error(t, err)
		require.NotContains(t, err.Error(), invalid)
	}
	calls := 0
	transport := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic-secret") })}
	a, err := NewClient(ClientOptions{AccessURL: "https://user:password@EXAMPLE.COM/simplefin/", Import: ImportConfig{Currency: "USD", Scale: 2}, HTTPClient: transport}) // #nosec G101 -- synthetic URL, injected transport only.
	require.NoError(t, err)
	b, err := NewClient(ClientOptions{AccessURL: "https://user:password@example.com/simplefin", Import: ImportConfig{Currency: "USD", Scale: 2}, HTTPClient: transport}) // #nosec G101 -- synthetic URL, injected transport only.
	require.NoError(t, err)
	identity, err := a.ProbeIdentity(t.Context())
	require.NoError(t, err)
	other, err := b.ProbeIdentity(t.Context())
	require.NoError(t, err)
	require.Equal(t, identity, other)
	require.Len(t, identity.RemoteID, 64)
	require.Zero(t, calls)
	_, err = a.FetchSnapshot(t.Context(), shortFetch(), nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-secret")
}
