package domain

import (
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const randomIDBytes = 16

var unpaddedLowerBase32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NormalizeDisplayLabel validates a user-facing label and trims surrounding Unicode whitespace.
func NormalizeDisplayLabel(label string) (string, error) {
	if !utf8.ValidString(label) {
		return "", errors.New("normalize display label: invalid UTF-8")
	}
	for _, character := range label {
		if unicode.IsControl(character) {
			return "", errors.New("normalize display label: control characters are forbidden")
		}
	}
	label = strings.TrimFunc(label, unicode.IsSpace)
	if label == "" {
		return "", errors.New("normalize display label: label is empty")
	}
	return label, nil
}

// ValidateDisplayLabel reports whether a display label satisfies the profile contract.
func ValidateDisplayLabel(label string) error {
	_, err := NormalizeDisplayLabel(label)
	return err
}

// CollisionKey returns the platform-independent comparison key for an entity label.
func CollisionKey(label string) (string, error) {
	label, err := NormalizeDisplayLabel(label)
	if err != nil {
		return "", err
	}
	label = cases.Fold().String(norm.NFKC.String(label))
	fields := strings.FieldsFunc(label, unicode.IsSpace)
	if len(fields) == 0 {
		return "", errors.New("collision key: label is empty")
	}
	return strings.Join(fields, " "), nil
}

func randomID(prefix string, random io.Reader) (string, error) {
	if random == nil {
		return "", errors.New("new ID: random source is nil")
	}
	buffer := make([]byte, randomIDBytes)
	if _, err := io.ReadFull(random, buffer); err != nil {
		return "", fmt.Errorf("new ID: read randomness: %w", err)
	}
	return prefix + unpaddedLowerBase32.EncodeToString(buffer), nil
}
