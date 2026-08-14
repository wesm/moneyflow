# Go Read-Only Web Slice Benchmark

The complete read-only Go web projection and production frontend were measured on 2026-08-13.
Every measurement used deterministic synthetic data; no profile, provider, or personal finance
data was loaded.

## Environment

- OS: macOS 26.5.2, arm64
- CPU: Apple M5 Max
- Memory: 128 GiB
- Go: 1.26.5
- Bun: 1.3.14
- Playwright: 1.61.1
- Chromium: 149.0.7827.55
- Source: `v0.11.1-36-g4c9b5f7-dirty` (Task 15 working tree on base `4c9b5f7`)

The host was otherwise idle during the Go benchmark samples. Browser timings use the embedded Go
server and production frontend on loopback with reduced motion enabled.

## 100,000-Transaction Go Projection

The API benchmark decodes the durable URL, queries the complete 100,000-row corpus, builds the
200-row table and chart projection, converts exact money values to the wire contract, and encodes
the complete JSON response.

Command:

```bash
go test ./internal/api -run '^$' -bench BenchmarkProjection100K -benchmem -count=5
```

| Pipeline | Median | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Complete API projection | 13.73 ms | 27,453,647 | 21,004 |

The median is well below the 100 ms reference target. The committed smoke test permits one second
on shared CI and skips only when `MONEYFLOW_SKIP_PERF=1` is explicitly set by the race-detector job.

The existing analytics benchmark was repeated with the same five-sample method:

```bash
go test ./internal/analytics -run '^$' -bench Benchmark -benchmem -count=5
```

| Query | Median | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Full detail | 11.41 ms | 50,741,995 | 2,008 |
| Search | 20.85 ms | 25,026,694 | 43 |
| Merchant | 17.13 ms | 26,816,553 | 11,796 |
| Category | 11.93 ms | 24,922,048 | 2,023 |
| Group | 10.36 ms | 24,832,721 | 389 |
| Account | 11.87 ms | 24,834,063 | 447 |
| Time by month | 22.82 ms | 26,664,281 | 205,846 |
| Two-level drill-down | 3.57 ms | 24,837,280 | 32 |

No optimization was justified: the complete API path and every analytics query remain below their
reference targets.

## Frontend Asset Budgets

The budget checker follows the Vite entry's complete static import closure and sums each file after
independent Brotli quality-11 or gzip level-9 compression. Dynamic imports are not initial assets.
Each ceiling is the next 10 KiB boundary above 125% of the measured total.

| Initial asset group | Measured | Budget |
| --- | ---: | ---: |
| CSS, Brotli | 11,661 bytes | 20,480 bytes |
| CSS, gzip | 13,707 bytes | 20,480 bytes |
| JavaScript, Brotli | 154,967 bytes | 194,560 bytes |
| JavaScript, gzip | 179,004 bytes | 225,280 bytes |

The uncompressed entry files were 334,188 bytes of JavaScript and 75,801 bytes of CSS. Source maps
remain disabled and forbidden by distribution validation.

## Browser Interaction Budgets

Playwright dispatched 100 real chart activations against the embedded application. The cursor
measurement starts at the chart event and ends when the linked pressed state changes. The drill
measurement ends when the server-authoritative durable URL transition is applied. Each ceiling is
the next 10 ms boundary above 125% of the observed p95.

| Interaction | Samples | p95 | Budget |
| --- | ---: | ---: | ---: |
| Chart cursor linkage | 100 | 24.5 ms | 40 ms |
| Chart drill transition | 100 | 16.4 ms | 30 ms |

The Playwright performance test enforces these interaction ceilings. `make web-check` enforces the
asset ceilings after a fresh production build.
