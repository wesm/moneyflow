package main

import (
	"fmt"
	"strings"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// profileSelectionError explains a command-line --profile that names no profile or several. It
// lists profiles for the person at the terminal, who owns the catalog. It unwraps to the catalog
// error, so errors.Is and profilecatalog.CodeOf still classify it, and the API and MCP, which
// render catalog errors by code, never show the listing.
type profileSelectionError struct {
	message string
	cause   error
}

func (failure *profileSelectionError) Error() string { return failure.message }

func (failure *profileSelectionError) Unwrap() error { return failure.cause }

// resolveCommandProfile selects the profile that a --profile selector names. When the selector
// matches no profile or several, the error lists the profiles to choose from. An empty selector
// keeps the catalog's error unchanged.
func resolveCommandProfile(
	entries []profilecatalog.Entry,
	selector string,
) (profilecatalog.Entry, error) {
	entry, err := profilecatalog.ResolveEntries(entries, selector)
	if err == nil || selector == "" {
		return entry, err
	}
	var message string
	switch profilecatalog.CodeOf(err) {
	case profilecatalog.CodeProfileNotFound:
		message = fmt.Sprintf("no profile named %q. %s", selector, profileListing(entries))
	case profilecatalog.CodeProfileAmbiguous:
		message = fmt.Sprintf(
			"%q matches several profiles: %s. Pass an ID to --profile.",
			selector, describeProfiles(profilesNamed(entries, selector)),
		)
	default:
		return entry, err
	}
	return entry, &profileSelectionError{message: message, cause: err}
}

func profileListing(entries []profilecatalog.Entry) string {
	if len(entries) == 0 {
		return "No profiles exist yet."
	}
	return "Profiles: " + describeProfiles(entries)
}

func profilesNamed(entries []profilecatalog.Entry, name string) []profilecatalog.Entry {
	key := profileNameKey(name)
	matches := make([]profilecatalog.Entry, 0, 2)
	for _, entry := range entries {
		if profileNameKey(entry.DisplayName) == key {
			matches = append(matches, entry)
		}
	}
	return matches
}

// describeProfiles names each profile, adding its ID when another listed profile has the same name.
func describeProfiles(entries []profilecatalog.Entry) string {
	counts := make(map[string]int, len(entries))
	for _, entry := range entries {
		counts[profileNameKey(entry.DisplayName)]++
	}
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := entry.DisplayName
		if counts[profileNameKey(entry.DisplayName)] > 1 {
			id := entry.ID
			if id == "" {
				id = entry.Key
			}
			label += " (ID " + id + ")"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, ", ")
}

func profileNameKey(name string) string {
	_, key, err := profilecatalog.NormalizeDisplayName(name)
	if err != nil {
		return name
	}
	return key
}
