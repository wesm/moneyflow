package bankcsv

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chaseHeader = "Transaction Date,Description,Amount,Category,Memo\n"

func TestParseChasePreservesOccurrencesAndExactMoney(t *testing.T) {
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	input := "\ufeff" + chaseHeader + "09/01/2026, Example Shop ,-12.34,Food,\" line one\nline two \"\n09/01/2026,Example Shop,-12.34,Food,\n"
	file, err := Parse(t.Context(), strings.NewReader(input), mapping, " Main card ", "input.csv", ProductionLimits)
	require.NoError(t, err)
	require.Len(t, file.Rows, 2)
	assert.Equal(t, int64(-1234), file.Rows[0].Amount.Minor)
	assert.Equal(t, "2026-09-01", file.Rows[0].Date.String())
	assert.Equal(t, " line one\nline two ", file.Rows[0].Notes)
	assert.NotEqual(t, file.Rows[0].SourceKey, file.Rows[1].SourceKey)
	assert.Equal(t, int64(len(input)), file.Bytes)
	overlap, err := Parse(t.Context(), strings.NewReader(input), mapping, "Main card", "another.csv", ProductionLimits)
	require.NoError(t, err)
	assert.Equal(t, file.Rows, overlap.Rows)
	assert.NotEqual(t, file.Key, overlap.Key)
	other, err := Parse(t.Context(), strings.NewReader(input), mapping, "Other card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	assert.NotEqual(t, file.Rows[0].SourceKey, other.Rows[0].SourceKey)
}

func TestParseSkipsSemanticErrorsButRejectsMalformedFiles(t *testing.T) {
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	file, err := Parse(t.Context(), strings.NewReader(chaseHeader+"02/30/2026,Example,-1.00,,\n09/01/2026,Example,0.001,,\n09/01/2026,,1,,\n09/01/2026,Example,1,,\n"), mapping, "Card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	require.Len(t, file.Rows, 1)
	assert.Len(t, file.Skipped, 3)
	assert.Equal(t, "Uncategorized", file.Rows[0].Category)
	for _, input := range []string{
		"Transaction Date,Description,Description,Amount\n",
		"Transaction Date,Description\n",
		chaseHeader + "09/01/2026,Example,1\n",
		chaseHeader + "09/01/2026,\"unfinished,1,,\n",
		chaseHeader + "09/01/2026,\xff,1,,\n",
	} {
		_, err := Parse(t.Context(), strings.NewReader(input), mapping, "Card", "input.csv", ProductionLimits)
		require.Error(t, err)
	}
	empty, err := Parse(t.Context(), strings.NewReader(chaseHeader), mapping, "Card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	assert.Empty(t, empty.Rows)
}

func TestParseBoundsAndSplitAmount(t *testing.T) {
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	input := chaseHeader + "09/01/2026,Example,1.23,,\n"
	for _, change := range []func(*Limits){
		func(l *Limits) { l.BytesPerFile = 10 },
		func(l *Limits) { l.BytesPerRecord = 10 },
		func(l *Limits) { l.Columns = 2 },
		func(l *Limits) { l.BytesPerField = 3 },
		func(l *Limits) { l.Records = 1 },
	} {
		limits := ProductionLimits
		change(&limits)
		_, err := Parse(t.Context(), strings.NewReader(input+"09/02/2026,Example,1,,\n"), mapping, "Card", "input.csv", limits)
		require.Error(t, err)
	}
	mapping.Amount = ""
	mapping.Credit, mapping.Debit = "Credit", "Debit"
	file, err := Parse(t.Context(), strings.NewReader("Transaction Date,Description,Credit,Debit\n09/01/2026,Example,5,12.34\n"), mapping, "Card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	require.Len(t, file.Rows, 1)
	assert.Equal(t, int64(-734), file.Rows[0].Amount.Minor)
	emptyAmount, err := Parse(t.Context(), strings.NewReader("Transaction Date,Description,Credit,Debit\n09/01/2026,Example,,\n"), mapping, "Card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	assert.Empty(t, emptyAmount.Rows)
	assert.Len(t, emptyAmount.Skipped, 1)
	mapping.Amount = "Amount"
	_, err = Parse(t.Context(), strings.NewReader(input), mapping, "Card", "input.csv", ProductionLimits)
	require.Error(t, err)
}

func TestParseBOMBeforeQuotedHeaderAndQuotedNewline(t *testing.T) {
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	input := "\ufeff\"Transaction Date\",Description,Amount,Memo\n09/01/2026,Example,1,\"long\nnotes\"\n"
	file, err := Parse(t.Context(), strings.NewReader(input), mapping, "Card", "input.csv", ProductionLimits)
	require.NoError(t, err)
	require.Len(t, file.Rows, 1)
	assert.Equal(t, "long\nnotes", file.Rows[0].Notes)
	limits := ProductionLimits
	limits.BytesPerRecord = 60
	input = "\ufeff\"" + strings.Repeat("unknown\n", 12) + "\",Transaction Date,Description,Amount\n"
	_, err = Parse(t.Context(), strings.NewReader(input), mapping, "Card", "input.csv", limits)
	require.Error(t, err)
}

func TestParseCancelledInput(t *testing.T) {
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Parse(ctx, strings.NewReader(chaseHeader), mapping, "Card", "input.csv", ProductionLimits)
	require.ErrorIs(t, err, context.Canceled)
}
