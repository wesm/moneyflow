package profilecatalog

import (
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	profileIDPrefix = "profile_"
	profileIDBytes  = 16
	profileIDLength = len(profileIDPrefix) + 26
)

var profileIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewProfileID returns one opaque 128-bit catalog identity.
func NewProfileID(random io.Reader) (string, error) {
	if random == nil {
		return "", errors.New("new profile ID: random source is nil")
	}
	buffer := make([]byte, profileIDBytes)
	if _, err := io.ReadFull(random, buffer); err != nil {
		return "", fmt.Errorf("new profile ID: read randomness: %w", err)
	}
	return profileIDPrefix + profileIDEncoding.EncodeToString(buffer), nil
}

// ValidProfileID reports whether a profile ID is in its one canonical form.
func ValidProfileID(value string) bool {
	if len(value) != profileIDLength || !strings.HasPrefix(value, profileIDPrefix) {
		return false
	}
	for _, character := range value[len(profileIDPrefix):] {
		if (character < 'a' || character > 'z') && (character < '2' || character > '7') {
			return false
		}
	}
	encoded := value[len(profileIDPrefix):]
	decoded, err := profileIDEncoding.DecodeString(encoded)
	return err == nil && len(decoded) == profileIDBytes &&
		profileIDEncoding.EncodeToString(decoded) == encoded
}
