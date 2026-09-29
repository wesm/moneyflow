// Package simplefin imports posted transactions through the read-only SimpleFIN protocol.
package simplefin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

const providerKind = "simplefin"

// ErrClaimRejected means the one-use token may have been used or revoked.
var ErrClaimRejected = errors.Join(provider.NewError(provider.CodeReconnectRequired),
	errors.New("SimpleFIN token was rejected; revoke the unused connection and obtain a new token"))

// ImportConfig fixes the exact money interpretation of one profile.
type ImportConfig struct {
	Currency domain.Currency `json:"currency"`
	Scale    uint8           `json:"scale"`
}

// Validate checks the repository's currency and precision contract.
func (config ImportConfig) Validate() error {
	if !domain.IsValidCurrency(config.Currency) || config.Scale > 9 {
		return errors.New("simplefin currency or precision is invalid")
	}
	return nil
}

// ClientOptions selects one credential and its exact money interpretation.
type ClientOptions struct {
	AccessURL  string
	Import     ImportConfig
	HTTPClient *http.Client
}

// Client is a read-only connection; it never opens local application storage.
type Client struct {
	endpoint                     *url.URL
	username, password, remoteID string
	config                       ImportConfig
	http                         *http.Client
}

var _ provider.Reader = (*Client)(nil)

// NewClient validates the credential without contacting its server.
func NewClient(options ClientOptions) (*Client, error) {
	if err := options.Import.Validate(); err != nil {
		return nil, err
	}
	access, err := canonicalAccessURL(options.AccessURL)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(access.String()))
	username := access.User.Username()
	password, _ := access.User.Password()
	access.User = nil
	access.Path += "/accounts"
	if access.RawPath != "" {
		access.RawPath += "/accounts"
	}
	return &Client{endpoint: access, username: username, password: password,
		remoteID: hex.EncodeToString(digest[:]), config: options.Import,
		http: noRedirectClient(options.HTTPClient)}, nil
}

func canonicalAccessURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || len(value) > 8192 || strings.TrimSpace(value) != value ||
		u.Scheme != "https" || u.Hostname() == "" || u.User == nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("invalid SimpleFIN Access URL")
	}
	password, present := u.User.Password()
	if !present || u.User.Username() == "" || password == "" || strings.Contains(u.User.Username(), ":") ||
		strings.ContainsFunc(u.User.Username()+password, unicode.IsControl) {
		return nil, errors.New("invalid SimpleFIN Access URL")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u, nil
}

func noRedirectClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	configured := *client
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &configured
}

// ProbeIdentity identifies the credential, not a verified financial-account owner.
func (client *Client) ProbeIdentity(ctx context.Context) (provider.ProfileIdentity, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProfileIdentity{}, err
	}
	return provider.ProfileIdentity{Kind: providerKind, RemoteID: client.remoteID}, nil
}

func readResponse(ctx context.Context, client *http.Client, request *http.Request, limit int64, now time.Time) ([]byte, error) {
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewError(provider.CodeUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, provider.NewError(provider.CodeReconnectRequired)
		case http.StatusPaymentRequired:
			return nil, provider.ErrPaymentRequired
		case http.StatusTooManyRequests:
			retry := time.Duration(0)
			value := response.Header.Get("Retry-After")
			if seconds, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil && seconds > 0 {
				retry = time.Duration(min(seconds, int64(provider.MaxRetryAfter/time.Second))) * time.Second
			} else if date, parseErr := http.ParseTime(value); parseErr == nil {
				retry = date.Sub(now)
			}
			return nil, provider.NewErrorWithRetry(provider.CodeRateLimited, retry)
		default:
			return nil, provider.NewError(provider.CodeUnavailable)
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewError(provider.CodeUnavailable)
	}
	if int64(len(body)) > limit {
		return nil, provider.NewError(provider.CodeDataInvalid)
	}
	return body, nil
}

// Claim exchanges a setup token once, or validates an already claimed Access URL.
// Callers must save its result before attempting an import; this function never retries.
func Claim(ctx context.Context, input string, client *http.Client) (string, error) {
	input = strings.TrimSpace(input)
	invalid := errors.New("enter a valid SimpleFIN setup token or HTTPS Access URL")
	if input == "" || len(input) > 8192 {
		return "", invalid
	}
	if strings.HasPrefix(input, "https://") {
		access, err := canonicalAccessURL(input)
		if err != nil {
			return "", invalid
		}
		return access.String(), nil
	}
	encoded := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimRight(input, "="))
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", invalid
	}
	claimURL, err := url.Parse(string(decoded))
	if err != nil || claimURL.Scheme != "https" || claimURL.User != nil || claimURL.RawQuery != "" ||
		claimURL.ForceQuery || claimURL.Fragment != "" || (claimURL.Port() != "" && claimURL.Port() != "443") ||
		(claimURL.Hostname() != "bridge.simplefin.org" && claimURL.Hostname() != "beta-bridge.simplefin.org") {
		return "", invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL.String(), nil)
	if err != nil {
		return "", invalid
	}
	body, err := readResponse(ctx, noRedirectClient(client), request, 8192, time.Now())
	if code, ok := provider.CodeOf(err); ok && code == provider.CodeReconnectRequired {
		return "", ErrClaimRejected
	}
	if err != nil {
		return "", err
	}
	access, err := canonicalAccessURL(strings.TrimSpace(string(body)))
	if err != nil {
		return "", provider.NewError(provider.CodeDataInvalid)
	}
	return access.String(), nil
}
