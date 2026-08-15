package monarch

import (
	"errors"
	"strings"
	"time"
)

const (
	providerKind   = "monarch"
	sessionVersion = uint16(1)
)

// Session is the complete provider-owned durable session format. It never contains credentials.
type Session struct {
	Version         uint16    `json:"version"`
	Token           string    `json:"token"`
	DeviceUUID      string    `json:"device_uuid"`
	RemoteProfileID string    `json:"remote_profile_id"`
	IssuedAt        time.Time `json:"issued_at"`
	ValidatedAt     time.Time `json:"validated_at"`
}

// ProviderKind implements provider.Session.
func (Session) ProviderKind() string { return providerKind }

// Validate checks the provider-owned session format without exposing its values.
func (session Session) Validate() error {
	if session.Version != sessionVersion {
		return errors.New("validate monarch session: unsupported version")
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
