# Explicit TUI Command Design

**Status:** Approved

## Purpose

Moneyflow now has two first-class interactive interfaces. The command-line structure should name
both interfaces explicitly instead of treating the terminal user interface as the default and the
web interface as a subcommand.

This is a deliberate command-line parity break from Python Moneyflow.

## Command Contract

Running `moneyflow` with no arguments prints the standard Cobra help and exits successfully. It
does not open a profile or start an interface. Cobra provides this behavior when the root command
has subcommands but no runnable handler; Moneyflow does not add a custom help handler.

The supported interactive entry points are:

```text
moneyflow tui
moneyflow web
```

`moneyflow tui` opens the persistent default profile. `moneyflow tui --demo` opens a fresh
temporary profile seeded with synthetic data. The former bare `moneyflow` TUI behavior and the
`moneyflow demo` command are removed without compatibility aliases. The likely legacy invocation
`moneyflow --demo` fails with Cobra's unknown-flag error and a nonzero exit status.

The TUI-specific `--theme` flag belongs to the `tui` subcommand. The hidden fixture test seam also
belongs to the interface subcommand that consumes it, so tests and internal tooling use
`moneyflow tui --fixture PATH` or `moneyflow web --fixture PATH`. Passing `--fixture` always selects
a temporary profile; combining `--demo --fixture PATH` has the same profile behavior as passing
`--fixture PATH` alone. Root and web commands reject the TUI-only `--theme` flag.

## Guidance

Cobra help and the Go-port examples at `README.md:82` and `README.md:106` use the explicit command.
The `tui-demo` Make target runs `$(BINARY) tui --demo`, and the root Cobra example leads with
`moneyflow tui --demo`.

The Python distribution keeps its existing command line. Invocations of `moneyflow --demo` in
Python documentation, release files, package tests, and scripts do not change. The existing web
test server already uses `moneyflow web --demo` and also remains unchanged.

After every successful provider connection, including month-to-date imports and connections that
reuse a retained session, the CLI keeps the import summary as the only standard-output line and
prints this guidance to standard error:

```text
Run moneyflow tui or moneyflow web to continue.
```

Historical design documents and implementation plans remain unchanged because they record the
decisions in effect when those slices were designed.

## Verification

Command tests prove that:

- bare `moneyflow` prints help, exits successfully, and never opens a profile;
- `moneyflow tui` opens the persistent profile and runs the TUI;
- `moneyflow tui --demo` opens a temporary seeded profile;
- `moneyflow tui --theme` validates the selected theme before opening the profile;
- `moneyflow demo` is rejected as an unknown command;
- `moneyflow --demo` and `moneyflow web --theme` are rejected as invalid invocations;
- the hidden fixture seam works independently for TUI and web commands; and
- successful Monarch connection output keeps the summary on standard output and names the two
  explicit next steps on standard error.

The existing Go verification suite remains the completion gate.
