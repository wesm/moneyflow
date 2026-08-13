package analytics

import (
	"testing"

	"github.com/wesm/moneyflow/internal/fixture"
)

var benchmarkResultIdentity int

func BenchmarkQuery100K(b *testing.B) {
	transactions := fixture.Generate(20260812, performanceTransactionCount)
	for _, benchmark := range performanceCases(transactions) {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				result, err := Query(transactions, benchmark.spec)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkResultIdentity = resultIdentity(result)
			}
			if benchmarkResultIdentity == 0 {
				b.Fatal("query result was unexpectedly empty")
			}
		})
	}
}
