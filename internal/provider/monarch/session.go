package monarch

import (
	"errors"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

const (
	providerKind   = "monarch"
	sessionVersion = uint16(2)
)

// ImportConfig is the explicit exact-money interpretation persisted with a session.
type ImportConfig struct {
	Currency domain.Currency `json:"currency"`
	Scale    uint8           `json:"scale"`
}

// Validate rejects implicit, malformed, or unsupported money interpretation.
func (config ImportConfig) Validate() error {
	if !validCurrency(config.Currency) || config.Scale > 9 {
		return errors.New("validate monarch import configuration: currency or scale is invalid")
	}
	return nil
}

// Session is the complete provider-owned durable session format. It never contains credentials.
type Session struct {
	Version         uint16       `json:"version"`
	Token           string       `json:"token"`
	DeviceUUID      string       `json:"device_uuid"`
	RemoteProfileID string       `json:"remote_profile_id"`
	Import          ImportConfig `json:"import"`
	IssuedAt        time.Time    `json:"issued_at"`
	ValidatedAt     time.Time    `json:"validated_at"`
}

// ProviderKind implements provider.Session.
func (Session) ProviderKind() string { return providerKind }

// Validate checks the provider-owned session format without exposing its values.
func (session Session) Validate() error {
	if session.Version != sessionVersion {
		return errors.New("validate monarch session: unsupported version")
	}
	if err := session.Import.Validate(); err != nil {
		return err
	}
	for _, value := range []string{session.Token, session.DeviceUUID, session.RemoteProfileID} {
		if value == "" || strings.TrimSpace(value) != value {
			return errors.New("validate monarch session: required value is invalid")
		}
	}
	if session.IssuedAt.IsZero() || session.ValidatedAt.IsZero() ||
		session.ValidatedAt.Before(session.IssuedAt) {
		return errors.New("validate monarch session: timestamps are invalid")
	}
	return nil
}
