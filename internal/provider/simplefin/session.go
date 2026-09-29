package simplefin

import "errors"

// Session is the provider-owned credential and interpretation, stored outside SQLite.
type Session struct {
	Version   int          `json:"version"`
	AccessURL string       `json:"access_url"`
	Import    ImportConfig `json:"import"`
}

// ProviderKind identifies the adapter without exposing the credential.
func (Session) ProviderKind() string { return providerKind }

// String redacts the Access URL from diagnostic formatting.
func (Session) String() string { return "SimpleFIN session" }

// GoString redacts the Access URL from Go-syntax formatting.
func (Session) GoString() string { return "simplefin.Session{AccessURL:<redacted>}" }

// Validate rejects incompatible versions and invalid credential/settings values.
func (session Session) Validate() error {
	if session.Version != 1 {
		return errors.New("unsupported SimpleFIN session version")
	}
	if _, err := canonicalAccessURL(session.AccessURL); err != nil {
		return err
	}
	return session.Import.Validate()
}
