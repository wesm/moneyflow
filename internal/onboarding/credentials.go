package onboarding

import (
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/wesm/moneyflow/internal/provider/monarch"
)

type credentialMaterial struct {
	email           []byte
	password        []byte
	totpSecret      []byte
	accountPassword []byte
}

func newCredentialMaterial(input *CredentialInput) (*credentialMaterial, error) {
	if input == nil {
		return nil, newError(CodeCredentialInputInvalid, errors.New("credentials are absent"))
	}
	email := strings.TrimSpace(string(input.Email))
	normalizedSecret := monarch.NormalizeTOTPSecret(string(input.TOTPSecret))
	if email == "" || len(input.Password) == 0 || normalizedSecret == "" ||
		len(input.AccountPassword) == 0 ||
		len(input.AccountPassword) != len(input.Confirmation) ||
		subtle.ConstantTimeCompare(input.AccountPassword, input.Confirmation) != 1 {
		return nil, newError(CodeCredentialInputInvalid, errors.New("credentials are invalid"))
	}
	credentials := monarch.StoredCredentials{
		Email: email, Password: string(input.Password), TOTPSecret: normalizedSecret,
	}
	if err := credentials.Validate(); err != nil {
		return nil, newError(CodeCredentialInputInvalid, err)
	}
	return &credentialMaterial{
		email: []byte(email), password: append([]byte(nil), input.Password...),
		totpSecret:      []byte(normalizedSecret),
		accountPassword: append([]byte(nil), input.AccountPassword...),
	}, nil
}

func (material *credentialMaterial) clear() {
	if material == nil {
		return
	}
	clear(material.email)
	clear(material.password)
	clear(material.totpSecret)
	clear(material.accountPassword)
}

func (material *credentialMaterial) storedCredentials() monarch.StoredCredentials {
	return monarch.StoredCredentials{
		Email: string(material.email), Password: string(material.password),
		TOTPSecret: string(material.totpSecret),
	}
}
