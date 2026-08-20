package tui

import (
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/exporter"
)

var exportFormats = []exporter.Format{
	exporter.FormatParquet,
	exporter.FormatCSV,
	exporter.FormatSQLite,
}

var exportScopes = []app.ExportScope{
	app.ExportScopeFull,
	app.ExportScopeFiltered,
}

func exportFormatLabel(format exporter.Format) string {
	switch format {
	case exporter.FormatParquet:
		return "Parquet"
	case exporter.FormatCSV:
		return "CSV"
	case exporter.FormatSQLite:
		return "SQLite"
	default:
		return "Unknown"
	}
}

func exportScopeLabel(scope app.ExportScope) string {
	switch scope {
	case app.ExportScopeFull:
		return "Full dataset"
	case app.ExportScopeFiltered:
		return "Filtered transactions"
	default:
		return "Unknown"
	}
}
