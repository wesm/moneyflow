package api

import (
	"errors"
	"strings"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// CatalogMutationScope binds catalog-wide browser mutations to their bootstrap.
const CatalogMutationScope = "catalog"

// ProfileAPIPath builds one canonical profile-scoped API path.
func ProfileAPIPath(basePath string, profileID string, endpoint string) (string, error) {
	normalized, err := NormalizeBasePath(basePath)
	if err != nil {
		return "", err
	}
	if !profilecatalog.ValidProfileID(profileID) {
		return "", errors.New("profile API path: profile ID is invalid")
	}
	if !validProfileEndpoint(endpoint) {
		return "", errors.New("profile API path: endpoint is invalid")
	}
	return normalized + "api/v1/profiles/" + profileID + "/" + endpoint, nil
}

// ParseProfileAPIPath extracts a canonical profile ID and bounded relative endpoint.
// The input is an escaped request path; percent encoding is rejected rather than normalized.
func ParseProfileAPIPath(basePath string, escapedPath string) (string, string, error) {
	normalized, err := NormalizeBasePath(basePath)
	if err != nil {
		return "", "", err
	}
	if strings.ContainsAny(escapedPath, "%?#\\") {
		return "", "", errors.New("profile API path: encoded path is invalid")
	}
	prefix := normalized + "api/v1/profiles/"
	if !strings.HasPrefix(escapedPath, prefix) {
		return "", "", errors.New("profile API path: prefix is invalid")
	}
	remainder := strings.TrimPrefix(escapedPath, prefix)
	profileID, endpoint, found := strings.Cut(remainder, "/")
	if !found || !profilecatalog.ValidProfileID(profileID) || !validProfileEndpoint(endpoint) {
		return "", "", errors.New("profile API path: route is invalid")
	}
	return profileID, endpoint, nil
}

func validProfileEndpoint(endpoint string) bool {
	if endpoint == "" || strings.HasPrefix(endpoint, "/") || strings.HasSuffix(endpoint, "/") ||
		strings.ContainsAny(endpoint, "%?#\\") {
		return false
	}
	for segment := range strings.SplitSeq(endpoint, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
