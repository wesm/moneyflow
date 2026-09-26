package monarch

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/pquerna/otp/totp"
)

// NormalizeTOTPSecret accepts the grouped Base32 form shown during Monarch setup.
func NormalizeTOTPSecret(secret string) string {
	return strings.ToUpper(strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, secret))
}

// GenerateTOTPCode creates the six-digit RFC 6238 code used by Monarch login.
func GenerateTOTPCode(secret string, at time.Time) (string, error) {
	normalized := NormalizeTOTPSecret(secret)
	if normalized == "" {
		return "", errors.New("generate Monarch verification code: TOTP secret is empty")
	}
	code, err := totp.GenerateCode(normalized, at)
	if err != nil {
		return "", errors.New("generate Monarch verification code: TOTP secret is invalid")
	}
	return code, nil
}
