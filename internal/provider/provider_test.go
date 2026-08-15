package provider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

type contractSession struct{}

func (contractSession) ProviderKind() string { return "example" }

type contractReader struct{}

func (contractReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "example", RemoteID: "remote_a"}, nil
}

func (contractReader) FetchSnapshot(
	context.Context,
	provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	return domain.ImportSnapshot{}, nil
}

func TestReadContractsAreCapabilitySized(t *testing.T) {
	t.Parallel()

	var session provider.Session = contractSession{}
	var reader provider.Reader = contractReader{}
	assert.Equal(t, "example", session.ProviderKind())
	identity, err := reader.ProbeIdentity(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "remote_a", identity.RemoteID)
}
