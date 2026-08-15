package monarch

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wesm/moneyflow/internal/provider"
)

const loginMutation = `
mutation LoginMutation($email: String!, $password: String!, $rememberMe: Boolean!, $totpToken: String) {
  login(email: $email, password: $password, rememberMe: $rememberMe, totpToken: $totpToken) {
    token
    errors { messages }
  }
}`

var errMFARequired = errors.New("monarch MFA required")

type loginFailure struct {
	Messages []string `json:"messages"`
}

// Authenticator implements the provider-neutral connection and validation capability.
type Authenticator struct {
	options Options
}

// NewAuthenticator validates injected dependencies.
func NewAuthenticator(options Options) (*Authenticator, error) {
	client, err := NewClient(options, "", "")
	if err != nil {
		return nil, err
	}
	if client.options.Random == nil {
		client.options.Random = cryptorand.Reader
	}
	return &Authenticator{options: client.options}, nil
}

// Connect authenticates through REST first, handles one MFA challenge, and validates identity.
func (authenticator *Authenticator) Connect(
	ctx context.Context,
	credentials provider.Credentials,
	respond provider.ChallengeResponder,
) (provider.Session, error) {
	if strings.TrimSpace(credentials.Login) == "" || credentials.Password == "" {
		return nil, provider.NewError(provider.CodeReconnectRequired)
	}
	deviceUUID, err := newDeviceUUID(authenticator.options.Random)
	if err != nil {
		return nil, errors.New("create monarch device identity")
	}
	token, err := authenticator.login(ctx, credentials, deviceUUID, "")
	if errors.Is(err, errMFARequired) {
		if respond == nil {
			return nil, provider.NewError(provider.CodeReconnectRequired)
		}
		code, responseErr := respond(ctx, provider.Challenge{
			Kind: "mfa", Prompt: "Enter the Monarch verification code",
		})
		if responseErr != nil {
			return nil, responseErr
		}
		if strings.TrimSpace(code) == "" {
			return nil, provider.NewError(provider.CodeReconnectRequired)
		}
		token, err = authenticator.login(ctx, credentials, deviceUUID, code)
	}
	if err != nil {
		return nil, err
	}
	client, err := NewClient(authenticator.options, token, deviceUUID)
	if err != nil {
		return nil, provider.NewError(provider.CodeReconnectRequired)
	}
	subscription, err := client.GetSubscriptionDetails(ctx)
	if err != nil || strings.TrimSpace(subscription.ID) == "" {
		if err != nil {
			return nil, err
		}
		return nil, provider.NewError(provider.CodeDataInvalid)
	}
	issuedAt := authenticator.options.Now().UTC()
	return Session{
		Version:         sessionVersion,
		Token:           token,
		DeviceUUID:      deviceUUID,
		RemoteProfileID: subscription.ID,
		IssuedAt:        issuedAt,
		ValidatedAt:     authenticator.options.Now().UTC(),
	}, nil
}

// Validate probes a saved session and verifies its stored subscription identity.
func (authenticator *Authenticator) Validate(
	ctx context.Context,
	providerSession provider.Session,
) (provider.ProfileIdentity, error) {
	session, ok := providerSession.(Session)
	if !ok {
		if pointer, pointerOK := providerSession.(*Session); pointerOK && pointer != nil {
			session = *pointer
			ok = true
		}
	}
	if !ok || session.Validate() != nil {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	client, err := NewClient(authenticator.options, session.Token, session.DeviceUUID)
	if err != nil {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	subscription, err := client.GetSubscriptionDetails(ctx)
	if err != nil {
		return provider.ProfileIdentity{}, err
	}
	if subscription.ID == "" {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeDataInvalid)
	}
	if subscription.ID != session.RemoteProfileID {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeIdentityMismatch)
	}
	return provider.ProfileIdentity{Kind: providerKind, RemoteID: subscription.ID}, nil
}

func (authenticator *Authenticator) login(
	ctx context.Context,
	credentials provider.Credentials,
	deviceUUID string,
	code string,
) (string, error) {
	token, status, err := authenticator.restLogin(ctx, credentials, deviceUUID, code)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return authenticator.graphQLLogin(ctx, credentials, deviceUUID, code)
	}
	if status == http.StatusForbidden && code == "" {
		return "", errMFARequired
	}
	if status != http.StatusOK {
		return "", providerErrorForStatus(status)
	}
	if token == "" {
		return "", provider.NewError(provider.CodeDataInvalid)
	}
	return token, nil
}

func (authenticator *Authenticator) restLogin(
	ctx context.Context,
	credentials provider.Credentials,
	deviceUUID string,
	code string,
) (string, int, error) {
	body := struct {
		Username          string `json:"username"`
		Password          string `json:"password"`
		TrustedDevice     bool   `json:"trusted_device"`
		SupportsMFA       bool   `json:"supports_mfa"`
		SupportsEmailOTP  bool   `json:"supports_email_otp"`
		SupportsRecaptcha bool   `json:"supports_recaptcha"`
		TOTP              string `json:"totp,omitempty"`
	}{
		Username: credentials.Login, Password: credentials.Password, TrustedDevice: true,
		SupportsMFA: true, SupportsEmailOTP: true, SupportsRecaptcha: true, TOTP: code,
	}
	encoded, err := json.Marshal(body) //nolint:gosec // transient provider login body; never persisted or logged.
	if err != nil {
		return "", 0, errors.New("encode monarch login")
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, authenticator.options.LoginURL.String(), bytes.NewReader(encoded),
	)
	if err != nil {
		return "", 0, errors.New("create monarch login request")
	}
	setReadHeaders(request.Header, "", deviceUUID)
	response, err := credentialHTTPClient(authenticator.options.HTTPClient).Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", 0, ctxErr
		}
		return "", 0, provider.NewError(provider.CodeUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", response.StatusCode, nil
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, authenticator.options.MaxBodyBytes+1))
	if err != nil {
		return "", 0, provider.NewError(provider.CodeUnavailable)
	}
	if int64(len(payload)) > authenticator.options.MaxBodyBytes {
		return "", 0, provider.NewError(provider.CodeDataInvalid)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(payload, &result); err != nil {
		return "", 0, provider.NewError(provider.CodeDataInvalid)
	}
	return result.Token, response.StatusCode, nil
}

func (authenticator *Authenticator) graphQLLogin(
	ctx context.Context,
	credentials provider.Credentials,
	deviceUUID string,
	code string,
) (string, error) {
	credentialOptions := authenticator.options
	credentialOptions.HTTPClient = credentialHTTPClient(authenticator.options.HTTPClient)
	client, err := NewClient(credentialOptions, "", deviceUUID)
	if err != nil {
		return "", provider.NewError(provider.CodeReconnectRequired)
	}
	variables := map[string]any{
		"email": credentials.Login, "password": credentials.Password, "rememberMe": true,
	}
	if code != "" {
		variables["totpToken"] = code
	}
	type loginData struct {
		Login struct {
			Token  string         `json:"token"`
			Errors []loginFailure `json:"errors"`
		} `json:"login"`
	}
	data, err := graphQLCall[loginData](ctx, client, "LoginMutation", loginMutation, variables)
	if err != nil {
		return "", err
	}
	if code == "" && loginRequiresMFA(data.Login.Errors) {
		return "", errMFARequired
	}
	if len(data.Login.Errors) > 0 || data.Login.Token == "" {
		return "", provider.NewError(provider.CodeReconnectRequired)
	}
	return data.Login.Token, nil
}

func credentialHTTPClient(source *http.Client) *http.Client {
	client := cloneHTTPClient(source)
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func loginRequiresMFA(failures []loginFailure) bool {
	for _, failure := range failures {
		for _, message := range failure.Messages {
			switch strings.ToLower(strings.TrimSpace(message)) {
			case "mfa required", "multi-factor auth required",
				"multi-factor authentication required", "two-factor authentication required":
				return true
			}
		}
	}
	return false
}

func newDeviceUUID(random io.Reader) (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32],
	), nil
}

var _ provider.Connector = (*Authenticator)(nil)
