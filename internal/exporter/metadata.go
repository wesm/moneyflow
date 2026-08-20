package exporter

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
)

type metadataEntry struct {
	Key   string
	Value string
}

func metadataEntries(metadata app.ExportMetadata) []metadataEntry {
	providerKinds, _ := json.Marshal(metadata.ProviderKinds)
	earliest := ""
	if metadata.EarliestDate != nil {
		earliest = metadata.EarliestDate.String()
	}
	latest := ""
	if metadata.LatestDate != nil {
		latest = metadata.LatestDate.String()
	}
	return []metadataEntry{
		{Key: "moneyflow_export_schema_version", Value: strconv.Itoa(metadata.SchemaVersion)},
		{Key: "moneyflow_app_version", Value: metadata.AppVersion},
		{Key: "exported_at_utc", Value: metadata.ExportedAt.Format("2006-01-02T15:04:05.999999999Z07:00")},
		{Key: "source_revision", Value: strconv.FormatUint(metadata.ProfileRevision, 10)},
		{Key: "journal_cursor", Value: strconv.Itoa(metadata.JournalCursor)},
		{Key: "excluded_pending_operation_count", Value: strconv.Itoa(metadata.ExcludedActiveOperations)},
		{Key: "inactive_redo_operation_count", Value: strconv.Itoa(metadata.InactiveRedoOperations)},
		{Key: "scope", Value: string(metadata.Scope)},
		{Key: "canonical_query", Value: metadata.CanonicalQuery},
		{Key: "transaction_count", Value: strconv.Itoa(metadata.TransactionCount)},
		{Key: "earliest_date", Value: earliest},
		{Key: "latest_date", Value: latest},
		{Key: "provider_kinds", Value: string(providerKinds)},
	}
}

func sanitizeMetadataValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.NewReplacer("\r", " ", "\n", " ", ",", " ").Replace(value)
	if needsFormulaGuard(value) {
		return "'" + value
	}
	return value
}
