package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/provider"
)

func newTestTransactionWriter(t *testing.T, handler http.HandlerFunc) *transactionWriter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	client, err := NewClient(ClientOptions{HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: endpoint}, "synthetic-token")
	require.NoError(t, err)
	return &transactionWriter{client: client, planID: "plan-a", currency: "USD", scale: 2, now: func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) }, validate: func() error { return nil }}
}

func writeTestTransaction() map[string]any {
	return map[string]any{"id": "txn-a", "date": "2026-09-01", "amount": int64(-12340), "memo": "Example memo", "approved": true, "cleared": "reconciled", "flag_color": "blue", "account_id": "account-a", "payee_id": "payee-a", "payee_name": "Example Payee", "category_id": "category-a", "import_id": "import-a", "transfer_account_id": nil, "transfer_transaction_id": nil, "deleted": false, "subtransactions": []any{}}
}

func writeTestResponse(t *testing.T, response http.ResponseWriter, transaction map[string]any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"data": map[string]any{"transaction": transaction}}))
}

func TestYNABUpdateSendsOnlyRequestedFieldsAndFreshApproval(t *testing.T) {
	for _, test := range []struct {
		name   string
		update provider.TransactionUpdate
		patch  map[string]any
	}{
		{"mapped payee", provider.TransactionUpdate{MerchantExternalID: provider.Some("payee-b")}, map[string]any{"payee_id": "payee-b", "approved": true}},
		{"new payee", provider.TransactionUpdate{MerchantName: provider.Some("New Payee")}, map[string]any{"payee_id": nil, "payee_name": "New Payee", "approved": true}},
		{"category", provider.TransactionUpdate{CategoryExternalID: provider.Some("category-b")}, map[string]any{"category_id": "category-b", "approved": true}},
		{"clear", provider.TransactionUpdate{ClearCategory: true}, map[string]any{"category_id": nil, "approved": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var methods []string
			adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
				methods = append(methods, request.Method)
				if request.URL.Path == "/v1/plans/plan-a/payees/payee-b" {
					response.Header().Set("Content-Type", "application/json")
					_, _ = response.Write([]byte(`{"data":{"payee":{"id":"payee-b","name":"Example Payee","deleted":false,"transfer_account_id":null}}}`))
					return
				}
				assert.Equal(t, "/v1/plans/plan-a/transactions/txn-a", request.URL.Path)
				assert.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
				transaction := writeTestTransaction()
				if request.Method == http.MethodPut {
					var body struct {
						Transaction map[string]any `json:"transaction"`
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
					assert.Equal(t, test.patch, body.Transaction)
					for key, value := range body.Transaction {
						transaction[key] = value
					}
					if test.update.MerchantName.Present {
						transaction["payee_id"] = "payee-new"
					}
				}
				writeTestResponse(t, response, transaction)
			})
			update := test.update
			update.TransactionExternalID = "txn-a"
			result, err := adapter.UpdateTransaction(context.Background(), update)
			require.NoError(t, err)
			if test.update.MerchantExternalID.Present {
				assert.Equal(t, []string{"GET", "GET", "PUT"}, methods)
			} else {
				assert.Equal(t, []string{"GET", "PUT"}, methods)
			}
			assert.Equal(t, test.update.ClearCategory, result.CategoryCleared)
		})
	}
}

func TestYNABDestinationPayeePreflight(t *testing.T) {
	for _, scenario := range []string{"ordinary", "transfer", "deleted", "missing", "wrong identity", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			var paths []string
			writes := 0
			adapter := newTestTransactionWriter(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/plans/plan-a/payees/payee-b" {
					if scenario == "missing" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					payee := map[string]any{"id": "payee-b", "name": "Example Payee", "deleted": scenario == "deleted", "transfer_account_id": nil}
					if scenario == "transfer" {
						payee["transfer_account_id"] = "account-b"
					}
					if scenario == "wrong identity" {
						payee["id"] = "payee-other"
					}
					if scenario == "malformed" {
						delete(payee, "deleted")
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"payee": payee}}))
					return
				}
				transaction := writeTestTransaction()
				if r.Method == http.MethodPut {
					writes++
					transaction["payee_id"] = "payee-b"
				}
				writeTestResponse(t, w, transaction)
			})
			_, err := adapter.UpdateTransaction(t.Context(), provider.TransactionUpdate{TransactionExternalID: "txn-a", MerchantExternalID: provider.Some("payee-b")})
			if scenario == "ordinary" {
				require.NoError(t, err)
				assert.Equal(t, 1, writes)
				assert.Equal(t, []string{"/v1/plans/plan-a/transactions/txn-a", "/v1/plans/plan-a/payees/payee-b", "/v1/plans/plan-a/transactions/txn-a"}, paths)
			} else {
				require.Error(t, err)
				assert.Zero(t, writes)
			}
		})
	}
}

func TestYNABUpdatePreservationAndExplicitResultCategory(t *testing.T) {
	for _, field := range []string{"amount", "date", "memo", "account_id", "approved", "cleared", "flag_color", "import_id", "missing category", "missing amount", "null amount", "missing approval", "wrong identity"} {
		t.Run(field, func(t *testing.T) {
			adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
				transaction := writeTestTransaction()
				if request.Method == http.MethodPut {
					switch field {
					case "amount":
						transaction[field] = int64(-50000)
					case "approved":
						transaction[field] = false
					case "missing category":
						delete(transaction, "category_id")
					case "missing amount":
						delete(transaction, "amount")
					case "null amount":
						transaction["amount"] = nil
					case "missing approval":
						delete(transaction, "approved")
					case "wrong identity":
						transaction["id"] = "different-transaction"
					default:
						transaction[field] = "changed"
					}
				}
				writeTestResponse(t, response, transaction)
			})
			_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
			reason, ok := provider.WriteFailureReasonOf(err)
			require.True(t, ok)
			assert.Contains(t, []provider.WriteFailureReason{provider.WriteOutcomeUnknown, provider.WriteIdentityConflict}, reason)
		})
	}
}

func TestYNABUpdateSplitsCompareByIdentityAndKeepSplitNull(t *testing.T) {
	for _, changed := range []bool{false, true} {
		adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/v1/plans/plan-a/payees/payee-b" {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":{"payee":{"id":"payee-b","name":"Example Payee","deleted":false,"transfer_account_id":null}}}`))
				return
			}
			transaction := writeTestTransaction()
			transaction["amount"], transaction["category_id"] = int64(-30000), nil
			children := []any{map[string]any{"id": "split-a", "transaction_id": "txn-a", "amount": int64(-10000), "deleted": false, "memo": nil}, map[string]any{"id": "split-b", "transaction_id": "txn-a", "amount": int64(-20000), "deleted": false, "memo": nil}}
			if request.Method == http.MethodPut {
				slices.Reverse(children)
				transaction["payee_id"] = "payee-b"
				if changed {
					children[0].(map[string]any)["memo"] = "changed"
				}
			}
			transaction["subtransactions"] = children
			writeTestResponse(t, response, transaction)
		})
		result, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", MerchantExternalID: provider.Some("payee-b")})
		if changed {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.False(t, result.CategoryCleared)
		}
	}
}

func TestYNABWriteFailuresDistinguishDispatchAndNeverRetryInsideAdapter(t *testing.T) {
	for _, phase := range []string{"preflight", "mutation"} {
		for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusBadRequest} {
			t.Run(phase+http.StatusText(status), func(t *testing.T) {
				calls := 0
				adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
					calls++
					if phase == "preflight" || request.Method == http.MethodPut {
						response.WriteHeader(status)
						_, _ = response.Write([]byte("private provider payload"))
						return
					}
					writeTestResponse(t, response, writeTestTransaction())
				})
				_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private provider payload")
				if phase == "preflight" {
					assert.Equal(t, 1, calls)
				} else {
					assert.Equal(t, 2, calls)
				}
				if status == http.StatusServiceUnavailable {
					if phase == "preflight" {
						code, _ := provider.CodeOf(err)
						assert.Equal(t, provider.CodeUnavailable, code)
					} else {
						reason, _ := provider.WriteFailureReasonOf(err)
						assert.Equal(t, provider.WriteOutcomeUnknown, reason)
					}
				}
				if status == http.StatusTooManyRequests {
					delay, ok := provider.RetryAfterOf(err)
					require.True(t, ok)
					assert.Equal(t, time.Hour, delay)
				}
			})
		}
	}
}

func TestYNABDeleteAlreadyAbsentRequiresAccessibleBoundPlan(t *testing.T) {
	for _, accessible := range []bool{false, true} {
		var methods []string
		adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
			methods = append(methods, request.Method)
			if request.URL.Path == "/v1/plans/plan-a" && accessible {
				_, _ = response.Write([]byte(`{"data":{"plan":{"id":"plan-a","currency_format":{"iso_code":"USD","decimal_digits":2}}}}`))
				return
			}
			response.WriteHeader(http.StatusNotFound)
		})
		result, err := adapter.DeleteTransaction(context.Background(), "txn-a")
		if accessible {
			require.NoError(t, err)
			assert.True(t, result.AlreadyAbsent)
		} else {
			code, _ := provider.CodeOf(err)
			assert.Equal(t, provider.CodeIdentityMismatch, code)
		}
		assert.Equal(t, []string{"GET", "GET"}, methods)
	}
}

func TestYNABWriteRetryAfter(t *testing.T) {
	for _, test := range []struct {
		header string
		delay  time.Duration
	}{
		{"", time.Hour}, {"0", time.Hour}, {"-1", time.Hour}, {"garbage", time.Hour}, {"90", 90 * time.Second}, {"999999999999999999999", provider.MaxRetryAfter}, {"Mon, 07 Sep 2026 12:03:00 GMT", 3 * time.Minute}, {"Mon, 07 Sep 2026 11:59:00 GMT", time.Hour},
	} {
		adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Retry-After", test.header)
			response.WriteHeader(http.StatusTooManyRequests)
		})
		_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
		delay, ok := provider.RetryAfterOf(err)
		require.True(t, ok)
		assert.Equal(t, test.delay, delay, test.header)
	}
}

func TestYNABPreflightStopsUnsupportedTargetsWithoutSending(t *testing.T) {
	for _, field := range []string{"transfer", "transfer child", "split category", "missing amount", "null amount", "missing approval", "missing splits"} {
		t.Run(field, func(t *testing.T) {
			calls := 0
			adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, request.Method)
				transaction := writeTestTransaction()
				switch field {
				case "transfer":
					transaction["transfer_account_id"] = "account-b"
				case "transfer child", "split category":
					child := map[string]any{"id": "split-a", "transaction_id": "txn-a", "amount": int64(-12340), "deleted": false}
					if field == "transfer child" {
						child["transfer_account_id"] = "account-b"
					}
					transaction["subtransactions"] = []any{child}
				case "missing amount":
					delete(transaction, "amount")
				case "null amount":
					transaction["amount"] = nil
				case "missing approval":
					delete(transaction, "approved")
				case "missing splits":
					delete(transaction, "subtransactions")
				}
				writeTestResponse(t, response, transaction)
			})
			_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
			require.Error(t, err)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestYNABUpdatePreservesZeroAndFalseAndAcceptsOrdinaryOverrides(t *testing.T) {
	adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
		transaction := writeTestTransaction()
		transaction["amount"], transaction["approved"] = int64(0), false
		if request.Method == http.MethodPut {
			var body struct {
				Transaction map[string]any `json:"transaction"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, false, body.Transaction["approved"])
			transaction["category_id"], transaction["payee_id"] = "category-override", "payee-override"
		}
		writeTestResponse(t, response, transaction)
	})
	result, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
	require.NoError(t, err)
	assert.Equal(t, provider.Some("category-override"), result.CategoryExternalID)
	assert.Equal(t, provider.Some("payee-override"), result.MerchantExternalID)
	assert.False(t, result.CategoryCleared)
}

func TestYNABDeletePreflightsAndValidatesTheWholeParent(t *testing.T) {
	for _, outcome := range []string{"deleted", "not found", "transfer", "malformed", "not deleted"} {
		t.Run(outcome, func(t *testing.T) {
			var methods []string
			adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
				methods = append(methods, request.Method)
				if request.URL.Path == "/v1/plans/plan-a" {
					_, _ = response.Write([]byte(`{"data":{"plan":{"id":"plan-a","currency_format":{"iso_code":"USD","decimal_digits":2}}}}`))
					return
				}
				transaction := writeTestTransaction()
				if outcome == "transfer" {
					transaction["transfer_account_id"] = "account-b"
				}
				if request.Method == http.MethodDelete {
					switch outcome {
					case "not found":
						response.WriteHeader(http.StatusNotFound)
						return
					case "malformed":
						_, _ = response.Write([]byte("{"))
						return
					case "deleted":
						transaction["deleted"] = true
					}
				}
				writeTestResponse(t, response, transaction)
			})
			result, err := adapter.DeleteTransaction(context.Background(), "txn-a")
			switch outcome {
			case "deleted":
				require.NoError(t, err)
				assert.Equal(t, []string{"GET", "DELETE"}, methods)
			case "not found":
				require.NoError(t, err)
				assert.True(t, result.AlreadyAbsent)
				assert.Equal(t, []string{"GET", "DELETE", "GET"}, methods)
			case "transfer":
				require.Error(t, err)
				assert.Equal(t, []string{"GET"}, methods)
			default:
				reason, ok := provider.WriteFailureReasonOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.WriteOutcomeUnknown, reason)
			}
		})
	}
}

func TestYNABDeleteAcceptsDeletedSplitChildren(t *testing.T) {
	adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
		transaction := writeTestTransaction()
		deleted := request.Method == http.MethodDelete
		transaction["deleted"] = deleted
		transaction["subtransactions"] = []any{map[string]any{"id": "split-a", "transaction_id": "txn-a", "amount": int64(-12340), "deleted": deleted}}
		writeTestResponse(t, response, transaction)
	})
	_, err := adapter.DeleteTransaction(context.Background(), "txn-a")
	require.NoError(t, err)
}

type writeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn writeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestYNABTransportAndMalformedResponseKeepDispatchUncertainty(t *testing.T) {
	for _, phase := range []string{"preflight", "mutation"} {
		for _, failure := range []string{"transport", "malformed", "oversized"} {
			t.Run(phase+failure, func(t *testing.T) {
				adapter := newTestTransactionWriter(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected server call") })
				calls := 0
				adapter.client.maxBodyBytes = 4096
				adapter.client.httpClient.Transport = writeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					if phase == "preflight" || request.Method == http.MethodPut {
						if failure == "transport" {
							return nil, errors.New("private transport detail")
						}
						body := "{"
						if failure == "oversized" {
							body = strings.Repeat(" ", 4097)
						}
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
					}
					body, err := json.Marshal(map[string]any{"data": map[string]any{"transaction": writeTestTransaction()}})
					require.NoError(t, err)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
				})
				_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", ClearCategory: true})
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private transport detail")
				if phase == "mutation" {
					reason, ok := provider.WriteFailureReasonOf(err)
					require.True(t, ok)
					assert.Equal(t, provider.WriteOutcomeUnknown, reason)
					assert.Equal(t, 2, calls)
				} else {
					assert.Equal(t, 1, calls)
				}
			})
		}
	}
}

func TestYNABInvalidUpdateNeverReachesTransport(t *testing.T) {
	for _, update := range []provider.TransactionUpdate{
		{}, {ClearCategory: true}, {TransactionExternalID: "txn-a"},
		{TransactionExternalID: "txn-a", Hidden: provider.Some(false)},
		{TransactionExternalID: "txn-a", MerchantName: provider.Some("Name"), MerchantExternalID: provider.Some("payee-a")},
		{TransactionExternalID: "txn-a", ClearCategory: true, CategoryExternalID: provider.Some("category-a")},
		{TransactionExternalID: "txn-a", MerchantName: provider.Some(strings.Repeat("é", 201))},
		{TransactionExternalID: "txn-a", MerchantExternalID: provider.Some("")},
		{TransactionExternalID: "txn-a", CategoryExternalID: provider.Some("")},
	} {
		adapter := newTestTransactionWriter(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid patch reached transport") })
		_, err := adapter.UpdateTransaction(context.Background(), update)
		code, ok := provider.CodeOf(err)
		require.True(t, ok)
		assert.Equal(t, provider.CodeWriteUnsupported, code)
	}
}

func TestYNABMalformedPreflightAndSplitFactsNeverReachMutation(t *testing.T) {
	for _, defect := range []string{"invalid date", "invalid cleared", "fractional minor", "empty category", "empty transaction", "duplicate child", "missing child amount", "wrong child parent", "split sum", "deleted child"} {
		t.Run(defect, func(t *testing.T) {
			adapter := newTestTransactionWriter(t, func(response http.ResponseWriter, request *http.Request) {
				assert.Equal(t, http.MethodGet, request.Method)
				transaction := writeTestTransaction()
				child := map[string]any{"id": "split-a", "transaction_id": "txn-a", "amount": int64(-12340), "deleted": false}
				switch defect {
				case "invalid date":
					transaction["date"] = "invalid"
				case "invalid cleared":
					transaction["cleared"] = "invalid"
				case "fractional minor":
					transaction["amount"] = int64(1)
				case "empty category":
					transaction["category_id"] = ""
				case "empty transaction":
					transaction = nil
				default:
					transaction["subtransactions"] = []any{child}
					switch defect {
					case "duplicate child":
						transaction["subtransactions"] = []any{child, child}
					case "missing child amount":
						delete(child, "amount")
					case "wrong child parent":
						child["transaction_id"] = "other"
					case "split sum":
						child["amount"] = int64(0)
					case "deleted child":
						child["deleted"] = true
					}
				}
				writeTestResponse(t, response, transaction)
			})
			_, err := adapter.UpdateTransaction(context.Background(), provider.TransactionUpdate{TransactionExternalID: "txn-a", MerchantExternalID: provider.Some("payee-b")})
			reason, ok := provider.WriteFailureReasonOf(err)
			require.True(t, ok)
			assert.Equal(t, provider.WriteResponseIncomplete, reason)
		})
	}
}
