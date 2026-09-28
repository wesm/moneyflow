# Develop Moneyflow

Use Go 1.27.1, Bun 1.3.14, and Make. The application has no Python or CGO requirement.
Python and uv are needed only for the [documentation website](https://github.com/wesm/moneyflow/blob/main/docs/README.md).
Use golangci-lint 2.14.0 for lint checks, matching CI's Go 1.27-compatible build.

On Unix, set a private temporary directory for tests and demos before running the commands
below. Moneyflow rejects profile paths beneath group- or world-writable ancestors,
including the usual `/tmp`, even when the profile directory itself is private.

```bash
export GOTOOLCHAIN=go1.27.1
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
If PowerShell is on `PATH`, it must run successfully: the installer tests exercise it.
A version-manager shim without a selected version is not a working installation.

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
