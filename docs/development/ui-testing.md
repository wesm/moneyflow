# Probe UI behavior with synthetic data

The UI harness runs the real application service, terminal event loop, and browser
handlers against a small synthetic account. It checks edit scope, pending versus
committed data, provider calls, recovery, and audit output. It never opens a real
provider session. These developer tools are unreleased.

## Run a scenario

Use the pinned tools and the [private temporary directory](developing.md) required
by Unix tests. Build frontend assets before browser runs.

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

## Replay and reduce a failure

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

## Add a regression

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
API and UI. `web/scripts/scenario-server.ts` owns its private root and process.
`stop` preserves both stores, `restart` reattaches the provider, and `finish` removes
successful runs while retaining failures. Test controls observe state and schedule
faults; editing actions still go through the rendered UI and production handlers.
There are no scenario controls in the production binary.

Logical application time starts at 2026-10-15 12:00 UTC. Browser scenarios also fix
browser time and timezone. Deadlines, polling intervals, and process lifetimes use
real elapsed time. TUI display formatting follows the host timezone, which the
probe records. Random IDs and wall-clock audit timestamps are not byte-for-byte
replay guarantees.

## CI and manual checks

The `go` matrix and `race` job in `.github/workflows/go.yml` run the Go scenarios.
The `web` job in `.github/workflows/web.yml` runs the Playwright scenarios, without
retries for this suite. Synthetic failure artifacts are retained for seven days.
Set `MONEYFLOW_UI_ARTIFACTS` to an absolute private directory to retain native fuzz
and Go scenario failures somewhere specific; otherwise they remain beneath the
private `TMPDIR`.

The pipe driver covers event handling, asynchronous commands, frames, and input
decoding. It does not emulate a terminal. Check terminal-specific color or repaint
issues manually in the affected terminal and tmux configuration.

The browser probe provides a repeatable starting point for a usability pass. Inspect
both themes, narrow layouts, keyboard focus, filtering, editing, review, recovery,
and empty results. Browser scenarios check currency-valued chart axes and completed
write counts as well as exact committed amounts and provider calls. Merchant editing
scenarios cover partial-match selection, explicit creation, keyboard navigation,
and preserving filtered scope through provider completion.

The browser still uses the date filter instead of the TUI time chooser. This is a
product gap, not a behavior the harness simulates.
