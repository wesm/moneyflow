package api

import (
	"strconv"
	"time"
)

// Bootstrap is the no-store browser-memory mutation configuration.
type Bootstrap struct {
	Version        string `json:"version"`
	CanonicalURL   string `json:"canonical_url"`
	BasePath       string `json:"base_path"`
	Revision       string `json:"revision" pattern:"^[0-9]+$"`
	MutationToken  string `json:"mutation_token"`
	TokenExpiresAt string `json:"token_expires_at" format:"date-time"`
}

// ProfileBootstrap adds the profile's currencies, most transactions first, so a browser can pick
// its default currency before the first projection.
type ProfileBootstrap struct {
	Bootstrap
	Currencies []CurrencyCount `json:"currencies" nullable:"false"`
}

func newBootstrap(
	version string,
	origin OriginConfig,
	revision uint64,
	security *MutationSecurity,
	scope string,
) (Bootstrap, error) {
	issued, err := security.Issue(scope)
	if err != nil {
		return Bootstrap{}, err
	}
	return Bootstrap{
		Version: version, CanonicalURL: origin.Canonical.String(), BasePath: origin.BasePath,
		Revision: strconv.FormatUint(revision, 10), MutationToken: issued.Value,
		TokenExpiresAt: issued.ExpiresAt.Format(time.RFC3339),
	}, nil
}
