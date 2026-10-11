package api

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

const moneyCasesPath = "../../web/src/lib/money/format-cases.json"

var updateMoneyCases = flag.Bool(
	"update-money-cases", false, "rewrite web/src/lib/money/format-cases.json from formatMoney",
)

// moneyCase is one browser formatter expectation. Minor is a decimal string so that the int64
// extremes survive JSON.
type moneyCase struct {
	Minor   string `json:"minor"`
	Scale   uint8  `json:"scale"`
	Display string `json:"display"`
}

func buildMoneyCases() []moneyCase {
	byScale := []struct {
		scale  uint8
		minors []int64
	}{
		{2, []int64{
			0, 1, -1, 5, -5, 9, -9, 50, -50, 99, -99, 100, -100, 999, -999, 1000, -1000,
			99_999, 100_000, 999_999, -999_999, 1_000_000, -1_000_000, 123_456_789,
			math.MaxInt64, math.MinInt64,
		}},
		{0, []int64{0, 1, -1, 999, 1000, -1000, 999_999, 1_000_000, math.MaxInt64, math.MinInt64}},
		{3, []int64{
			0, 1, -1, -5, 999, 1000, -999_999, 1_000_000, math.MaxInt64, math.MinInt64,
		}},
	}
	var cases []moneyCase
	for _, group := range byScale {
		for _, minor := range group.minors {
			money := domain.Money{Minor: minor, Currency: "USD", Scale: group.scale}
			cases = append(cases, moneyCase{
				Minor: strconv.FormatInt(minor, 10), Scale: group.scale, Display: formatMoney(money),
			})
		}
	}
	return cases
}

// TestWebMoneyCases keeps the browser's money formatter tied to formatMoney. Vitest reads the
// committed file. To rewrite it, run
// `go test ./internal/api -run TestWebMoneyCases -update-money-cases`.
func TestWebMoneyCases(t *testing.T) {
	cases := buildMoneyCases()
	displays := make(map[string]string, len(cases))
	for _, item := range cases {
		displays[item.Minor+"@"+strconv.Itoa(int(item.Scale))] = item.Display
	}
	assert.Equal(t, "+0.00", displays["0@2"])
	assert.Equal(t, "-0.05", displays["-5@2"])
	assert.Equal(t, "+9.99", displays["999@2"])
	assert.Equal(t, "+10,000.00", displays["1000000@2"])
	assert.Equal(t, "+1,000", displays["1000@0"])
	assert.Equal(t, "-999.999", displays["-999999@3"])
	assert.Equal(t, "-92,233,720,368,547,758.08", displays["-9223372036854775808@2"])
	generated, err := json.MarshalIndent(cases, "", "  ")
	require.NoError(t, err)
	generated = append(generated, '\n')

	if *updateMoneyCases {
		require.NoError(t, os.MkdirAll(filepath.Dir(moneyCasesPath), 0o750))
		require.NoError(t, os.WriteFile(moneyCasesPath, generated, 0o600))
		return
	}
	rewrite := "run go test ./internal/api -run TestWebMoneyCases -update-money-cases"
	committed, err := os.ReadFile(moneyCasesPath)
	require.NoError(t, err, rewrite)
	assert.Equal(t, string(generated), string(committed), "format-cases.json is stale; "+rewrite)
}
