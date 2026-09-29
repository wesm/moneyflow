package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestClientRequiresExplicitZeroScale(t *testing.T) {
	for _, digits := range []string{"", `,"decimal_digits":null`, `,"decimal_digits":0`} {
		t.Run(digits, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"data":{"server_knowledge":1,"plan":{"id":"plan-a","currency_format":{"iso_code":"JPY"%s},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]}}}`, digits)
			}))
			defer server.Close()
			base, err := url.Parse(server.URL + "/")
			require.NoError(t, err)
			client, err := NewClient(ClientOptions{BaseURL: base}, "synthetic-token")
			require.NoError(t, err)
			plan, err := client.FetchPlan(t.Context(), "plan-a")
			if digits == `,"decimal_digits":0` {
				require.NoError(t, err)
				_, err = Normalize(plan, time.Now())
				require.NoError(t, err)
			} else {
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.CodeDataInvalid, code)
			}
		})
	}
}

func TestClientRequiresExplicitTransactionAndSplitAmounts(t *testing.T) {
	for _, location := range []string{"plan parent", "plan split", "detail parent", "detail split"} {
		for _, amount := range []string{"", `,"amount":null`, `,"amount":0`} {
			t.Run(location+"/"+amount, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					writer.Header().Set("Content-Type", "application/json")
					part := "plan"
					if strings.HasSuffix(request.URL.Path, "/transactions") {
						part = "detail"
					}
					parentAmount, splitAmount := `,"amount":0`, `,"amount":0`
					if location == part+" parent" {
						parentAmount = amount
					}
					if location == part+" split" {
						splitAmount = amount
					}
					parent := `{"id":"txn-a","date":"2026-08-30","cleared":"cleared","approved":true,"account_id":"account-a","deleted":false` + parentAmount
					split := `{"id":"split-a","transaction_id":"txn-a","deleted":false` + splitAmount + `}`
					if part == "detail" {
						_, _ = fmt.Fprintf(writer, `{"data":{"server_knowledge":1,"transactions":[%s,"subtransactions":[%s]}]}}`, parent, split)
						return
					}
					_, _ = fmt.Fprintf(writer, `{"data":{"server_knowledge":1,"plan":{
						"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},
						"accounts":[{"id":"account-a","name":"Account Name","type":"checking","on_budget":true,"closed":false,"deleted":false}],
						"payees":[],"categories":[],"category_groups":[],"transactions":[%s}],"subtransactions":[%s]}}}`, parent, split)
				}))
				defer server.Close()
				base, err := url.Parse(server.URL + "/v1/")
				require.NoError(t, err)
				client, err := NewClient(ClientOptions{BaseURL: base}, "synthetic-token")
				require.NoError(t, err)
				plan, err := client.FetchPlan(context.Background(), "plan-a")
				if amount != `,"amount":0` {
					code, ok := provider.CodeOf(err)
					require.True(t, ok, "an absent or null amount must not become zero")
					assert.Equal(t, provider.CodeDataInvalid, code)
					assert.Empty(t, plan.Transactions)
					return
				}
				require.NoError(t, err)
				snapshot, err := Normalize(plan, time.Now())
				require.NoError(t, err)
				require.Len(t, snapshot.Transactions, 1)
				assert.Zero(t, snapshot.Transactions[0].Amount.Minor, "explicit zero is valid money")
			})
		}
	}
}

func TestClientImportsCategoriesFromCoherentTransactionDetails(t *testing.T) {
	for _, scenario := range []string{"complete", "changed generation", "changed transaction", "missing transaction", "duplicate transaction", "missing split", "changed split", "conflicting labels"} {
		t.Run(scenario, func(t *testing.T) {
			plan := syntheticPlan()
			plan.Transactions[0].CategoryID = "category-historical"
			plan.Subtransactions[0].CategoryID = "category-historical"
			encoded, err := json.Marshal(plan.Transactions)
			require.NoError(t, err)
			var details []map[string]any
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&details))
			for index, transaction := range plan.Transactions {
				details[index]["subtransactions"] = []any{}
				if transaction.CategoryID == "category-historical" {
					details[index]["category_name"] = "Historical Category"
				}
				for _, split := range plan.Subtransactions {
					if split.TransactionID != transaction.ID {
						continue
					}
					encoded, err = json.Marshal(split)
					require.NoError(t, err)
					var child map[string]any
					decoder = json.NewDecoder(bytes.NewReader(encoded))
					decoder.UseNumber()
					require.NoError(t, decoder.Decode(&child))
					delete(child, "transaction_id")
					if split.CategoryID == "category-historical" {
						child["category_name"] = "Historical Category"
						if scenario == "conflicting labels" {
							child["category_name"] = "Other Category"
						}
					}
					details[index]["subtransactions"] = append(details[index]["subtransactions"].([]any), child)
				}
			}
			knowledge := plan.ServerKnowledge
			switch scenario {
			case "changed generation":
				knowledge++
			case "changed transaction":
				details[0]["amount"] = -99900
			case "missing transaction":
				details = details[1:]
			case "duplicate transaction":
				details = append(details, details[0])
			case "missing split":
				details[2]["subtransactions"] = []any{}
			case "changed split":
				details[2]["subtransactions"].([]any)[0].(map[string]any)["amount"] = -99900
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/v1/plans/plan-a/transactions" {
					_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"transactions": details, "server_knowledge": knowledge}})
					return
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{
					"server_knowledge": plan.ServerKnowledge,
					"plan": map[string]any{"id": plan.ID, "name": plan.Name, "currency_format": plan.CurrencyFormat,
						"accounts": plan.Accounts, "payees": plan.Payees, "category_groups": plan.CategoryGroups,
						"categories": plan.Categories, "transactions": plan.Transactions, "subtransactions": plan.Subtransactions},
				}})
			}))
			defer server.Close()
			base, err := url.Parse(server.URL + "/v1/")
			require.NoError(t, err)
			client, err := NewClient(ClientOptions{BaseURL: base}, "synthetic-token")
			require.NoError(t, err)
			fetched, err := client.FetchPlan(context.Background(), plan.ID)
			if scenario != "complete" && scenario != "conflicting labels" {
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.CodeSnapshotUnstable, code)
				return
			}
			require.NoError(t, err)
			snapshot, err := Normalize(fetched, time.Now())
			if scenario == "conflicting labels" {
				require.Error(t, err)
				assert.Empty(t, snapshot.Transactions)
				return
			}
			require.NoError(t, err)
			assert.Len(t, snapshot.Transactions, 3)
			assert.Equal(t, "category-historical", transactionByID(snapshot, "txn-uncleared").CategoryExternalID)
			assert.Equal(t, int64(-1234), transactionByID(snapshot, "txn-uncleared").Amount.Minor)
			assert.Contains(t, snapshot.Categories, domain.ImportEntity{Kind: domain.EntityKindCategory,
				ExternalID: "category-historical", Label: "Historical Category"})
			assert.Equal(t, "Historical Category", snapshot.Splits[1].CategoryLabel)
		})
	}
}

func TestClientListsAndFetchesPlansWithBearerAuthentication(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(context.Background())
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/plans" {
			_, _ = writer.Write([]byte(`{"data":{"plans":[{"id":"plan-a","name":"Example Budget","last_modified_on":"2026-08-30T00:00:00Z"}]}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"data":{"plan":{"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]},"server_knowledge":1}}`))
	}))
	defer server.Close()
	base, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	client, err := NewClient(ClientOptions{
		HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: base, MaxBodyBytes: 1 << 20,
	}, "synthetic-token")
	require.NoError(t, err)
	plans, err := client.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "plan-a", plans[0].ID)
	plan, err := client.FetchPlan(context.Background(), "plan-a")
	require.NoError(t, err)
	assert.Equal(t, "plan-a", plan.ID)
	for range 2 {
		request := <-requests
		assert.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
	}
}

func TestClientTranslatesBoundedHTTPFailuresWithoutBodies(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]provider.ErrorCode{
		http.StatusUnauthorized:        provider.CodeReconnectRequired,
		http.StatusNotFound:            provider.CodeIdentityMismatch,
		http.StatusTooManyRequests:     provider.CodeRateLimited,
		http.StatusInternalServerError: provider.CodeUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte("private provider response"))
			}))
			defer server.Close()
			base, err := url.Parse(server.URL + "/v1/")
			require.NoError(t, err)
			client, err := NewClient(ClientOptions{
				HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: base,
			}, "synthetic-token")
			require.NoError(t, err)
			_, err = client.FetchPlan(context.Background(), "plan-a")
			code, ok := provider.CodeOf(err)
			assert.True(t, ok)
			assert.Equal(t, want, code)
			assert.NotContains(t, err.Error(), "private provider response")
		})
	}
}
