package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/httpsecurity"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

const (
	// MutationTokenHeader carries the browser-memory request-forgery token.
	// #nosec G101 -- this is the public protocol header name, not token material.
	MutationTokenHeader = "X-Moneyflow-Mutation-Token"
	// MaxMutationTokenBytes bounds parsing before base64 or JSON allocation.
	MaxMutationTokenBytes = 4096
	mutationTokenVersion  = "1"
	mutationTokenLifetime = time.Hour
	mutationClockSkew     = 5 * time.Minute
)

var (
	// ErrTokenExpired identifies a token that requires a safe bootstrap refresh.
	ErrTokenExpired = errors.New("mutation token expired")
	// ErrInvalidMutationToken identifies every other token validation failure.
	ErrInvalidMutationToken = errors.New("mutation token invalid")
)

// OriginConfig contains the one canonical browser URL and normalized mount path.
type OriginConfig = httpsecurity.OriginConfig

// ResolveOrigin validates the listener, external URL, and exact base-path relationship.
func ResolveOrigin(listen string, basePath string, externalURL string) (OriginConfig, error) {
	return httpsecurity.ResolveOrigin(listen, basePath, externalURL)
}

// IssuedMutationToken includes the opaque value and public refresh deadline.
type IssuedMutationToken struct {
	Value     string
	ExpiresAt time.Time
}

type mutationTokenClaims struct {
	Version  string `json:"v"`
	Instance string `json:"i"`
	Origin   string `json:"o"`
	BasePath string `json:"p"`
	Scope    string `json:"s"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// MutationSecurity owns one process-local signing secret and canonical browser origin.
type MutationSecurity struct {
	origin   OriginConfig
	secret   [32]byte
	instance string
	now      func() time.Time
}

// NewMutationSecurity creates a server-instance signer. Nil dependencies use secure defaults.
func NewMutationSecurity(
	origin OriginConfig,
	random io.Reader,
	now func() time.Time,
) (*MutationSecurity, error) {
	if origin.Canonical == nil || origin.Origin() == "" {
		return nil, errors.New("new mutation security: canonical origin is required")
	}
	normalized, err := NormalizeBasePath(origin.BasePath)
	if err != nil || normalized != origin.BasePath || origin.Canonical.Path != normalized {
		return nil, errors.New("new mutation security: origin configuration is inconsistent")
	}
	if random == nil {
		random = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	security := &MutationSecurity{origin: origin, now: now}
	if _, err = io.ReadFull(random, security.secret[:]); err != nil {
		return nil, fmt.Errorf("new mutation security: read secret: %w", err)
	}
	instanceDigest := sha256.Sum256(security.secret[:])
	security.instance = base64.RawURLEncoding.EncodeToString(instanceDigest[:12])
	return security, nil
}

// Issue creates one exactly scoped signed token with a fixed one-hour lifetime.
func (security *MutationSecurity) Issue(scope string) (IssuedMutationToken, error) {
	if !validMutationScope(scope) {
		return IssuedMutationToken{}, errors.New("issue mutation token: scope is invalid")
	}
	issuedAt := security.now().UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(mutationTokenLifetime)
	claims := mutationTokenClaims{
		Version: mutationTokenVersion, Instance: security.instance, Origin: security.origin.Origin(),
		BasePath: security.origin.BasePath, Scope: scope,
		IssuedAt: issuedAt.Unix(), Expires: expiresAt.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return IssuedMutationToken{}, fmt.Errorf("issue mutation token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := security.sign(encoded)
	value := encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
	if len(value) > MaxMutationTokenBytes {
		return IssuedMutationToken{}, errors.New("issue mutation token: encoded token is oversized")
	}
	return IssuedMutationToken{Value: value, ExpiresAt: expiresAt}, nil
}

// Verify checks signature, exact scope, binding, lifetime, and future-clock allowance.
func (security *MutationSecurity) Verify(value string, scope string) error {
	if !validMutationScope(scope) {
		return ErrInvalidMutationToken
	}
	if value == "" || len(value) > MaxMutationTokenBytes || strings.Count(value, ".") != 1 {
		return ErrInvalidMutationToken
	}
	encoded, encodedSignature, _ := strings.Cut(value, ".")
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) > MaxMutationTokenBytes {
		return ErrInvalidMutationToken
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var claims mutationTokenClaims
	if err = decoder.Decode(&claims); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ErrInvalidMutationToken
	}
	if claims.Instance != security.instance {
		return ErrTokenExpired
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || len(signature) != sha256.Size || !hmac.Equal(signature, security.sign(encoded)) {
		return ErrInvalidMutationToken
	}
	if claims.Version != mutationTokenVersion || claims.Origin != security.origin.Origin() ||
		claims.BasePath != security.origin.BasePath || claims.Scope != scope ||
		claims.Expires-claims.IssuedAt != int64(mutationTokenLifetime/time.Second) {
		return ErrInvalidMutationToken
	}
	now := security.now().UTC()
	issuedAt := time.Unix(claims.IssuedAt, 0)
	expiresAt := time.Unix(claims.Expires, 0)
	if issuedAt.After(now.Add(mutationClockSkew)) {
		return ErrInvalidMutationToken
	}
	if !now.Before(expiresAt) {
		return ErrTokenExpired
	}
	return nil
}

func validMutationScope(scope string) bool {
	return scope == CatalogMutationScope || profilecatalog.ValidProfileID(scope)
}

func (security *MutationSecurity) sign(encoded string) []byte {
	digest := hmac.New(sha256.New, security.secret[:])
	_, _ = digest.Write([]byte(encoded))
	return digest.Sum(nil)
}

// Protect enforces browser mutation preconditions before calling the application handler.
func (security *MutationSecurity) Protect(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Origin") != security.origin.Origin() ||
			request.Header.Get("Sec-Fetch-Site") != "same-origin" {
			writeProblem(response, newProblem(
				http.StatusForbidden, string(CodeInvalidOrigin),
				"This request did not come from the canonical Moneyflow origin.",
			))
			return
		}
		if err := security.Verify(request.Header.Get(MutationTokenHeader), scope); err != nil {
			code := CodeInvalidToken
			detail := "The mutation token is invalid."
			if errors.Is(err, ErrTokenExpired) {
				code = CodeTokenExpired
				detail = "The mutation token expired."
			}
			writeProblem(response, newProblem(http.StatusForbidden, string(code), detail))
			return
		}
		next.ServeHTTP(response, request)
	})
}
