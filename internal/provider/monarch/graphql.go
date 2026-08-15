package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
)

const (
	defaultMaxBodyBytes int64 = 8 << 20
	userAgent                 = "moneyflow-go-v2"
)

var (
	defaultGraphQLURL = mustParseURL("https://api.monarch.com/graphql")
	defaultLoginURL   = mustParseURL("https://api.monarch.com/auth/login/")
)

type graphQLRequest struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

func graphQLCall[T any](
	ctx context.Context,
	client *Client,
	operation string,
	query string,
	variables map[string]any,
) (T, error) {
	var zero T
	body, err := json.Marshal(graphQLRequest{
		OperationName: operation,
		Query:         query,
		Variables:     variables,
	})
	if err != nil {
		return zero, fmt.Errorf("encode Monarch GraphQL request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.options.GraphQLURL.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return zero, fmt.Errorf("create Monarch GraphQL request: %w", err)
	}
	setReadHeaders(request.Header, client.authorization, client.deviceUUID)
	response, err := client.options.HTTPClient.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr
		}
		return zero, providerErrorForTransport(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return zero, providerErrorForResponse(response, client.options.Now())
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, client.options.MaxBodyBytes+1))
	if err != nil {
		return zero, provider.NewError(provider.CodeUnavailable)
	}
	if int64(len(payload)) > client.options.MaxBodyBytes {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	var envelope graphQLResponse
	if err = json.Unmarshal(payload, &envelope); err != nil || len(envelope.Errors) > 0 {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	if err = json.Unmarshal(data, &zero); err != nil {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	return zero, nil
}

func setReadHeaders(header http.Header, authorization string, deviceUUID string) {
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/json")
	header.Set("User-Agent", userAgent)
	header.Set("Client-Platform", "web")
	header.Set("Origin", "https://app.monarch.com")
	header.Set("X-CIO-Client-Platform", "web")
	if authorization != "" {
		header.Set("Authorization", "Token "+authorization)
	}
	if deviceUUID != "" {
		header.Set("Device-UUID", deviceUUID)
	}
}

func providerErrorForTransport(err error) error {
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return provider.NewError(provider.CodeUnavailable)
	}
	return provider.NewError(provider.CodeUnavailable)
}

func providerErrorForStatus(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return provider.NewError(provider.CodeReconnectRequired)
	case http.StatusTooManyRequests:
		return provider.NewError(provider.CodeRateLimited)
	default:
		if status >= http.StatusInternalServerError {
			return provider.NewError(provider.CodeUnavailable)
		}
		return provider.NewError(provider.CodeDataInvalid)
	}
}

func providerErrorForResponse(response *http.Response, now time.Time) error {
	if response.StatusCode != http.StatusTooManyRequests {
		return providerErrorForStatus(response.StatusCode)
	}
	retryAfter, ok := parseRetryAfter(response.Header.Get("Retry-After"), now)
	if !ok {
		return provider.NewError(provider.CodeRateLimited)
	}
	return provider.NewErrorWithRetry(provider.CodeRateLimited, retryAfter)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 || seconds > int64(provider.MaxRetryAfter/time.Second) {
			if seconds > int64(provider.MaxRetryAfter/time.Second) {
				return provider.MaxRetryAfter, true
			}
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now)
	if delay <= 0 {
		return 0, false
	}
	return min(delay, provider.MaxRetryAfter), true
}

func cloneHTTPClient(source *http.Client) *http.Client {
	clone := *source
	previousRedirect := source.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameOrigin(via[0].URL, request.URL) {
			request.Header.Del("Authorization")
			request.Header.Del("Device-UUID")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return left.Scheme == right.Scheme && left.Host == right.Host
}

func validateEndpoint(name string, endpoint *url.URL, production *url.URL) error {
	if endpoint == nil || endpoint.Host == "" {
		return fmt.Errorf("monarch %s endpoint is missing", name)
	}
	if endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname()) {
		return nil
	}
	if endpoint.Scheme != "https" {
		return fmt.Errorf("monarch %s endpoint must use HTTPS", name)
	}
	if endpoint.String() != production.String() {
		return fmt.Errorf("monarch %s endpoint must use the fixed production origin", name)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func mustParseURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}
