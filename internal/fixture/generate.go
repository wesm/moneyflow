package fixture

import (
	"fmt"
	"math/rand"

	"github.com/wesm/moneyflow/internal/domain"
)

type generatedCurrency struct {
	code  domain.Currency
	scale uint8
}

type generatedCategory struct {
	id    string
	name  string
	group string
}

var generatedCurrencies = []generatedCurrency{
	{code: "USD", scale: 2},
	{code: "JPY", scale: 0},
	{code: "KWD", scale: 3},
}

var generatedGroups = []string{
	"Housing", "Food", "Transport", "Utilities", "Shopping", "Health", "Income",
}

// Generate returns deterministic generic transactions for tests and benchmarks.
func Generate(seed int64, count int) []domain.Transaction {
	if count <= 0 {
		return []domain.Transaction{}
	}
	random := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible synthetic data, not security.
	baseDate, err := domain.ParseDate("2020-01-01")
	if err != nil {
		panic("fixture generator has an invalid fixed date")
	}
	transactions := make([]domain.Transaction, count)
	for index := range transactions {
		currency := generatedCurrencies[random.Intn(len(generatedCurrencies))]
		accountIndex := random.Intn(8)
		merchantIndex := random.Intn(128)
		categoryIndex := random.Intn(32)
		group := generatedGroups[categoryIndex%len(generatedGroups)]
		if index%10 == 0 {
			group = "Transfers"
		}
		category := generatedCategory{
			id:    fmt.Sprintf("category-%02d", categoryIndex),
			name:  fmt.Sprintf("Category %02d", categoryIndex),
			group: group,
		}
		if group == "Transfers" {
			category.id = "category-transfer"
			category.name = "Transfer"
		}
		dayOffset := 2_191 - index*2_192/count
		date, dateErr := baseDate.AddDays(dayOffset)
		if dateErr != nil {
			panic("fixture generator date range exceeds the domain")
		}
		magnitude := int64(random.Intn(250_000) + 1)
		if index%5 != 0 {
			magnitude = -magnitude
		}
		var metadata map[string]string
		if index%100 == 0 {
			metadata = map[string]string{"source": "synthetic"}
		}
		transactions[index] = domain.Transaction{
			ID:         fmt.Sprintf("transaction-%06d", index),
			ProviderID: fmt.Sprintf("provider-transaction-%06d", index),
			Provider:   "synthetic",
			Account: domain.EntityRef{
				ID: fmt.Sprintf("account-%02d", accountIndex), Name: fmt.Sprintf("Account %02d", accountIndex),
			},
			Date: date,
			Merchant: domain.EntityRef{
				ID: fmt.Sprintf("merchant-%03d", merchantIndex), Name: fmt.Sprintf("Merchant %03d", merchantIndex),
			},
			Category: domain.CategoryRef{ID: category.id, Name: category.name, Group: category.group},
			Amount:   domain.Money{Minor: magnitude, Currency: currency.code, Scale: currency.scale},
			Hidden:   random.Intn(20) == 0,
			Pending:  random.Intn(25) == 0,
			Notes:    "Synthetic benchmark transaction",
			Metadata: metadata,
		}
	}
	return transactions
}
