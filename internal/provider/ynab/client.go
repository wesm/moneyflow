package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
)

const defaultMaximumBodyBytes = int64(512 << 20)

var defaultBaseURL = &url.URL{Scheme: "https", Host: "api.ynab.com", Path: "/v1/"}

// ClientOptions inject bounded outbound transport behavior.
type ClientOptions struct {
	HTTPClient   *http.Client
	BaseURL      *url.URL
	MaxBodyBytes int64
}

// Client is the minimal YNAB read client.
type Client struct {
	httpClient   *http.Client
	baseURL      *url.URL
	accessToken  string
	maxBodyBytes int64
}

// NewClient constructs a direct REST reader without exposing the token in errors.
func NewClient(options ClientOptions, accessToken string) (*Client, error) {
	if accessToken == "" || strings.TrimSpace(accessToken) != accessToken {
		return nil, errors.New("create YNAB client: access token is invalid")
	}
	if options.BaseURL == nil {
		defaultURLCopy := *defaultBaseURL
		options.BaseURL = &defaultURLCopy
	}
	if !options.BaseURL.IsAbs() || options.BaseURL.Host == "" {
		return nil, errors.New("create YNAB client: base URL is invalid")
	}
	copyURL := *options.BaseURL
	if !strings.HasSuffix(copyURL.Path, "/") {
		copyURL.Path += "/"
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	} else if options.HTTPClient.Timeout <= 0 {
		return nil, errors.New("create YNAB client: timeout must be positive")
	}
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = defaultMaximumBodyBytes
	}
	if options.MaxBodyBytes < 1 {
		return nil, errors.New("create YNAB client: response limit must be positive")
	}
	return &Client{
		httpClient: options.HTTPClient, baseURL: &copyURL,
		accessToken: accessToken, maxBodyBytes: options.MaxBodyBytes,
	}, nil
}

// ListPlans returns the token-visible onboarding choices.
func (client *Client) ListPlans(ctx context.Context) ([]PlanSummary, error) {
	var response plansResponse
	if err := client.getJSON(ctx, "plans", &response); err != nil {
		return nil, err
	}
	if response.Data.Plans == nil {
		return nil, provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	return append([]PlanSummary(nil), (*response.Data.Plans)...), nil
}

// FetchPlan retrieves one complete, non-delta plan document.
func (client *Client) FetchPlan(ctx context.Context, planID string) (PlanDocument, error) {
	if planID == "" || strings.TrimSpace(planID) != planID {
		return PlanDocument{}, provider.NewError(provider.CodeIdentityMismatch)
	}
	var response planResponse
	if err := client.getJSON(ctx, "plans/"+url.PathEscape(planID), &response); err != nil {
		return PlanDocument{}, err
	}
	plan := response.Data.Plan
	if plan.ID != planID {
		return PlanDocument{}, provider.NewError(provider.CodeIdentityMismatch)
	}
	if response.Data.ServerKnowledge == nil || *response.Data.ServerKnowledge < 0 ||
		plan.Accounts == nil || plan.Payees == nil || plan.CategoryGroups == nil ||
		plan.Categories == nil || plan.Transactions == nil || plan.Subtransactions == nil {
		return PlanDocument{}, provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	return PlanDocument{
		ID: plan.ID, Name: plan.Name, CurrencyFormat: plan.CurrencyFormat,
		Accounts:        append([]Account(nil), (*plan.Accounts)...),
		Payees:          append([]Payee(nil), (*plan.Payees)...),
		CategoryGroups:  append([]CategoryGroup(nil), (*plan.CategoryGroups)...),
		Categories:      append([]Category(nil), (*plan.Categories)...),
		Transactions:    append([]Transaction(nil), (*plan.Transactions)...),
		Subtransactions: append([]Subtransaction(nil), (*plan.Subtransactions)...),
		ServerKnowledge: *response.Data.ServerKnowledge,
	}, nil
}

func (client *Client) getJSON(ctx context.Context, relative string, target any) error {
	endpoint := client.baseURL.String() + relative
	// #nosec G704 -- NewClient validates and privately owns the absolute base URL; callers supply
	// only the fixed plans path or a PathEscape-encoded plan identity.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.NewError(provider.CodeUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+client.accessToken)
	request.Header.Set("Accept", "application/json")
	// #nosec G704 -- the request URL is constructed only through the validated client boundary
	// above; the injected HTTP client is required for bounded transport tests.
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return provider.NewError(provider.CodeUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return translateHTTPFailure(response)
	}
	contentType := response.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	limited := io.LimitReader(response.Body, client.maxBodyBytes+1)
	contents, err := io.ReadAll(limited)
	if err != nil || int64(len(contents)) > client.maxBodyBytes {
		return provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	if err = decodeJSON(contents, target); err != nil {
		return provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	return nil
}

func decodeJSON(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func translateHTTPFailure(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return provider.NewError(provider.CodeReconnectRequired)
	case http.StatusNotFound:
		return provider.NewError(provider.CodeIdentityMismatch)
	case http.StatusTooManyRequests:
		if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 {
			delay := time.Duration(seconds) * time.Second
			if delay <= provider.MaxRetryAfter {
				return provider.NewErrorWithRetry(provider.CodeRateLimited, delay)
			}
		}
		return provider.NewError(provider.CodeRateLimited)
	default:
		if response.StatusCode >= 500 {
			return provider.NewError(provider.CodeUnavailable)
		}
		return provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
}
