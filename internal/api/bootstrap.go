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

func newBootstrap(
	version string,
	origin OriginConfig,
	revision uint64,
	security *MutationSecurity,
) (Bootstrap, error) {
	issued, err := security.Issue()
	if err != nil {
		return Bootstrap{}, err
	}
	return Bootstrap{
		Version: version, CanonicalURL: origin.Canonical.String(), BasePath: origin.BasePath,
		Revision: strconv.FormatUint(revision, 10), MutationToken: issued.Value,
		TokenExpiresAt: issued.ExpiresAt.Format(time.RFC3339),
	}, nil
}
