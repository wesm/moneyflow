package monarch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestAuthenticatorUsesRESTFirstAndValidatesSubscription(t *testing.T) {
	t.Parallel()

	var restCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/auth/login/":
			restCalls.Add(1)
			assert.Empty(t, request.Header.Get("Authorization"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "user@example.com", body["username"])
			assert.Equal(t, "transient-password", body["password"])
			_, _ = writer.Write([]byte(`{"token":"rest-token"}`))
		case "/graphql":
			writeSubscriptionResponse(t, writer, request, "subscription-a")
		default:
			http.NotFound(writer, request)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	sessionValue, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, nil)
	require.NoError(t, err)
	session := sessionValue.(Session)
	assert.Equal(t, "rest-token", session.Token)
	assert.Equal(t, "subscription-a", session.RemoteProfileID)
	assert.Equal(t, int32(1), restCalls.Load())
}

func TestAuthenticatorSendsGeneratedOneTimeCodeOnInitialLogin(t *testing.T) {
	t.Parallel()

	var challengeCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/auth/login/":
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "287082", body["totp"])
			_, _ = writer.Write([]byte(`{"token":"rest-token"}`))
		case "/graphql":
			writeSubscriptionResponse(t, writer, request, "subscription-a")
		default:
			http.NotFound(writer, request)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	_, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password", OneTimeCode: "287082",
	}, func(context.Context, provider.Challenge) (string, error) {
		challengeCalls.Add(1)
		return "must-not-be-used", nil
	})
	require.NoError(t, err)
	assert.Zero(t, challengeCalls.Load())
}

func TestAuthenticatorRetriesRESTMFAChallengeAfterInitialOneTimeCode(t *testing.T) {
	t.Parallel()

	var loginCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/auth/login/":
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			if loginCalls.Add(1) == 1 {
				assert.Equal(t, "287082", body["totp"])
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			assert.Equal(t, "123456", body["totp"])
			_, _ = writer.Write([]byte(`{"token":"rest-token"}`))
		case "/graphql":
			writeSubscriptionResponse(t, writer, request, "subscription-a")
		default:
			http.NotFound(writer, request)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	session, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password", OneTimeCode: "287082",
	}, func(context.Context, provider.Challenge) (string, error) {
		return "123456", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "rest-token", session.(Session).Token)
	assert.Equal(t, int32(2), loginCalls.Load())
}

func TestAuthenticatorRetriesGraphQLMFAChallengeAfterInitialOneTimeCode(t *testing.T) {
	t.Parallel()

	var loginCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/auth/login/" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		switch envelope.OperationName {
		case "LoginMutation":
			if loginCalls.Add(1) == 1 {
				assert.Equal(t, "287082", envelope.Variables["totpToken"])
				_, _ = writer.Write([]byte(`{"data":{"login":{"token":"","errors":[{"messages":["Multi-factor authentication required"]}]}}}`))
				return
			}
			assert.Equal(t, "123456", envelope.Variables["totpToken"])
			_, _ = writer.Write([]byte(`{"data":{"login":{"token":"graphql-token","errors":[]}}}`))
		case "GetSubscriptionDetails":
			_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"subscription-a"}}}`))
		default:
			t.Fatalf("unexpected operation %q", envelope.OperationName)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	session, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password", OneTimeCode: "287082",
	}, func(context.Context, provider.Challenge) (string, error) {
		return "123456", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "graphql-token", session.(Session).Token)
	assert.Equal(t, int32(2), loginCalls.Load())
}

func TestAuthenticatorFallsBackToProvenGraphQLLoginOnRESTNotFound(t *testing.T) {
	t.Parallel()

	var operations []string
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/auth/login/" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		operations = append(operations, envelope.OperationName)
		switch envelope.OperationName {
		case "LoginMutation":
			assert.Equal(t, "287082", envelope.Variables["totpToken"])
			_, _ = writer.Write([]byte(`{"data":{"login":{"token":"graphql-token","errors":[]}}}`))
		case "GetSubscriptionDetails":
			_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"subscription-a"}}}`))
		default:
			t.Fatalf("unexpected operation %q", envelope.OperationName)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	sessionValue, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password", OneTimeCode: "287082",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "graphql-token", sessionValue.(Session).Token)
	assert.Equal(t, []string{"LoginMutation", "GetSubscriptionDetails"}, operations)
}

func TestAuthenticatorCompletesMFAWithoutPersistingTheCode(t *testing.T) {
	t.Parallel()

	var loginCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/auth/login/":
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			if loginCalls.Add(1) == 1 {
				assert.NotContains(t, body, "totp")
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			assert.Equal(t, "123456", body["totp"])
			_, _ = writer.Write([]byte(`{"token":"mfa-token"}`))
		case "/graphql":
			writeSubscriptionResponse(t, writer, request, "subscription-a")
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)
	var challenge provider.Challenge
	sessionValue, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, func(_ context.Context, got provider.Challenge) (string, error) {
		challenge = got
		return "123456", nil
	})
	require.NoError(t, err)
	session := sessionValue.(Session)
	assert.Equal(t, "mfa", challenge.Kind)
	assert.Equal(t, "mfa-token", session.Token)
	serialized, err := json.Marshal(session)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "123456")
}

func TestAuthenticatorCompletesMFAThroughGraphQLFallback(t *testing.T) {
	t.Parallel()

	var loginCalls atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/auth/login/" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		switch envelope.OperationName {
		case "LoginMutation":
			if loginCalls.Add(1) == 1 {
				_, _ = writer.Write([]byte(`{"data":{"login":{"token":"","errors":[{"messages":["Multi-factor authentication required"]}]}}}`))
				return
			}
			assert.Equal(t, "123456", envelope.Variables["totpToken"])
			_, _ = writer.Write([]byte(`{"data":{"login":{"token":"graphql-mfa-token","errors":[]}}}`))
		case "GetSubscriptionDetails":
			_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"subscription-a"}}}`))
		default:
			t.Fatalf("unexpected operation %q", envelope.OperationName)
		}
	})
	authenticator := newTestAuthenticator(t, server.URL)

	session, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, func(context.Context, provider.Challenge) (string, error) {
		return "123456", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "graphql-mfa-token", session.(Session).Token)
}

func TestAuthenticatorRejectsCredentialRedirects(t *testing.T) {
	t.Parallel()

	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	t.Cleanup(target.Close)
	redirect := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	})
	authenticator := newTestAuthenticator(t, redirect.URL)

	_, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, nil)
	require.Error(t, err)
	assert.Zero(t, targetCalls.Load(), "credential-bearing request must not follow redirects")
}

func TestAuthenticatorClassifiesOversizedForbiddenBodyAsMFA(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := newAuthServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/auth/login/" {
			if attempts.Add(1) == 1 {
				writer.WriteHeader(http.StatusForbidden)
				_, _ = writer.Write([]byte(strings.Repeat("x", 128)))
				return
			}
			_, _ = writer.Write([]byte(`{"token":"mfa-token"}`))
			return
		}
		writeSubscriptionResponse(t, writer, request, "subscription-a")
	})
	options := testClientOptions(t, server.URL)
	options.MaxBodyBytes = 64
	options.Now = func() time.Time { return time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC) }
	options.Random = strings.NewReader(strings.Repeat("a", 64))
	authenticator, err := NewAuthenticator(options)
	require.NoError(t, err)

	_, err = authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, func(context.Context, provider.Challenge) (string, error) { return "123456", nil })
	require.NoError(t, err)
}

func TestAuthenticatorRejectsInvalidSessionAndRedactsLoginFailure(t *testing.T) {
	t.Parallel()

	server := newAuthServer(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`private login response`))
	})
	authenticator := newTestAuthenticator(t, server.URL)
	_, err := authenticator.Connect(context.Background(), provider.Credentials{
		Login: "user@example.com", Password: "transient-password",
	}, nil)
	code, ok := provider.CodeOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.CodeReconnectRequired, code)
	assert.NotContains(t, err.Error(), "private login response")

	_, err = authenticator.Validate(context.Background(), Session{Version: sessionVersion})
	code, ok = provider.CodeOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.CodeReconnectRequired, code)
}

func newAuthServer(
	t *testing.T,
	handler func(http.ResponseWriter, *http.Request),
) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	return server
}

func newTestAuthenticator(t *testing.T, serverURL string) *Authenticator {
	t.Helper()
	options := testClientOptions(t, serverURL)
	options.Now = func() time.Time {
		return time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	}
	options.Random = strings.NewReader(strings.Repeat("a", 64))
	authenticator, err := NewAuthenticator(options)
	require.NoError(t, err)
	return authenticator
}

func testClientOptions(t *testing.T, serverURL string) Options {
	t.Helper()
	if serverURL == "" {
		return Options{HTTPClient: &http.Client{Timeout: time.Second}}
	}
	base, err := url.Parse(serverURL)
	require.NoError(t, err)
	login := base.ResolveReference(&url.URL{Path: "/auth/login/"})
	graphql := base.ResolveReference(&url.URL{Path: "/graphql"})
	return Options{
		HTTPClient: &http.Client{Timeout: time.Second}, LoginURL: login, GraphQLURL: graphql,
	}
}

func writeSubscriptionResponse(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
	remoteID string,
) {
	t.Helper()
	var envelope graphQLRequest
	require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
	assert.Equal(t, "GetSubscriptionDetails", envelope.OperationName)
	_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"` + remoteID + `"}}}`))
}
