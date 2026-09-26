package analytics

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

var duplicateBenchmarkDigest string

func BenchmarkFindDuplicates100K(b *testing.B) {
	transactions := makeDuplicateBenchmarkTransactions(b, 100_000)
	groups := FindDuplicates(transactions, nil)
	require.Len(b, groups, 50_000)
	require.Equal(b, 100_000, duplicateRowCount(groups))
	require.Equal(b, "07b1af96bca1180ae5948f51cac60d3f1d03d718601a6027b2985eff2cc9183f", duplicateDigest(groups))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		duplicateBenchmarkDigest = duplicateDigest(FindDuplicates(transactions, nil))
	}
}

func makeDuplicateBenchmarkTransactions(t testing.TB, count int) []domain.Transaction {
	t.Helper()
	transactions := make([]domain.Transaction, 0, count)
	for index := 0; index < count; index++ {
		pair := index / 2
		transactions = append(transactions, duplicateTransaction(
			t,
			fmt.Sprintf("transaction-%06d", count-index),
			fmt.Sprintf("2026-08-%02d", pair%28+1),
			fmt.Sprintf("-%d.%02d", pair%10_000+1, pair%100),
			fmt.Sprintf("merchant-%06d", index),
			fmt.Sprintf("Merchant %06d", pair),
			fmt.Sprintf("account-%02d", pair%8),
		))
	}
	return transactions
}

func duplicateRowCount(groups []DuplicateGroup) int {
	count := 0
	for _, group := range groups {
		count += len(group.TransactionIDs)
	}
	return count
}

func duplicateDigest(groups []DuplicateGroup) string {
	digest := sha256.New()
	for _, group := range groups {
		_, _ = fmt.Fprintf(digest, "%s|%s|%d|%s|%d|%s\n", group.Date, group.Amount.Currency,
			group.Amount.Scale, group.MatchingLabel, group.Amount.Minor, group.AccountLabel)
		for _, transactionID := range group.TransactionIDs {
			_, _ = fmt.Fprintln(digest, transactionID)
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}
