//go:build monarchlive

package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
)

func TestLiveCharacterization(t *testing.T) {
	if os.Getenv("MONEYFLOW_MONARCH_LIVE") != "1" {
		t.Skip("set MONEYFLOW_MONARCH_LIVE=1 for the explicit live characterization")
	}
	client := newLiveClient(t)
	wait := livePendingWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+wait)
	defer cancel()

	firstIdentity, err := client.ProbeIdentity(ctx)
	if err != nil {
		t.Fatal("first subscription identity probe failed")
	}
	first, err := client.fetchRemoteSnapshot(ctx, 1, 1, nil)
	if err != nil {
		t.Fatal("first complete live snapshot failed")
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("live pending-lifecycle wait exceeded the test deadline")
		case <-timer.C:
		}
	}
	second, err := client.fetchRemoteSnapshot(ctx, 1, 1, nil)
	if err != nil {
		t.Fatal("second complete live snapshot failed")
	}
	secondIdentity, err := client.ProbeIdentity(ctx)
	if err != nil {
		t.Fatal("second subscription identity probe failed")
	}
	if firstIdentity.Kind != secondIdentity.Kind || firstIdentity.RemoteID != secondIdentity.RemoteID {
		t.Fatal("subscription identity changed within one live characterization")
	}

	restrictedAccounts, representedRestricted, err := characterizeAccountScope(second)
	if err != nil {
		t.Fatal("transaction feed referenced an account outside the complete account response")
	}
	normalized, err := client.normalizeSnapshot(
		second.accounts, second.merchants, second.groups, second.categories, second.transactions,
	)
	if err != nil {
		t.Fatal("live snapshot normalization failed")
	}
	pending, posted, transitions := characterizePendingLifecycle(first.transactions, second.transactions)
	if len(normalized.Transactions) != posted {
		t.Fatal("pending transactions entered the normalized posted snapshot")
	}
	if os.Getenv("MONEYFLOW_MONARCH_LIVE_REQUIRE_PENDING_TRANSITION") == "1" && transitions == 0 {
		t.Fatal("no pending-to-posted transition was observed during the requested wait")
	}
	merchantIDInput, err := characterizeMerchantIDInput(ctx, client)
	if err != nil {
		t.Fatal("transaction update input characterization failed")
	}
	emptyMerchants := characterizeEmptyMerchants(second.merchants, second.transactions)
	t.Logf(
		"live characterization counts: accounts=%d restricted_accounts=%d represented_restricted_accounts=%d transactions=%d posted=%d pending=%d observed_pending_transitions=%d empty_merchants=%d merchant_id_input=%t",
		len(second.accounts), restrictedAccounts, representedRestricted, len(second.transactions),
		posted, pending, transitions, emptyMerchants, merchantIDInput,
	)
}

func TestLiveDeleteDisposableTransactions(t *testing.T) {
	if os.Getenv("MONEYFLOW_MONARCH_LIVE") != "1" ||
		os.Getenv("MONEYFLOW_MONARCH_LIVE_DELETE") != "1" {
		t.Skip("set both live opt-ins for the destructive deletion characterization")
	}
	externalIDs := []string{os.Getenv("MONEYFLOW_MONARCH_LIVE_DELETE_TRANSACTION_ID")}
	if externalIDs[0] == "" {
		t.Fatal("MONEYFLOW_MONARCH_LIVE_DELETE_TRANSACTION_ID must name a disposable transaction")
	}
	if bankID := os.Getenv("MONEYFLOW_MONARCH_LIVE_DELETE_BANK_TRANSACTION_ID"); bankID != "" {
		externalIDs = append(externalIDs, bankID)
	}
	client := newLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for _, externalID := range externalIDs {
		result, deleteErr := client.DeleteTransaction(ctx, externalID)
		snapshot, snapshotErr := client.fetchRemoteSnapshot(ctx, 1, 1, nil)
		if snapshotErr != nil {
			t.Fatal("complete snapshot after the deletion attempt failed")
		}
		if deleteErr != nil {
			t.Fatal("disposable transaction deletion was not positively characterized")
		}
		for _, transaction := range snapshot.transactions {
			if transaction.ID == externalID {
				t.Fatal("deleted disposable transaction remained in the complete snapshot")
			}
		}
		t.Logf(
			"live delete characterization: already_absent=%t transactions_after=%d",
			result.AlreadyAbsent,
			len(snapshot.transactions),
		)
	}
}

func newLiveClient(t *testing.T) *Client {
	t.Helper()
	sessionPath := os.Getenv("MONEYFLOW_MONARCH_LIVE_SESSION_FILE")
	if sessionPath == "" {
		t.Fatal("MONEYFLOW_MONARCH_LIVE_SESSION_FILE is required")
	}
	profileRoot := os.Getenv("MONEYFLOW_HOME")
	if profileRoot == "" {
		t.Fatal("MONEYFLOW_HOME must name an isolated temporary directory")
	}
	session, err := readLiveSession(sessionPath)
	if err != nil {
		t.Fatal("live session could not be loaded")
	}
	paths, err := home.ResolveRoot(profileRoot, nil, "")
	if err != nil {
		t.Fatal("isolated live profile root is invalid")
	}
	sessions, err := NewSessionStore(paths)
	if err != nil || sessions.Save(session) != nil {
		t.Fatal("live session could not be copied into the isolated profile")
	}
	client, err := NewClient(Options{
		ImportCurrency: domain.Currency("USD"), ImportScale: 2,
	}, session.Token, session.DeviceUUID)
	if err != nil {
		t.Fatal("live client could not be created")
	}
	return client
}

type inputTypeCharacterization struct {
	Type struct {
		InputFields []struct {
			Name string `json:"name"`
			Type struct {
				Kind   string `json:"kind"`
				Name   string `json:"name"`
				OfType *struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"ofType"`
			} `json:"type"`
		} `json:"inputFields"`
	} `json:"__type"`
}

func characterizeMerchantIDInput(ctx context.Context, client *Client) (bool, error) {
	data, err := graphQLCall[inputTypeCharacterization](ctx, client, "CharacterizeUpdateInput", `
query CharacterizeUpdateInput {
  __type(name: "UpdateTransactionMutationInput") {
    inputFields { name type { kind name ofType { kind name } } }
  }
}`, nil)
	if err != nil {
		return false, err
	}
	for _, field := range data.Type.InputFields {
		if field.Name != "merchantId" && field.Name != "merchantID" {
			continue
		}
		name := field.Type.Name
		if field.Type.OfType != nil {
			name = field.Type.OfType.Name
		}
		if name == "ID" || name == "UUID" {
			return true, nil
		}
	}
	return false, nil
}

func characterizeEmptyMerchants(merchants []Merchant, transactions []Transaction) int {
	referenced := make(map[string]struct{}, len(transactions))
	for _, transaction := range transactions {
		referenced[transaction.Merchant.ID] = struct{}{}
	}
	empty := 0
	for _, merchant := range merchants {
		if _, exists := referenced[merchant.ID]; !exists {
			empty++
		}
	}
	return empty
}

func readLiveSession(path string) (Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(io.LimitReader(file, maxSessionBytes+1))
	if err != nil || int64(len(contents)) > maxSessionBytes {
		return Session{}, io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var session Session
	if err = decoder.Decode(&session); err != nil {
		return Session{}, err
	}
	if err = requireJSONEOF(decoder); err != nil {
		return Session{}, err
	}
	return session, session.Validate()
}

func livePendingWait(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("MONEYFLOW_MONARCH_LIVE_PENDING_WAIT")
	if value == "" {
		return 0
	}
	wait, err := time.ParseDuration(value)
	if err != nil || wait < 0 || wait > 30*time.Minute {
		t.Fatal("MONEYFLOW_MONARCH_LIVE_PENDING_WAIT must be between zero and 30 minutes")
	}
	return wait
}

func characterizeAccountScope(snapshot remoteSnapshot) (int, int, error) {
	restricted := make(map[string]struct{})
	accounts := make(map[string]struct{}, len(snapshot.accounts))
	for _, account := range snapshot.accounts {
		accounts[account.ID] = struct{}{}
		if account.IsHidden || account.HideFromList || account.DeactivatedAt != nil {
			restricted[account.ID] = struct{}{}
		}
	}
	represented := make(map[string]struct{})
	for _, transaction := range snapshot.transactions {
		if _, exists := accounts[transaction.Account.ID]; !exists {
			return 0, 0, io.ErrUnexpectedEOF
		}
		if _, exists := restricted[transaction.Account.ID]; exists {
			represented[transaction.Account.ID] = struct{}{}
		}
	}
	return len(restricted), len(represented), nil
}

func characterizePendingLifecycle(first, second []Transaction) (int, int, int) {
	firstPending := make(map[string]struct{})
	for _, transaction := range first {
		if transaction.Pending {
			firstPending[transaction.ID] = struct{}{}
		}
	}
	pending := 0
	posted := 0
	transitions := 0
	for _, transaction := range second {
		if transaction.Pending {
			pending++
			continue
		}
		posted++
		if _, wasPending := firstPending[transaction.ID]; wasPending {
			transitions++
		}
	}
	return pending, posted, transitions
}
