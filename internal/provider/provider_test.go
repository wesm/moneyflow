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

type contractWriter struct{}

func (contractReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "example", RemoteID: "remote_a"}, nil
}

func (contractReader) FetchSnapshot(
	context.Context,
	provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	return domain.ImportSnapshot{}, nil
}

func (contractWriter) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "example", RemoteID: "remote_a"}, nil
}

func (contractWriter) UpdateTransaction(
	context.Context,
	provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	return provider.TransactionUpdateResult{TransactionExternalID: "transaction-a"}, nil
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

func TestWriteContractsPreserveOptionalFieldPresence(t *testing.T) {
	t.Parallel()

	var writer provider.Writer = contractWriter{}
	update := provider.TransactionUpdate{
		TransactionExternalID: "transaction-a",
		MerchantName:          provider.Some("Example Merchant"),
		CategoryExternalID:    provider.Optional[string]{},
		Hidden:                provider.Some(false),
	}
	result, err := writer.UpdateTransaction(context.Background(), update)
	assert.NoError(t, err)
	assert.Equal(t, "transaction-a", result.TransactionExternalID)
	assert.True(t, update.MerchantName.Present)
	assert.Equal(t, "Example Merchant", update.MerchantName.Value)
	assert.False(t, update.CategoryExternalID.Present)
	assert.True(t, update.Hidden.Present)
	assert.False(t, update.Hidden.Value)
}
