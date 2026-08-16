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
	wait := livePendingWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+wait)
	defer cancel()

	firstIdentity, err := client.ProbeIdentity(ctx)
	if err != nil {
		t.Fatal("first subscription identity probe failed")
	}
	first, err := client.fetchRemoteSnapshot(ctx, 1, nil)
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
	second, err := client.fetchRemoteSnapshot(ctx, 1, nil)
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
	t.Logf(
		"live characterization counts: accounts=%d restricted_accounts=%d represented_restricted_accounts=%d transactions=%d posted=%d pending=%d observed_pending_transitions=%d",
		len(second.accounts), restrictedAccounts, representedRestricted, len(second.transactions),
		posted, pending, transitions,
	)
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
