package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestYNABProviderCommandsExposeOnlyProfileFlag(t *testing.T) {
	t.Parallel()
	root := newRootCommand(IOStreams{})
	connect, _, err := root.Find([]string{"provider", "connect", "ynab"})
	require.NoError(t, err)
	assert.Equal(t, "ynab", connect.Name())
	assert.NotNil(t, connect.Flags().Lookup("profile"))
	for _, forbidden := range []string{"currency", "scale", "budget", "token"} {
		assert.Nil(t, connect.Flags().Lookup(forbidden), forbidden)
	}
	disconnect, _, err := root.Find([]string{"provider", "disconnect", "ynab"})
	require.NoError(t, err)
	assert.Equal(t, "ynab", disconnect.Name())
	assert.NotNil(t, disconnect.Flags().Lookup("profile"))
}

func TestYNABProviderConnectUsesMaskedTokenSelectsPlanAndImports(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	client := &commandYNABClient{plans: []ynab.PlanSummary{
		{ID: "plan-b", Name: "Zulu Budget"},
		{ID: "plan-a", Name: "Alpha Budget"},
	}, plan: commandYNABPlan("plan-a")}
	vault := &commandYNABVault{}
	prompts := &recordingPrompt{answers: []string{
		"token-example", "account-password", "account-password", "1", "",
	}}
	var stdout, stderr bytes.Buffer
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &stderr, Prompt: prompts.Prompt,
		OpenYNAB: func(home.Paths) (onboarding.Runtime, error) {
			return commandYNABRuntime(vault, client), nil
		},
	})
	command.SetArgs([]string{"provider", "connect", "ynab"})
	require.NoError(t, command.Execute())
	assert.Equal(t, "Imported 1 YNAB transaction.\n", stdout.String())
	assert.Contains(t, stderr.String(), "1. Alpha Budget")
	assert.NotContains(t, stdout.String()+stderr.String(), "plan-a")
	assert.Equal(t, []bool{true, true, true, false, false}, prompts.secretFlags())
	assert.Equal(t, "plan-a", vault.credentials.PlanID)
}

func TestYNABDisconnectIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	vault := &commandYNABVault{}
	for range 2 {
		var stdout bytes.Buffer
		command := newRootCommand(IOStreams{
			In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{},
			OpenYNAB: func(home.Paths) (onboarding.Runtime, error) {
				return commandYNABRuntime(vault, &commandYNABClient{}), nil
			},
		})
		command.SetArgs([]string{"provider", "disconnect", "ynab"})
		require.NoError(t, command.Execute())
		assert.Equal(t, "Disconnected YNAB. Profile data was preserved.\n", stdout.String())
	}
}

func TestDefaultProviderOnboardingRuntimeSelectsYNABFromManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	catalog, err := openProfileCatalog(root)
	require.NoError(t, err)
	entry, err := catalog.Create(context.Background(), profilecatalog.CreateRequest{
		DisplayName: "Example Profile", ProviderKind: "ynab",
	})
	require.NoError(t, err)
	called := false
	want := onboarding.Runtime{ProviderKind: "ynab", InstanceID: "ynab-runtime"}
	got, err := defaultProviderOnboardingRuntime(entry.ProfilePaths(), IOStreams{
		OpenYNAB: func(home.Paths) (onboarding.Runtime, error) {
			called = true
			return want, nil
		},
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, want.ProviderKind, got.ProviderKind)
	assert.Equal(t, want.InstanceID, got.InstanceID)
}

type commandYNABVault struct {
	exists      bool
	credentials ynab.StoredCredentials
}

func (vault *commandYNABVault) Exists() (bool, error) { return vault.exists, nil }
func (vault *commandYNABVault) Load([]byte) (ynab.StoredCredentials, error) {
	return vault.credentials, nil
}
func (vault *commandYNABVault) Save(credentials ynab.StoredCredentials, _ []byte) error {
	vault.exists = true
	vault.credentials = credentials
	return nil
}
func (vault *commandYNABVault) Delete() error {
	vault.exists = false
	vault.credentials = ynab.StoredCredentials{}
	return nil
}

type commandYNABClient struct {
	plans []ynab.PlanSummary
	plan  ynab.PlanDocument
}

func (client *commandYNABClient) ListPlans(context.Context) ([]ynab.PlanSummary, error) {
	return append([]ynab.PlanSummary(nil), client.plans...), nil
}
func (client *commandYNABClient) FetchPlan(context.Context, string) (ynab.PlanDocument, error) {
	return client.plan, nil
}

func commandYNABRuntime(vault *commandYNABVault, client *commandYNABClient) onboarding.Runtime {
	return onboarding.Runtime{
		ProviderKind: "ynab", YNABVault: vault, InstanceID: "ynab-command-test", Now: time.Now,
		NewYNABClient: func([]byte) (onboarding.YNABPlanClient, error) { return client, nil },
		NewYNABSource: func(_ ynab.StoredCredentials, initial *provider.SnapshotResult) (provider.ReaderSource, error) {
			return &commandYNABSource{result: *initial}, nil
		},
	}
}

type commandYNABSource struct{ result provider.SnapshotResult }

func (source *commandYNABSource) Reader(context.Context, bool) (provider.Reader, provider.SessionFingerprint, error) {
	return commandYNABReader{result: source.result}, "ynab-command-fingerprint", nil
}
func (*commandYNABSource) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }

type commandYNABReader struct{ result provider.SnapshotResult }

func (reader commandYNABReader) FetchSnapshot(context.Context, provider.ProgressFunc) (provider.SnapshotResult, error) {
	return reader.result, nil
}

func commandYNABPlan(planID string) ynab.PlanDocument {
	no, yes := false, true
	return ynab.PlanDocument{
		ID: planID, Name: "Example Budget",
		CurrencyFormat: ynab.CurrencyFormat{ISOCode: "USD", DecimalDigits: 2},
		Accounts:       []ynab.Account{{ID: "account-example", Name: "Account Name", Type: "checking", OnBudget: &yes, Closed: &no, Deleted: &no}},
		Payees:         []ynab.Payee{{ID: "payee-example", Name: "Example Payee", Deleted: &no}},
		Transactions: []ynab.Transaction{{ID: "transaction-example", Date: "2026-08-30", Amount: -12340,
			Cleared: "cleared", Approved: &yes, AccountID: "account-example", PayeeID: "payee-example", Deleted: &no}},
		ServerKnowledge: 1,
	}
}

func bindYNABCommandProfile(t testing.TB, root string) {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	snapshot, err := ynab.Normalize(commandYNABPlan("plan-example"), time.Now().UTC())
	require.NoError(t, err)
	source := &commandYNABSource{result: provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-example"}, Snapshot: snapshot,
	}}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "ynab", Currency: "USD", Scale: 2,
		Renderer: "cli", InstanceID: "ynab-bind-test", Now: time.Now,
	}))
	_, err = service.RefreshProvider(context.Background(), providerRefreshRequest())
	require.NoError(t, err)
	require.NoError(t, profile.Close())
}
