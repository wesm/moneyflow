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
| Full detail | 74.21 ms | 50,742,000 | 2,008 |
| Search | 75.99 ms | 25,027,640 | 43 |
| Merchant | 52.28 ms | 26,637,793 | 4,253 |
| Category | 18.07 ms | 24,878,689 | 223 |
| Group | 17.00 ms | 24,824,155 | 69 |
| Account | 20.23 ms | 24,824,164 | 69 |
| Time by month | 38.90 ms | 26,567,145 | 201,763 |
| Two-level drill-down | 5.26 ms | 24,837,284 | 32 |

Category, group, account, time, and drill-down queries meet the 50 ms reference target. Full detail,
regular-expression search, and merchant aggregation miss that aspirational target, but all
representative queries remain below the 500 ms interactive smoke-test budget. Detail sorting
materializes 100,000 independent rows, search evaluates the configured regular expression against
the complete corpus, and merchant aggregation also computes top-category activity. These costs are
recorded for later profiling; this slice keeps the simple public query contracts intact.
