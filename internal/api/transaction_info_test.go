package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestTransactionInformationIsReadOnlyAndUsesExactMoneyStrings(t *testing.T) {
	t.Parallel()
	transaction := apiTransaction(t)
	transaction.Merchant.Name = "Amazon Marketplace"
	service, err := app.NewService([]domain.Transaction{transaction})
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "transaction-information")
	require.NoError(t, err)
	response := requestJSON(t, server, path, TransactionInformationBody{
		Version: TransactionInformationWireVersion, ExpectedRevision: "0", Query: "v=1",
		Target:      TransitionTarget{Kind: app.IdentityTransaction, Identity: transaction.ID},
		MatchWindow: Window{Limit: 20}, ItemWindow: Window{Limit: 20},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body TransactionInformationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "-1234", body.Transaction.Amount.Minor)
	assert.Equal(t, "-12.34", body.Transaction.Amount.Decimal)
	assert.True(t, body.AmazonQualified)
	assert.Zero(t, body.TotalMatches)
	assert.Equal(t, 20, body.MatchWindow.Limit)
	assert.Equal(t, 20, body.ItemWindow.Limit)
}

func TestTransactionInformationRejectsAggregateTargets(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, "/")
	path, err := ProfileAPIPath("/", testProfileID, "transaction-information")
	require.NoError(t, err)
	response := requestJSON(t, server, path, TransactionInformationBody{
		Version: TransactionInformationWireVersion, ExpectedRevision: "0", Query: "v=1",
		Target:      TransitionTarget{Kind: app.IdentityAggregate, Identity: "aggregate"},
		MatchWindow: Window{Limit: 20}, ItemWindow: Window{Limit: 20},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
}

func TestTransactionInformationRequiresNoMutationToken(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, "/")
	path, err := ProfileAPIPath("/", testProfileID, "transaction-information")
	require.NoError(t, err)
	request := TransactionInformationBody{
		Version: TransactionInformationWireVersion, ExpectedRevision: "0", Query: "v=1",
		Target:      TransitionTarget{Kind: app.IdentityTransaction, Identity: "txn-1"},
		MatchWindow: Window{Limit: 20}, ItemWindow: Window{Limit: 20},
	}
	response := requestJSON(t, server, path, request)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())

	get := requestServer(t, server, http.MethodGet, path, nil)
	assert.Equal(t, http.StatusMethodNotAllowed, get.Code)
}
