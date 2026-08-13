# Go Port Foundation Benchmark

The read-only Go query pipeline was measured on 2026-08-12 with the committed deterministic
100,000-transaction generator.

## Environment

- OS: macOS 26.5.2, arm64
- CPU: Apple M5 Max
- Go: 1.26.5
- Command: `go test ./internal/analytics -run '^$' -bench 'BenchmarkQuery100K' -benchmem -count=5`

The development host was shared and under concurrent load during collection. These are conservative
observed medians rather than isolated hardware limits.

## Results

Each time is the median of five benchmark samples. Allocation values were stable; the table uses
the median sample's values.

| Query | Median | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Full detail | 9.90 ms | 50,741,991 | 2,008 |
| Search | 18.45 ms | 25,024,869 | 43 |
| Merchant | 12.79 ms | 26,816,533 | 11,796 |
| Category | 7.99 ms | 24,922,036 | 2,023 |
| Group | 8.04 ms | 24,832,714 | 389 |
| Account | 11.59 ms | 24,834,068 | 447 |
| Time by month | 17.19 ms | 26,664,254 | 205,846 |
| Two-level drill-down | 2.18 ms | 24,837,280 | 32 |

Every representative query meets the 50 ms reference target and remains comfortably below the
500 ms interactive smoke-test budget. Detail sorting still materializes 100,000 independent rows,
search evaluates the configured regular expression against the complete corpus, and merchant
aggregation also computes top-category activity. Allocation counts for aggregate queries include
exact cross-scale amount comparisons; they remain acceptable for this simple first slice and are
recorded for later profiling rather than hidden behind a cache.
