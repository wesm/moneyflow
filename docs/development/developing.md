# Develop Moneyflow

Set up the pinned toolchain, build `bin/moneyflow`, try a change against synthetic data, and run
the gates a commit must pass. Install [mise](https://mise.jdx.dev/getting-started.html) and Make,
then run from the repository root:

```bash
mise trust
mise install
mise exec -- make web-install build
```

`mise.toml` pins Go, Bun, Node, golangci-lint, prek, PowerShell, uv, the air live-reload tool,
and the Freeze capture tool.
It sets `GOTOOLCHAIN=local`, so Go uses the mise-selected compiler rather than a toolchain
inherited from your shell. Do not set a Go version in your shell configuration.
Keep the Go pin aligned with `go.mod` and the Bun pin aligned with `web/package.json` and CI.

Prefix commands below with `mise exec --`, or
[activate mise in your shell](https://mise.jdx.dev/cli/activate.html) to use them directly.
Make is the build and test entry point; `make help` lists the main targets. The prek hooks in
`prek.toml` run through mise too: before a commit they check formatting and run
`make test-go-quick`, and before a push they run `make vet` and `make lint`. `make install-hooks`
installs them.
The application has no Python or CGO requirement. Python and uv are needed only for the
[documentation website](https://github.com/wesm/moneyflow/blob/main/docs/README.md).

On Unix, set a private temporary directory and file-creation permissions for tests
and demos before running the commands below. Go creates test subdirectories using
the shell's umask. Moneyflow rejects profile paths beneath group- or world-writable ancestors,
including the usual `/tmp`, even when the profile directory itself is private.

```bash
umask 077
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
make web-demo
```

`make tui-demo` opens the terminal app and `make web-demo` serves the browser app on a free port
and prints its URL. Both use temporary synthetic profiles, not personal data.
The binary is `bin/moneyflow` or `bin/moneyflow.exe` on Windows.

## Live development

`make dev` runs the browser application with live reload against a synthetic profile:

```bash
make dev
```

It starts two servers on `127.0.0.1` and stops both when you press Ctrl-C:

- The Vite dev server, on port 5173, serves the frontend.
  It forwards `/api/`, `/openapi.json`, `/openapi.yaml`, and `/demo/` to Go.
- [air](https://github.com/air-verse/air) builds and runs the Go server on port 8081.

`make dev` refuses to start if either port is in use. If either server exits, it stops the
other and exits with an error. air itself keeps running when the Go server crashes, and
rebuilds on the next change. `scripts/dev.sh` runs both servers; `make dev-backend` and
`make dev-web` run one each.

Open the `Moneyflow web:` URL that the Go server prints, for example
`http://127.0.0.1:5173/p/profile_…/`. `http://127.0.0.1:5173/` shows the profile chooser.

Changes reload as follows:

- **Svelte and TypeScript:** Vite updates the open page in place, usually in under 0.2 seconds.
  Page state and pending edits are kept.
- **Go and `profile.sql`:** air rebuilds and restarts the server in about 1 second. The open
  page keeps working. Its next change gets a `token_expired` response, fetches a new token,
  and succeeds.

The first run seeds a profile in `tmp/dev-home` with synthetic data. It seeds into a staging
directory and renames it only when seeding succeeds, so a failed or interrupted run leaves
nothing behind and the next run tries again. Edits persist across restarts. Stop `make dev`,
then run `make dev-reset` to delete the profile; the next `make dev` seeds a fresh one. Do this
after changing `profile.sql`, because the dev profile keeps the schema it was created with.
Without `PROFILE`, `make dev` never uses `~/.moneyflow`. Output is also written to
`tmp/logs/backend.log` and `tmp/logs/web.log`.

The synthetic profile is named `Local`. It has no provider connection, so it opens offline.
If `tmp/dev-home` holds no profile with that name, run `make dev-reset`. To work against one of
your own profiles, such as one connected to Monarch, name it with `PROFILE`:

```bash
make dev PROFILE=Monarch
```

This opens the profile from `MONEYFLOW_HOME` when it is set, and from `~/.moneyflow`
otherwise. It does not seed `tmp/dev-home`. Before the servers start, `make dev` prints a
warning: edits are saved to that profile, and committing provider edits writes them to the
provider. If no profile has that name, the Go server lists the profiles that exist and exits
without creating one. Stop `make dev` and run it again with one of those names.

To run several worktrees at once, give each its own ports:

```bash
make dev DEV_WEB_PORT=5174 DEV_BACKEND_PORT=8082
```

The two ports must differ; `make dev` refuses to start otherwise.

The Go server runs with `--external-url` set to the Vite URL, so its mutation checks treat
Vite as the canonical origin. The Vite proxy passes the browser's `Origin` header through
unchanged. Only the Vite dev server fills the `index.html` placeholders; production builds keep
them for the Go server, and `make web-embed` rejects a build without them. Vite pages carry no
Content Security Policy, so check CSP-sensitive changes with `make web-demo`.

Limits:

- Only the root base path `/` works. `web/tests/base-path.spec.ts` covers other base paths.
- The dev home always holds the seeded profile, so first-run onboarding of an empty home does
  not appear. Adding a profile from the chooser works and stores it in `tmp/dev-home`.
- The Windows configuration in `.air.toml` is untested.

## Verify behavior

Write a failing test before changing application behavior. Keep fixtures synthetic.
The installer tests use the PowerShell version selected by mise.

| Command | Run it | Checks |
| --- | --- | --- |
| `make verify-go` | Before every commit | Formatting, Go tests, MCP, storage and provider gates, vet, and lint |
| `make test-race` | Before every commit | Go tests under the race detector |
| `git diff --check` | Before every commit | Whitespace errors |
| `make verify-web` | For browser, API, embedded-asset, or cross-interface changes | Generated API, types, formatting, lint, unit tests, dependency audit, assets, and the Playwright suite |
| `make docs-check` | For documentation changes | Markdown lint and the website build; see the [docs guide](https://github.com/wesm/moneyflow/blob/main/docs/README.md) |

[UI testing](ui-testing.md) covers the Playwright browser suite, the terminal tests, and the UI
harness that probes synthetic editing workflows, fuzzes service histories, and replays terminal
or browser failures.

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
