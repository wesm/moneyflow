# Explicit TUI Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `moneyflow tui` the only persistent TUI entry point while bare `moneyflow` prints
Cobra help and `moneyflow web` remains the peer browser entry point.

**Architecture:** Give the TUI its own Cobra subcommand and local flags, just as the web interface
already has. Give the root a minimal handler that delegates to Cobra help without opening a
profile; making it runnable also preserves unknown-command errors. Move the hidden fixture seam
into each interface command and keep provider summaries on standard output while sending next-step
guidance to standard error.

**Tech Stack:** Go 1.25, Cobra, Testify, Make, Markdown

## Global Constraints

- This is a deliberate command-line parity break from Python Moneyflow.
- Do not retain aliases for bare TUI startup, `moneyflow demo`, or `moneyflow --demo`.
- Do not change Python distribution invocations, Python documentation, release files, or scripts.
- `--fixture` always uses a temporary profile and remains hidden from user-facing help.
- TUI theme validation must happen before opening a profile.
- Use tests before implementation and commit each verified task without amending earlier commits.
- Run branch binaries only against isolated temporary profiles; never use the live default profile.

---

### Task 1: Make the TUI an Explicit Cobra Subcommand

**Files:**

- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/web.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `cmd/moneyflow/profile_test.go`
- Modify: `cmd/moneyflow/provider_test.go`

**Interfaces:**

- Consumes: `previewOptions(string) (tui.Options, error)`, `ProfileOptions`, `IOStreams`
- Produces: `newTUICommand(IOStreams) *cobra.Command`
- Produces: `newWebCommand(IOStreams) *cobra.Command` with a command-local hidden fixture flag

- [ ] **Step 1: Write failing root-command contract tests**

Replace the old default/demo tests in `cmd/moneyflow/root_test.go` with tests that exercise the
public command surface:

```go
func TestRootCommandWithoutArgumentsPrintsHelpWithoutOpeningProfile(t *testing.T) {
    var opens int
    var stdout bytes.Buffer
    command := newRootCommand(IOStreams{
        In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{},
        OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
            opens++
            return OpenedProfile{}, errors.New("must not open")
        },
    })

    require.NoError(t, command.Execute())
    assert.Zero(t, opens)
    assert.Contains(t, stdout.String(), "moneyflow tui --demo")
    assert.Contains(t, stdout.String(), "Available Commands:")
}

func TestTUICommandStartsPersistentAndTemporaryProfiles(t *testing.T) {
    fixturePath := filepath.Join("..", "..", "testdata", "parity", "transactions.json")
    for _, test := range []struct {
        name string
        args []string
        want ProfileOptions
    }{
        {name: "persistent", args: []string{"tui"}, want: ProfileOptions{}},
        {name: "demo", args: []string{"tui", "--demo"}, want: ProfileOptions{Demo: true}},
        {name: "fixture", args: []string{"tui", "--fixture", fixturePath}, want: ProfileOptions{Demo: true, FixturePath: fixturePath}},
        {name: "demo fixture", args: []string{"tui", "--demo", "--fixture", fixturePath}, want: ProfileOptions{Demo: true, FixturePath: fixturePath}},
    } {
        t.Run(test.name, func(t *testing.T) {
            var got ProfileOptions
            var runs int
            command := newRootCommand(IOStreams{
                In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
                OpenProfile: func(_ context.Context, options ProfileOptions) (OpenedProfile, error) {
                    got = options
                    return testProfileOpener(t)(context.Background(), options)
                },
                RunTUI: func(context.Context, *app.Service, app.Session, tui.Options, IOStreams) error {
                    runs++
                    return nil
                },
            })
            command.SetArgs(test.args)

            require.NoError(t, command.Execute())
            assert.Equal(t, test.want, got)
            assert.Equal(t, 1, runs)
        })
    }
}

func TestTUIThemeValidationPrecedesProfileOpen(t *testing.T) {
    var opens int
    command := newRootCommand(IOStreams{
        In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
        OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
            opens++
            return OpenedProfile{}, nil
        },
    })
    command.SetArgs([]string{"tui", "--theme", "missing"})

    err := command.Execute()
    require.ErrorContains(t, err, "unknown theme")
    assert.Zero(t, opens)
}

func TestRemovedAndMisScopedCommandsAreRejected(t *testing.T) {
    for _, args := range [][]string{
        {"demo"},
        {"--demo"},
        {"web", "--theme", "nord"},
    } {
        command := newRootCommand(IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
        command.SetArgs(args)
        assert.Error(t, command.Execute(), args)
    }
}
```

Update command executions in `cmd/moneyflow/profile_test.go` and the TUI case in
`cmd/moneyflow/provider_test.go` so persistent TUI runs pass `[]string{"tui"}`. Change the profile
matrix to use `tui`, `tui --demo`, `tui --fixture PATH`, `web --demo`, and `web --fixture PATH`.

- [ ] **Step 2: Run the focused command tests and verify RED**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./cmd/moneyflow -run 'Test(RootCommandWithoutArguments|TUICommand|TUITheme|RemovedAndMisScoped|CommandsOpenExpected|DefaultCommand|ProviderRuntime)' -count=1
```

Expected: FAIL because bare `moneyflow` still opens a profile, `tui` does not exist, root flags
remain global, and existing lifecycle tests still invoke the old command surface.

- [ ] **Step 3: Add the TUI command and localize interface flags**

In `cmd/moneyflow/root.go`, make the root delegate to Cobra help and add a focused command
constructor:

```go
func newTUICommand(streams IOStreams) *cobra.Command {
    var theme string
    var fixturePath string
    var demo bool
    command := &cobra.Command{
        Use:   "tui",
        Short: "Open the terminal application",
        Args:  cobra.NoArgs,
        RunE: func(command *cobra.Command, _ []string) error {
            options, err := previewOptions(theme)
            if err != nil {
                return fmt.Errorf("start TUI: %w", err)
            }
            opener := streams.OpenProfile
            if opener == nil {
                opener = openProfile
            }
            opened, err := opener(command.Context(), ProfileOptions{
                Demo: demo || fixturePath != "", FixturePath: fixturePath,
            })
            if err != nil {
                return fmt.Errorf("start TUI: %w", err)
            }
            if err = configureOpenedMonarchProvider(command.Context(), opened, streams, "tui"); err != nil {
                return fmt.Errorf("start TUI: %w", closeOpenedProfile(opened, err))
            }
            runner := streams.RunTUI
            if runner == nil {
                runner = func(ctx context.Context, service *app.Service, session app.Session, options tui.Options, streams IOStreams) error {
                    return tui.Run(ctx, service, session, options, streams.In, streams.Out)
                }
            }
            return closeOpenedProfile(opened, runner(command.Context(), opened.Service, app.NewSession(), options, streams))
        },
    }
    command.Flags().StringVar(&theme, "theme", string(tui.ThemeDefault), "color theme")
    command.Flags().BoolVar(&demo, "demo", false, "open a temporary profile seeded with synthetic data")
    command.Flags().StringVar(&fixturePath, "fixture", "", "fixture document")
    if err := command.Flags().MarkHidden("fixture"); err != nil {
        panic(err)
    }
    return command
}
```

Keep the existing close-error behavior rather than simplifying it if the direct `return` would
lose an error from both the runner and profile close. In `newRootCommand`, replace its TUI `RunE`
with a handler that returns `command.Help()`, remove the root persistent flags and `demo` command,
and add `newTUICommand(streams)`. Lead the Cobra examples with `moneyflow tui --demo`.

In `cmd/moneyflow/web.go`, change the constructor to `newWebCommand(streams IOStreams)`, declare a
local `fixturePath string`, add and hide the local `--fixture` flag, and open profiles with:

```go
ProfileOptions{Demo: demo || fixturePath != "", FixturePath: fixturePath}
```

- [ ] **Step 4: Run focused command and lifecycle tests and verify GREEN**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./cmd/moneyflow -count=1
```

Expected: PASS.

- [ ] **Step 5: Review and commit the command topology**

Run `gofmt` on the five changed Go files, inspect `git diff --check`, then commit only these files:

```bash
git add cmd/moneyflow/root.go cmd/moneyflow/root_test.go cmd/moneyflow/web.go cmd/moneyflow/profile_test.go cmd/moneyflow/provider_test.go
git commit -m "feat: make TUI startup explicit"
```

### Task 2: Update Provider Guidance and Go-Port Entry Points

**Files:**

- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `README.md`
- Modify: `Makefile`

**Interfaces:**

- Consumes: successful `runMonarchConnect` output and the new `moneyflow tui` command
- Produces: one import-summary line on standard output and one next-step line on standard error

- [ ] **Step 1: Write failing provider stream assertions**

In `cmd/moneyflow/provider_test.go`, tighten the existing successful first-connect and month-to-date
tests and the retained-session test:

```go
assert.Equal(t, "Imported 1 posted transaction.\n", stdout)
assert.Contains(t, stderr, "Run moneyflow tui or moneyflow web to continue.\n")
```

For month-to-date, assert the exact standard output is
`"Imported 1 posted month-to-date transaction.\n"` and the same standard-error guidance. In
`TestProviderConnectRetriesRetainedValidSessionWithoutPrompts`, capture both streams and assert the
guidance is present after its successful import.

- [ ] **Step 2: Run provider tests and verify RED**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./cmd/moneyflow -run 'TestProviderConnect(CreatesCurrentSchema|MonthToDateSeeds|RetriesRetained)' -count=1
```

Expected: FAIL because successful connect does not print next-step guidance.

- [ ] **Step 3: Print unconditional post-connect guidance to standard error**

After the existing import summary in `cmd/moneyflow/provider.go`, preserve output errors and write:

```go
if _, err = fmt.Fprintf(command.OutOrStdout(), "Imported %d posted %s%s.\n", count, scope, noun); err != nil {
    return err
}
_, err = fmt.Fprintln(command.ErrOrStderr(), "Run moneyflow tui or moneyflow web to continue.")
return err
```

Because this runs after every successful refresh result, it covers first connection, `--mtd`, and
retained-session reconnects without branching.

- [ ] **Step 4: Update only Go-port documentation and build targets**

Change the two Go v2 README examples from `./bin/moneyflow` to `./bin/moneyflow tui`. Change the
Makefile target to:

```make
tui-demo: build
    $(BINARY) tui --demo
```

Do not alter Python-facing `moneyflow --demo` examples or `web/scripts/e2e-server.ts`.

- [ ] **Step 5: Run focused verification and inspect command help**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./cmd/moneyflow -count=1
go run ./cmd/moneyflow --help
make -n tui-demo
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: tests pass; help leads with `moneyflow tui --demo`; the dry-run target ends with
`bin/moneyflow tui --demo`; Markdown checks pass.

- [ ] **Step 6: Run repository completion gates**

Run:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
make build
```

If the unrelated 100k provider-refresh timing gate again exceeds its 1-second ceiling, record the
measured value, run `MONEYFLOW_SKIP_PERF=1 make verify-go`, and do not describe the timing gate as
passing. All functional, formatting, vet, lint, parity, Python, and build checks must pass.

- [ ] **Step 7: Isolated CLI smoke test**

Use a fresh temporary `MONEYFLOW_HOME` and verify that bare startup does not create a profile:

```bash
isolated_root=$(mktemp -d)
MONEYFLOW_HOME="$isolated_root" ./bin/moneyflow > /tmp/moneyflow-help.txt
test ! -e "$isolated_root/moneyflow.db"
rg 'moneyflow tui --demo|Available Commands:' /tmp/moneyflow-help.txt
```

Do not start the interactive TUI in an automated smoke test. The command and profile lifecycle are
covered by the injected-runner tests.

- [ ] **Step 8: Review, privacy-scan, and commit the guidance changes**

Inspect `git status --short`, `git diff --stat`, `git diff HEAD`, and `git diff --check`. Scan the
pending README and source changes for private terms, absolute home paths, personal email addresses,
tailnet names, and financial data. Then commit only the task files:

```bash
git add cmd/moneyflow/provider.go cmd/moneyflow/provider_test.go README.md Makefile
git commit -m "docs: direct users to explicit interfaces"
```

Do not push or amend either commit.
