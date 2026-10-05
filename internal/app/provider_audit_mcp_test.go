package app_test

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	moneyflowmcp "github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestMCPStatusRetainsProviderCompletionAuditWarning(t *testing.T) {
	ctx := t.Context()
	_, profile := newProviderRefreshService(t)
	failing := &providerCompletionAuditProfile{Profile: profile, phase: "finalize"}
	service, err := app.NewProfileService(ctx, failing)
	require.NoError(t, err)
	now := providerWriteTime()
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "synthetic-session",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	configureProviderRefreshService(t, service, source, now, "audit-mcp-test")
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	target := before.Committed.Transactions[0]
	revision, err := profile.Append(ctx, before.Revision, domain.Operation{
		ID: "mcp-audit-test-edit", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: before.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	server, err := moneyflowmcp.New(moneyflowmcp.Dependencies{
		Service: service, ProfileID: "synthetic-profile", ProfileRoot: t.TempDir(), Clock: time.Now,
		Random: &incrementingReader{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, moneyflowmcp.Options{AllowWrite: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "audit-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Wait())
	})
	committed, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{Name: "commit_changes", Arguments: map[string]any{
		"expected_revision": strconv.FormatUint(revision, 10), "reviewed_revision": strconv.FormatUint(revision, 10),
	}})
	require.NoError(t, err)
	require.False(t, committed.IsError, "%v", committed.StructuredContent)
	var visible app.ProviderWriteStatus
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var statusErr error
		visible, statusErr = service.ProviderWriteStatus(ctx)
		require.NoError(collect, statusErr)
		require.Empty(collect, visible.Phase)
		require.NotEmpty(collect, visible.AuditWarning)
	}, 3*time.Second, 10*time.Millisecond)
	persisted, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Empty(t, persisted.Journal)
	require.True(t, persisted.Committed.Transactions[0].Hidden)
	for _, tool := range []string{"get_commit_status", "get_account_info"} {
		result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{Name: tool})
		require.NoError(t, err)
		require.False(t, result.IsError, "%v", result.StructuredContent)
		document := result.StructuredContent.(map[string]any)
		write := document["write"].(map[string]any)
		assert.Equal(t, "ok", document["status"])
		assert.Equal(t, strconv.FormatUint(persisted.Revision, 10), document["revision"])
		assert.Empty(t, write["phase"])
		assert.Equal(t, visible.AuditWarning, write["audit_warning"], "%s must retain completion warning", tool)
	}
}
