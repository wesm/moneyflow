# Develop Moneyflow

Install [mise](https://mise.jdx.dev/getting-started.html) and Make. From the repository root:

```bash
mise trust
mise install
mise exec -- make web-install build
```

`mise.toml` pins Go, Bun, Node, golangci-lint, prek, PowerShell, uv, and the Freeze capture tool.
It sets `GOTOOLCHAIN=local`, so Go uses the mise-selected compiler rather than a toolchain
inherited from your shell. Do not set a Go version in your shell configuration.
Keep the Go pin aligned with `go.mod` and the Bun pin aligned with `web/package.json` and CI.

Prefix commands below with `mise exec --`, or
[activate mise in your shell](https://mise.jdx.dev/cli/activate.html) to use them directly.
Git hooks run through mise as well. Make remains the build and test entry point.
The application has no Python or CGO requirement. Python and uv are needed only for the
[documentation website](https://github.com/wesm/moneyflow/blob/main/docs/README.md).

On Unix, set a private temporary directory for tests and demos before running the commands
below. Moneyflow rejects profile paths beneath group- or world-writable ancestors,
including the usual `/tmp`, even when the profile directory itself is private.

```bash
export TMPDIR="$(mktemp -d "$HOME/moneyflow-dev.XXXXXX")"
```

Keep this setting in the current shell. Do not change permissions on `/tmp` or disable
profile checks. Tests clean up their fixtures; after all commands finish, remove the
directory with `rmdir "$TMPDIR"` if it is empty, then `unset TMPDIR`.

## Build and try a change

```bash
make web-install
make build
make tui-demo
```

`make web-demo` runs the browser demo. Both use temporary synthetic profiles, not personal data.
The binary is `bin/moneyflow` or `bin/moneyflow.exe` on Windows.

## Verify behavior

Write a failing test before changing application behavior. Keep fixtures synthetic.
The installer tests use the PowerShell version selected by mise.

```bash
make verify-go
make test-race
make verify-web
```

`verify-go` checks formatting, Go tests, storage gates, provider workflows, vet, and lint.
`verify-web` checks the generated API, types, formatting, lint,
unit tests, dependency audit, assets, and browser journeys.

Install the pinned Playwright browsers when needed:

```bash
bun run --cwd web playwright install --with-deps chromium firefox webkit
```

For Amazon-specific work:

```bash
make test-amazon
make test-amazon-e2e
```

These cover imports, repeated imports, local edits, matching, search, and user-facing flows.
They use synthetic data and local services, not a real Amazon or bank account.

If host load makes a performance gate unreliable, rerun correctness checks with
`MONEYFLOW_SKIP_PERF=1` and disclose the exact timing failure. Do not change the threshold
just to obtain a passing build.

## Work with generated files

Use `make web-generate` when the API contract changes and review the generated diff.
`make web-embed` builds ignored frontend assets needed by Go embedding.
Do not commit `internal/web/dist` or browser screenshots.

## Send a change

Follow [AGENTS.md](https://github.com/wesm/moneyflow/blob/main/AGENTS.md) for repository instructions and
[contributing](contributing.md) for bug reports and review scope.
Use the [release guide](releases.md) for packaging; building does not publish anything.
