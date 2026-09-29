package mcp

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	amzimport "github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestMCPTaxonomyDryRunAndStaging(t *testing.T) {
	for _, test := range []struct {
		name, tool                    string
		args                          map[string]any
		entityCount, transactionCount int
		label, group                  string
		retired                       bool
	}{
		{"category create", "manage_category", map[string]any{"action": "create", "label": "New Category", "group_id": "group_a"}, 1, 0, "New Category", "group_a", false},
		{"category rename", "manage_category", map[string]any{"action": "rename", "category_id": "category_a", "label": "Renamed"}, 1, 2, "Renamed", "group_a", false},
		{"category move", "manage_category", map[string]any{"action": "move", "category_id": "category_a", "destination_id": "group_b"}, 1, 2, "Category A", "group_b", false},
		{"category merge", "manage_category", map[string]any{"action": "merge", "category_id": "category_a", "destination_id": "category_b"}, 1, 2, "Category A", "group_a", true},
		{"category delete", "manage_category", map[string]any{"action": "delete", "category_id": "category_a", "replacement_id": string(domain.UncategorizedCategoryID)}, 1, 2, "Category A", "group_a", true},
		{"group create", "manage_category_group", map[string]any{"action": "create", "label": "New Group"}, 1, 0, "New Group", "", false},
		{"group rename", "manage_category_group", map[string]any{"action": "rename", "group_id": "group_a", "label": "Renamed"}, 1, 2, "Renamed", "", false},
		{"group merge", "manage_category_group", map[string]any{"action": "merge", "group_id": "group_a", "destination_id": "group_b"}, 2, 2, "Group A", "", true},
		{"group delete", "manage_category_group", map[string]any{"action": "delete", "group_id": "group_a", "replacement_id": "group_b"}, 2, 2, "Group A", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, closeProfile := writeTestService(t, 3)
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			test.args["expected_revision"] = "1"
			for _, dry := range []bool{true, false} {
				test.args["dry_run"] = dry
				result := callWriteTool(t, client, test.tool, test.args)
				require.False(t, result.IsError, "%v", result.StructuredContent)
				doc := result.StructuredContent.(map[string]any)
				assert.Equal(t, float64(test.transactionCount), doc["affected_count"])
				assert.Len(t, doc["entity_changes"], test.entityCount)
				var subject map[string]any
				for _, raw := range doc["entity_changes"].([]any) {
					change := raw.(map[string]any)
					if change["entity_id"] == doc["entity_id"] {
						subject = change["after"].(map[string]any)
						if test.args["action"] == "create" {
							assert.Nil(t, change["before"])
						}
					}
				}
				require.NotNil(t, subject)
				assert.Equal(t, test.label, subject["label"])
				assert.Equal(t, test.retired, subject["retired"])
				if test.group != "" {
					assert.Equal(t, test.group, subject["group_id"])
				}
				if dry {
					assert.Equal(t, uint64(1), service.Revision())
					assert.Equal(t, float64(0), doc["pending"].(map[string]any)["active_operations"])
				} else {
					assert.Equal(t, uint64(2), service.Revision())
					assert.Equal(t, float64(1), doc["pending"].(map[string]any)["active_operations"])
					catalog := callWriteTool(t, client, "get_categories", nil)
					require.False(t, catalog.IsError)
					collection := "categories"
					if test.tool == "manage_category_group" {
						collection = "groups"
					}
					found := false
					for _, entry := range catalog.StructuredContent.(map[string]any)[collection].([]any) {
						entity := entry.(map[string]any)
						if entity["id"] == doc["entity_id"] {
							found = true
							assert.Equal(t, test.label, entity["label"])
						}
					}
					assert.Equal(t, !test.retired, found)
				}
			}
		})
	}
}

func TestMCPTaxonomyCreateUndoRedoCommit(t *testing.T) {
	service, closeProfile := writeTestService(t, 2)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	group := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": "1", "action": "create", "label": "New Group"})
	require.False(t, group.IsError)
	groupID := group.StructuredContent.(map[string]any)["entity_id"]
	category := callWriteTool(t, client, "manage_category", map[string]any{"expected_revision": "2", "action": "create", "label": "New Category", "group_id": groupID})
	require.False(t, category.IsError)
	categoryID := category.StructuredContent.(map[string]any)["entity_id"]
	assigned := callWriteTool(t, client, "update_transaction_category", map[string]any{"expected_revision": "3", "transaction_id": "transaction_000", "category_id": categoryID})
	require.False(t, assigned.IsError)
	for index, name := range []string{"undo_changes", "redo_changes"} {
		result := callWriteTool(t, client, name, map[string]any{"expected_revision": strconv.Itoa(index + 4)})
		require.False(t, result.IsError)
	}
	review := callWriteTool(t, client, "review_changes", map[string]any{"expected_revision": "6"})
	require.False(t, review.IsError)
	assert.Equal(t, float64(3), review.StructuredContent.(map[string]any)["pending"].(map[string]any)["active_operations"])
	commit := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": "6", "reviewed_revision": "6"})
	require.False(t, commit.IsError)
	assert.Equal(t, true, commit.StructuredContent.(map[string]any)["completed"])
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 2})
	require.NoError(t, err)
	for _, row := range rows.Rows {
		if row.ID == "transaction_000" {
			assert.Equal(t, categoryID, row.Category.ID)
			assert.Equal(t, groupID, row.Category.GroupID)
		}
	}
}

func TestMCPTaxonomyRefusesProviderManagement(t *testing.T) {
	for _, kind := range []string{"monarch", "ynab"} {
		t.Run(kind, func(t *testing.T) {
			service, _, closeProfile := providerTestService(t, 1, kind)
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			revision := service.Revision()
			for _, dry := range []bool{true, false} {
				result := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": strconv.FormatUint(revision, 10), "action": "create", "label": "New Group", "dry_run": dry})
				require.True(t, result.IsError)
				assert.Equal(t, "provider_write_unsupported", result.StructuredContent.(map[string]any)["code"])
				assert.Equal(t, revision, service.Revision())
			}
		})
	}
}

func TestMCPTaxonomyRejectsInvalidIntentAndStaleReplays(t *testing.T) {
	for _, test := range []struct {
		name, tool string
		args       map[string]any
	}{
		{"protected category", "manage_category", map[string]any{"action": "rename", "category_id": string(domain.UncategorizedCategoryID), "label": "Changed"}},
		{"protected group", "manage_category_group", map[string]any{"action": "rename", "group_id": string(domain.UncategorizedGroupID), "label": "Changed"}},
		{"colliding rename", "manage_category", map[string]any{"action": "rename", "category_id": "category_a", "label": "CATEGORY B"}},
		{"missing category replacement", "manage_category", map[string]any{"action": "delete", "category_id": "category_a"}},
		{"missing group replacement", "manage_category_group", map[string]any{"action": "delete", "group_id": "group_a"}},
		{"missing group", "manage_category", map[string]any{"action": "create", "label": "New Category"}},
		{"missing entity", "manage_category", map[string]any{"action": "rename", "category_id": "missing", "label": "Renamed"}},
		{"caller supplied create ID", "manage_category_group", map[string]any{"action": "create", "label": "New Group", "group_id": "group_new"}},
		{"extraneous rename destination", "manage_category", map[string]any{"action": "rename", "category_id": "category_a", "label": "Renamed", "destination_id": "category_b"}},
		{"extraneous delete label", "manage_category", map[string]any{"action": "delete", "category_id": "category_a", "replacement_id": "category_b", "label": "Ignored"}},
		{"group move", "manage_category_group", map[string]any{"action": "move", "group_id": "group_a", "destination_id": "group_b"}},
		{"unknown action", "manage_category", map[string]any{"action": "unknown", "category_id": "category_a"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, closeProfile := writeTestService(t, 3)
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			test.args["expected_revision"] = "1"
			for _, dry := range []bool{true, false} {
				test.args["dry_run"] = dry
				result := callWriteTool(t, client, test.tool, test.args)
				require.True(t, result.IsError)
				assert.Equal(t, "invalid_operation", result.StructuredContent.(map[string]any)["code"])
				assert.Equal(t, uint64(1), service.Revision())
			}
		})
	}
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	args := map[string]any{"expected_revision": "1", "action": "rename", "category_id": "category_a", "label": "Renamed"}
	staged := callWriteTool(t, client, "manage_category", args)
	require.False(t, staged.IsError)
	for _, dry := range []bool{true, false} {
		args["dry_run"] = dry
		stale := callWriteTool(t, client, "manage_category", args)
		require.True(t, stale.IsError)
		assert.Equal(t, "revision_conflict", stale.StructuredContent.(map[string]any)["code"])
		assert.Equal(t, uint64(2), service.Revision())
	}
}

func TestMCPTaxonomyLargeEditBoundsResponseNotMutation(t *testing.T) {
	service, closeProfile := writeTestService(t, 102)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	result := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": "1", "action": "merge", "group_id": "group_a", "destination_id": "group_b"})
	require.False(t, result.IsError)
	doc := result.StructuredContent.(map[string]any)
	assert.Equal(t, float64(101), doc["affected_count"])
	assert.Len(t, doc["changes"], 100)
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 102})
	require.NoError(t, err)
	require.Len(t, rows.Rows, 102)
	for _, row := range rows.Rows {
		assert.Equal(t, "group_b", row.Category.GroupID)
	}
}

func TestMCPTaxonomyAmazonLocalCommitSurvivesReopen(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = app.ImportAmazonProfile(t.Context(), profile, app.AmazonImportRequest{
		Candidate:  amzimport.Candidate{Digest: strings.Repeat("a", 64)},
		Settings:   amzimport.Settings{Currency: "USD", Scale: 2},
		ImportedAt: time.Date(2026, time.August, 29, 14, 30, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	client, cleanup := connectWriteTestServer(t, service, true)
	t.Cleanup(cleanup)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	staged := callWriteTool(t, client, "manage_category_group", map[string]any{
		"expected_revision": strconv.FormatUint(service.Revision(), 10), "action": "create", "label": "New Group",
	})
	require.False(t, staged.IsError)
	id := staged.StructuredContent.(map[string]any)["entity_id"]
	undo := callWriteTool(t, client, "undo_changes", map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10)})
	require.False(t, undo.IsError)
	catalog := callWriteTool(t, client, "get_categories", nil)
	require.False(t, catalog.IsError)
	for _, value := range catalog.StructuredContent.(map[string]any)["groups"].([]any) {
		assert.NotEqual(t, id, value.(map[string]any)["id"])
	}
	redo := callWriteTool(t, client, "redo_changes", map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10)})
	require.False(t, redo.IsError)
	revision := strconv.FormatUint(service.Revision(), 10)
	commit := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	require.False(t, commit.IsError)
	assert.Equal(t, true, commit.StructuredContent.(map[string]any)["completed"])
	require.NoError(t, profile.Close())
	profile, err = sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	loaded, err := profile.Load(t.Context())
	require.NoError(t, err)
	assert.Empty(t, loaded.Journal)
	found := false
	for _, group := range loaded.Committed.Groups {
		if string(group.ID) == id {
			found = true
			assert.Equal(t, "New Group", group.Label)
			assert.False(t, group.Retired)
		}
	}
	assert.True(t, found)
}

func TestMCPTaxonomyStartsPristineLocalProfile(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, profile.Close()) }()
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	for _, revision := range []string{"", "invalid", "-1"} {
		invalid := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": revision, "action": "create", "label": "New Group"})
		require.True(t, invalid.IsError)
		assert.Equal(t, uint64(0), service.Revision())
	}
	for _, dry := range []bool{true, false} {
		result := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": "0", "action": "create", "label": "New Group", "dry_run": dry})
		require.False(t, result.IsError, "%v", result.StructuredContent)
		if dry {
			assert.Equal(t, uint64(0), service.Revision())
		} else {
			assert.Equal(t, uint64(1), service.Revision())
		}
	}
	stale := callWriteTool(t, client, "manage_category_group", map[string]any{"expected_revision": "0", "action": "create", "label": "Other Group"})
	require.True(t, stale.IsError)
	assert.Equal(t, "revision_conflict", stale.StructuredContent.(map[string]any)["code"])
}
