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

type graphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
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

	payload, err := io.ReadAll(io.LimitReader(response.Body, client.options.MaxBodyBytes+1))
	if err != nil {
		return zero, provider.NewError(provider.CodeUnavailable)
	}
	if int64(len(payload)) > client.options.MaxBodyBytes {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return zero, providerErrorForStatus(response.StatusCode)
	}
	var envelope graphQLResponse[T]
	if err = json.Unmarshal(payload, &envelope); err != nil || len(envelope.Errors) > 0 {
		return zero, provider.NewError(provider.CodeDataInvalid)
	}
	return envelope.Data, nil
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
