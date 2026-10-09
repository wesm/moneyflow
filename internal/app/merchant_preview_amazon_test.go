package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestCommittedViewMembersExpandSearchToAmazonProductNames(t *testing.T) {
	t.Parallel()

	source := amazonSourceState(t, 1, "USD", 2, -1234)
	source.Items[0].ProductName = "Searchable Product"
	directory := &fakeAmazonDirectory{
		sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}},
	}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": source}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service, err := NewService([]domain.Transaction{
		matchingFinanceTransaction(t, "matched", "Amazon", -1234),
		matchingFinanceTransaction(t, "other", "Corner Grocery", -4000),
	})
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
	session := NewSession()
	session.SetSearch("searchable")

	members, err := service.committedViewMembers(context.Background(), session.ViewState().Current)
	require.NoError(t, err)
	assert.Contains(t, members, "matched", "the product name matches the search")
	assert.NotContains(t, members, "other")
}
