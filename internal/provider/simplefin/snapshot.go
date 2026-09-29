package simplefin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

const maxAccountsBytes = 16 << 20

type accountSet struct {
	Errors           []json.RawMessage `json:"errlist"`
	DeprecatedErrors []string          `json:"errors"`
	Connections      []json.RawMessage `json:"connections"`
	Accounts         []account         `json:"accounts"`
}

type account struct {
	ID           string            `json:"id"`
	ConnectionID string            `json:"conn_id"`
	Name         string            `json:"name"`
	Currency     domain.Currency   `json:"currency"`
	Transactions []json.RawMessage `json:"transactions"`
}

type transaction struct {
	ID           string `json:"id"`
	Posted       int64  `json:"posted"`
	TransactedAt int64  `json:"transacted_at"`
	Amount       string `json:"amount"`
	Description  string `json:"description"`
}

// FetchSnapshot imports posted rows only after every requested window is valid.
func (client *Client) FetchSnapshot(ctx context.Context, request provider.FetchRequest, progress provider.ProgressFunc) (provider.SnapshotResult, error) {
	windows, err := fetchWindows(request)
	if err != nil {
		return provider.SnapshotResult{}, err
	}
	result := domain.ImportSnapshot{ObservedAt: request.Now.UTC().Truncate(time.Millisecond)}
	accounts := make(map[string]bool)
	merchants := make(map[string]bool)
	transactions := make(map[string]domain.ImportTransaction)
	for _, window := range windows {
		set, fetchErr := client.fetchWindow(ctx, window, request.Now)
		if fetchErr != nil {
			return provider.SnapshotResult{}, fetchErr
		}
		for _, account := range set.Accounts {
			if account.ID == "" || account.ConnectionID == "" || account.Currency != client.config.Currency {
				return provider.SnapshotResult{}, provider.NewDataInvalidError(provider.DataInvalidEntity)
			}
			label, labelErr := domain.NormalizeDisplayLabel(account.Name)
			if labelErr != nil {
				return provider.SnapshotResult{}, provider.NewDataInvalidError(provider.DataInvalidEntity)
			}
			accountID := tupleID(account.ConnectionID, account.ID)
			if !accounts[accountID] {
				accounts[accountID] = true
				result.Accounts = append(result.Accounts, domain.ImportEntity{Kind: domain.EntityKindAccount, ExternalID: accountID, Label: label})
			}
			for _, raw := range account.Transactions {
				var pending struct {
					Pending bool `json:"pending"`
				}
				if json.Unmarshal(raw, &pending) != nil {
					return provider.SnapshotResult{}, provider.NewError(provider.CodeDataInvalid)
				}
				if pending.Pending {
					continue
				}
				var value transaction
				if json.Unmarshal(raw, &value) != nil {
					return provider.SnapshotResult{}, provider.NewError(provider.CodeDataInvalid)
				}
				row, merchant, normalizeErr := client.normalizeTransaction(account, value)
				if normalizeErr != nil {
					return provider.SnapshotResult{}, normalizeErr
				}
				if previous, exists := transactions[row.ExternalID]; exists {
					if previous != row {
						return provider.SnapshotResult{}, provider.NewDataInvalidError(provider.DataInvalidDuplicateIdentity)
					}
					continue
				}
				transactions[row.ExternalID] = row
				if !merchants[merchant.ExternalID] {
					merchants[merchant.ExternalID] = true
					result.Merchants = append(result.Merchants, merchant)
				}
				result.Transactions = append(result.Transactions, row)
			}
		}
		if progress != nil {
			progress(provider.Progress{Partition: "posted", Fetched: len(result.Transactions), Attempt: 1, Pass: 1})
		}
	}
	if err = ctx.Err(); err != nil {
		return provider.SnapshotResult{}, err
	}
	if result.Validate() != nil {
		return provider.SnapshotResult{}, provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	return provider.SnapshotResult{Identity: provider.ProfileIdentity{Kind: "simplefin", RemoteID: client.remoteID}, Snapshot: result}, nil
}

func (client *Client) fetchWindow(ctx context.Context, window fetchWindow, now time.Time) (accountSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint := *client.endpoint
	query := endpoint.Query()
	query.Set("version", "2")
	query.Set("start-date", strconv.FormatInt(window.Start.Unix(), 10))
	query.Set("end-date", strconv.FormatInt(window.End.Unix(), 10))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return accountSet{}, provider.NewError(provider.CodeUnavailable)
	}
	request.SetBasicAuth(client.username, client.password)
	request.Header.Set("Accept", "application/json")
	body, err := readResponse(ctx, client.http, request, maxAccountsBytes, now)
	if err != nil {
		return accountSet{}, err
	}
	var result accountSet
	if json.Unmarshal(body, &result) != nil || result.Errors == nil || result.Connections == nil || result.Accounts == nil ||
		len(result.Errors) > 0 || len(result.DeprecatedErrors) > 0 {
		return accountSet{}, provider.NewError(provider.CodeDataInvalid)
	}
	return result, nil
}

func (client *Client) normalizeTransaction(account account, value transaction) (domain.ImportTransaction, domain.ImportEntity, error) {
	if value.ID == "" {
		return domain.ImportTransaction{}, domain.ImportEntity{}, provider.NewDataInvalidError(provider.DataInvalidTransactionID)
	}
	posted := value.Posted
	if posted == 0 {
		posted = value.TransactedAt
	}
	date, err := domain.ParseDate(time.Unix(posted, 0).UTC().Format(time.DateOnly))
	if posted == 0 || err != nil {
		return domain.ImportTransaction{}, domain.ImportEntity{}, provider.NewDataInvalidError(provider.DataInvalidTransactionDate)
	}
	amount, err := domain.ParseMoney(value.Amount, client.config.Currency, client.config.Scale)
	if err != nil {
		return domain.ImportTransaction{}, domain.ImportEntity{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
	}
	label, err := domain.NormalizeDisplayLabel(value.Description)
	if err != nil {
		return domain.ImportTransaction{}, domain.ImportEntity{}, provider.NewDataInvalidError(provider.DataInvalidEntity)
	}
	merchant := domain.ImportEntity{Kind: domain.EntityKindMerchant, ExternalID: tupleID(account.ConnectionID, value.Description), Label: label}
	return domain.ImportTransaction{ExternalID: tupleID(account.ConnectionID, account.ID, value.ID),
		AccountExternalID: tupleID(account.ConnectionID, account.ID), MerchantExternalID: merchant.ExternalID,
		Date: date, Amount: amount}, merchant, nil
}

// String tuples cannot collide when provider IDs contain separators.
func tupleID(parts ...string) string {
	value, _ := json.Marshal(parts)
	return string(value)
}

type fetchWindow struct{ Start, End time.Time }

func fetchWindows(request provider.FetchRequest) ([]fetchWindow, error) {
	if request.Now.IsZero() {
		return nil, provider.NewError(provider.CodeDataInvalid)
	}
	now := request.Now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start, end := today.AddDate(0, 0, -1095), today.AddDate(0, 0, 1)
	if !request.LastSuccess.IsZero() {
		last := request.LastSuccess.UTC()
		overlap := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -14)
		if overlap.After(start) {
			start = overlap
		}
	}
	if !start.Before(end) {
		return nil, provider.NewError(provider.CodeDataInvalid)
	}
	var windows []fetchWindow
	for start.Before(end) {
		next := start.AddDate(0, 0, 90)
		if next.After(end) {
			next = end
		}
		windows = append(windows, fetchWindow{Start: start, End: next})
		start = next
	}
	return windows, nil
}
