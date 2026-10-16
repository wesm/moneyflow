# Test the browser and terminal UIs

Moneyflow has three layers of UI tests, all on synthetic data. Playwright drives the real
browser app against a Go test server. Go tests drive the terminal app's Bubble Tea model with
key messages. The UI harness runs scripted and fuzzed editing scenarios through the application
service, the TUI, and the browser, and keeps replayable evidence when one fails. None of them
opens a real provider session.

Use the pinned tools and the [private temporary directory](developing.md) that Unix tests
require.

## Browser tests

The Playwright suite lives in `web/tests/*.spec.ts`. `web/playwright.config.ts` defines three
projects: `chromium`, `firefox`, and `webkit`. Tests tagged `@smoke` in their title run on every
engine; the rest run on Chromium only.

`make web-e2e` runs, in order:

1. The full Chromium suite, except the chart performance test.
2. The `@smoke` tests on Firefox.
3. The `@smoke` tests on WebKit.
4. `performance.spec.ts` on Chromium, alone, with one worker, so other workflows do not compete
   with its measurements.

Before the workers start, Playwright's global setup builds `cmd/moneyflow`,
`internal/tools/webtestserver`, and `internal/tools/seedprofile` once per invocation, and
removes them afterward. Each worker starts its own demo server; editing and provider tests
create fresh servers and profiles. CI uses two workers; local runs use half the logical CPU
count, up to 32. CI retries a failed test twice, except `scenario.spec.ts`, which never
retries.

Install the pinned browsers once, then run a focused file after `make web-embed`:

```bash
bun run --cwd web playwright install --with-deps chromium firefox webkit
bun run --cwd web test:e2e -- editing.spec.ts --project=chromium
bun run --cwd web test:e2e -- keyboard.spec.ts --project=firefox --grep @smoke
bun run --cwd web test:e2e -- performance.spec.ts --project=chromium --workers=1
```

`make verify-web` builds frontend assets once and runs each browser test once. The focused
targets are useful during development and are already covered by `make verify-web`:

| Target | Runs |
| --- | --- |
| `make test-editing-e2e` | `test-editing-go`, then the base-path, editing, managers, origin, restart, and review specs on Chromium |
| `make test-provider-e2e` | The provider and refresh specs on Chromium |
| `make test-amazon-e2e` | The Amazon import spec on Chromium |

Find elements by role and accessible name, not by class or position, as the
[chart contracts](#chart-contracts) below do. Tag a test `@smoke` when a browser engine
difference could break it, such as focus, keyboard, or scrolling behavior.

## Terminal tests

The terminal app's tests are ordinary Go tests in `internal/tui`. They build a `Model` over a
temporary SQLite profile, send it key messages with helpers such as `press` and `keyRune`, and
assert on model state, service results, and rendered text from `RenderScreen()`. Shell tests
cover the profile selector, provider choice, onboarding forms, and recovery the same way.

```bash
go test ./internal/tui -count=1
make test-editing-go
```

`make test` and `make test-race` include these tests. `make test-editing-go` runs the editing,
identity, restart, deletion, duplicate, drill, privacy, and export tests across `internal/app`,
`internal/tui`, and `internal/api`.

For a scenario that needs the real event loop, use `internal/uitest/tuidriver`, described in
[add a regression](#add-a-regression). Neither layer emulates a terminal; check color and
repaint issues manually, as [CI and manual checks](#ci-and-manual-checks) describes.

## UI harness

The harness runs the real application service, terminal event loop, and browser handlers
against a small synthetic account. It checks edit scope, pending versus committed data,
provider calls, recovery, and audit output. These developer tools are not part of the released
binary.

### Run a scenario

Build frontend assets before browser runs.

```bash
make test-ui-scenarios
make fuzz-ui
make ui-probe UI_PROBE_ARGS='--seed 7'
make ui-probe UI_PROBE_ARGS='--layer tui --seed 1'
make ui-probe-web UI_PROBE_ARGS='--seed 1'
```

`test-ui-scenarios` runs Go scenarios and Playwright on the configured browsers.
`fuzz-ui` spends 30 seconds generating service histories, with two workers. Go
saves failing fuzz inputs in its usual corpus. Each failing history also retains
expanded actions so it can be replayed without reproducing the random generator.

The probe commands create a fresh directory under `.cache/ui-harness/` and print
its path and replay command. Seeds choose merchant, category, or hide journeys;
the service campaign also varies undo, redo, reopen, and uncertain writes. Use
`--headed` with the browser probe to watch its actions. A headless run retains
screenshots and a Playwright trace for inspection.

### Replay and reduce a failure

Copy the printed replay command. Each replay creates fresh local and simulated
provider state; it does not continue the failed account. For example:

```bash
make ui-probe UI_PROBE_ARGS='--replay /absolute/run/actions.jsonl --reduce'
make ui-probe UI_PROBE_ARGS='--layer tui --replay /absolute/run/actions.jsonl'
make ui-probe-web UI_PROBE_ARGS='--replay /absolute/run/actions.jsonl'
```

Service reduction tries at most 24 shorter histories. It accepts only the original
assertion identity, such as an incorrect target set or an unexpected full download.
A missing setup step or timeout does not qualify. `reduced.jsonl` keeps the smaller
sequence; the original actions and candidate runs remain available. UI replay is
supported, but automatic minimization of UI timeouts is not.

| Artifact | Contents |
| --- | --- |
| `run.json` | Fixture, layer, seed, clock and runtime metadata |
| `actions.jsonl` | Exact service operations or UI input actions |
| `checkpoints.jsonl` | Service observations and the first failed assertion |
| `frames.jsonl`, `terminal.ansi` | Terminal frames, data observations, actual renderer output |
| `steps.jsonl`, `observation-*.json` | Browser actions and committed/pending/provider observations |
| `screen-*.png`, `failure.png`, `trace.zip` | Browser captures and trace |
| `state/`, `server/home/` | Isolated SQLite profile and separate simulated provider state |

All fixture values are synthetic. Keep personal exports, screenshots, credentials,
and provider logs out of harness roots. Probe artifacts stay ignored and are not
committed. Remove a run directory when it is no longer useful.

### Add a regression

`internal/uitest` owns the hand-reviewed fixture and synthetic provider. Its seven
transactions distinguish two visible September source rows from a hidden row,
an earlier year, another month, another account, and a destination merchant.
Expected targets use literal external IDs; tests must not derive them with the
production filter or mutation planner.

`internal/uitest/runtime` composes the real SQLite store and service. Its simulator
preserves omitted fields, rejects missing targets, records calls, and persists
applied writes before returning an unknown outcome. Faults can reject, block,
or apply and block one update. Concurrent calls may still complete. Assertions
should check effects and request budgets, not impose an artificial call order.

`internal/uitest/tuidriver` feeds terminal bytes into Bubble Tea through a pipe.
Bubble Tea executes commands normally. Wait for a named visible state rather than
sleeping or assuming the program becomes globally idle. Closing the driver cancels
the model and waits for its provider worker through the service pause contract.
This is graceful teardown; browser restart scenarios use a killed process to test
an abrupt exit.

`internal/tools/webtestserver --scenario` is a test-only composition of the production
API and UI. `web/scripts/scenario-server.ts` owns the process and its private root
under `TMPDIR`. `stop` preserves both stores, `restart` reattaches the provider,
and `finish` removes successful runs while copying closed stores into failure
artifacts. Test controls observe state and schedule faults; editing actions still
go through the rendered UI and production handlers.
There are no scenario controls in the production binary.

Logical application time starts at 2026-10-15 12:00 UTC. Browser scenarios also fix
browser time and timezone. Deadlines, polling intervals, and process lifetimes use
real elapsed time. TUI display formatting follows the host timezone, which the
probe records. Random IDs and wall-clock audit timestamps are not byte-for-byte
replay guarantees.

## CI and manual checks

The `go` matrix and the `race` job in `.github/workflows/go.yml` run the Go scenarios and the
terminal tests. The `web` job in `.github/workflows/web.yml` runs `make web-e2e`, including the
Playwright scenarios. Synthetic failure artifacts are kept for seven days. Set
`MONEYFLOW_UI_ARTIFACTS` to an absolute private directory to keep native fuzz and Go scenario
failures somewhere specific; otherwise they stay under the private `TMPDIR`.

The pipe driver covers event handling, asynchronous commands, frames, and input decoding. It
does not emulate a terminal. Check terminal-specific color or repaint issues manually in the
affected terminal and tmux configuration.

The browser probe gives a repeatable starting point for a usability pass. Inspect both themes,
narrow layouts, keyboard focus, filtering, editing, review, recovery, and empty results. One
browser difference remains: the period chooser picks dates from calendars and lists, and does
not accept the typed dates that the TUI time chooser does. This is a product gap, not a
behavior the harness simulates. Browser scenarios check completed and discarded write results
as well as exact committed amounts and provider calls.

## Chart contracts

Browser tests find the charts by role and accessible name, not by class or position.
Amounts in names come from the server's `display` strings or from `formatMinor`, so
tests compare them with literal synthetic values.

| Element | Role and name |
| --- | --- |
| Chart row | Region `Charts`; the `Charts` switch shows or hides it, and the KPI strip stays |
| Phone drawer | Dialog `Moneyflow visualizations`, holding the KPI strip and the chart row |
| KPI strip | Description list of terms such as `Income`, `Spending`, `Net`, `Savings rate`, `Avg per transaction`, and `Transactions` |
| Cash flow | Group `Cash flow`; one button per period named `{label}, in {in}, out {out}, net {net}` |
| Composition | List named by its panel title, such as `Where it went`; one button per bar named `{label}, {share}, {net}`, where `{share}` ends in `%` and `{net}` is signed |
| Heatmap | Group `Daily spending`; one cell per day named `{Mon D, YYYY}, out {out}`, with `, in {in}` added when In is not zero |

Cash-flow labels for days are ISO dates in the response. The axis shows the day number,
with the month where it changes and the year where that changes, as `Jan 1`, `10`, `Feb 2`,
and `Jan 2025`. The button name shows the date, as `Sep 15, 2026, in …`. Month and year
labels come from the server.

When labels would crowd, the axis labels every few columns. It picks the smallest step at
which each label, measured in the axis font, keeps 6 px from the next and stays inside the
chart; a label at an edge moves inward. Month steps divide twelve, and day steps count from
the first day of a new year, so the label that names the year always shows.

A double-click on a composition bar drills into its row. The performance spec times that
drill and the cursor's move to a new cash-flow column; their limits are in
`web/budgets.json`. That spec runs with Playwright tracing off, because tracing snapshots the
page inside the timed window. Every other spec keeps the configured tracing.

The `chart_drill` limit is 70 ms. It is sized for GitHub's ubuntu runners. The earlier 30 ms
limit was never met there: the chart this one replaced measured 31–37 ms. The drill now draws
the transaction view's calendar heatmap in the same task. The limit comes from 4x
CPU-throttled local runs, which measure a little above those runners, plus headroom.
