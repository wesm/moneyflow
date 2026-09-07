package ynab

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

const ynabPerformanceRows = 100_000

func TestYNABDecodeAndNormalize100KPerformance(t *testing.T) {
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance gate disabled")
	}
	contents := syntheticYNABResponseJSON(t, ynabPerformanceRows)
	started := time.Now()
	result := decodeAndNormalizeYNABResponse(t, contents)
	duration := time.Since(started)
	t.Logf("YNAB 100k JSON decode and normalization: %s", duration)
	require.Len(t, result.Transactions, ynabPerformanceRows)
	require.Less(t, duration, 4*time.Second)
}

func BenchmarkYNABDecodeAndNormalize100K(b *testing.B) {
	contents := syntheticYNABResponseJSON(b, ynabPerformanceRows)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result := decodeAndNormalizeYNABResponse(b, contents)
		if len(result.Transactions) != ynabPerformanceRows {
			b.Fatalf("normalized %d transactions", len(result.Transactions))
		}
	}
}

func syntheticYNABResponseJSON(t testing.TB, count int) []byte {
	t.Helper()
	no, yes := false, true
	transactions := make([]Transaction, count)
	subtransactions := make([]Subtransaction, 0, count/5)
	for index := range count {
		transactionID := fmt.Sprintf("transaction-%06d", index)
		transactions[index] = Transaction{
			ID: transactionID, Date: "2026-08-30", Amount: new(int64(-1000 - index*10)),
			Cleared: "cleared", Approved: &yes, AccountID: "account-example",
			PayeeID: "payee-example", CategoryID: "category-example", Deleted: &no,
		}
		if index%10 == 0 {
			transactions[index].Amount = new(int64(-3000))
			transactions[index].CategoryID = ""
			subtransactions = append(subtransactions,
				Subtransaction{
					ID: fmt.Sprintf("split-%06d-a", index), TransactionID: transactionID,
					Amount: new(int64(-1000)), PayeeID: "payee-example", Deleted: &no,
				},
				Subtransaction{
					ID: fmt.Sprintf("split-%06d-b", index), TransactionID: transactionID,
					Amount: new(int64(-2000)), CategoryID: "category-example", Deleted: &no,
				},
			)
		}
	}
	var response planResponse
	response.Data.Plan.ID = "plan-performance"
	response.Data.Plan.Name = "Example Budget"
	response.Data.Plan.CurrencyFormat = CurrencyFormat{ISOCode: "USD", DecimalDigits: 2}
	accounts := []Account{{
		ID: "account-example", Name: "Account Name", Type: "checking",
		OnBudget: &yes, Closed: &no, Deleted: &no,
	}}
	payees := []Payee{{ID: "payee-example", Name: "Example Payee", Deleted: &no}}
	groups := []CategoryGroup{{
		ID: "group-example", Name: "Example Group", Hidden: &no, Deleted: &no,
	}}
	categories := []Category{{
		ID: "category-example", CategoryGroupID: "group-example",
		Name: "Example Category", Hidden: &no, Deleted: &no,
	}}
	response.Data.Plan.Accounts = &accounts
	response.Data.Plan.Payees = &payees
	response.Data.Plan.CategoryGroups = &groups
	response.Data.Plan.Categories = &categories
	response.Data.Plan.Transactions = &transactions
	response.Data.Plan.Subtransactions = &subtransactions
	serverKnowledge := int64(1)
	response.Data.ServerKnowledge = &serverKnowledge
	contents, err := json.Marshal(response)
	require.NoError(t, err)
	return contents
}

func decodeAndNormalizeYNABResponse(t testing.TB, contents []byte) domain.ImportSnapshot {
	t.Helper()
	var response planResponse
	require.NoError(t, decodeJSON(contents, &response))
	require.NotNil(t, response.Data.Plan.Accounts)
	require.NotNil(t, response.Data.Plan.Payees)
	require.NotNil(t, response.Data.Plan.CategoryGroups)
	require.NotNil(t, response.Data.Plan.Categories)
	require.NotNil(t, response.Data.Plan.Transactions)
	require.NotNil(t, response.Data.Plan.Subtransactions)
	plan := PlanDocument{
		ID: response.Data.Plan.ID, Name: response.Data.Plan.Name,
		CurrencyFormat:  response.Data.Plan.CurrencyFormat,
		Accounts:        append([]Account(nil), (*response.Data.Plan.Accounts)...),
		Payees:          append([]Payee(nil), (*response.Data.Plan.Payees)...),
		CategoryGroups:  append([]CategoryGroup(nil), (*response.Data.Plan.CategoryGroups)...),
		Categories:      append([]Category(nil), (*response.Data.Plan.Categories)...),
		Transactions:    append([]Transaction(nil), (*response.Data.Plan.Transactions)...),
		Subtransactions: append([]Subtransaction(nil), (*response.Data.Plan.Subtransactions)...),
		ServerKnowledge: *response.Data.ServerKnowledge,
	}
	snapshot, err := Normalize(plan, time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return snapshot
}
