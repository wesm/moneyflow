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

func (contractReader) FetchSnapshot(
	context.Context,
	provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	return provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: "example", RemoteID: "remote_a"},
		Snapshot: domain.ImportSnapshot{},
	}, nil
}

type contractReaderSource struct{}

func (contractReaderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return contractReader{}, "reader-generation", nil
}

func (contractReaderSource) Changed(provider.SessionFingerprint) (bool, error) {
	return false, nil
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

func (contractWriter) DeleteTransaction(
	context.Context,
	string,
) (provider.TransactionDeleteResult, error) {
	return provider.TransactionDeleteResult{TransactionExternalID: "transaction-a"}, nil
}

func TestReadContractsAreCapabilitySized(t *testing.T) {
	t.Parallel()

	var session provider.Session = contractSession{}
	var reader provider.Reader = contractReader{}
	var source provider.ReaderSource = contractReaderSource{}
	assert.Equal(t, "example", session.ProviderKind())
	result, err := reader.FetchSnapshot(context.Background(), nil)
	assert.NoError(t, err)
	assert.Equal(t, "remote_a", result.Identity.RemoteID)
	changed, err := source.Changed("reader-generation")
	assert.NoError(t, err)
	assert.False(t, changed)
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
	deleted, err := writer.DeleteTransaction(context.Background(), "transaction-a")
	assert.NoError(t, err)
	assert.Equal(t, "transaction-a", deleted.TransactionExternalID)
	assert.False(t, deleted.AlreadyAbsent)
}
