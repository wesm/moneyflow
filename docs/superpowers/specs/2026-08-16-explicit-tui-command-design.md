# Explicit TUI Command Design

**Status:** Approved

## Purpose

Moneyflow now has two first-class interactive interfaces. The command-line structure should name
both interfaces explicitly instead of treating the terminal user interface as the default and the
web interface as a subcommand.

This is a deliberate command-line parity break from Python Moneyflow.

## Command Contract

Running `moneyflow` with no arguments prints the standard Cobra help and exits successfully. It
does not open a profile or start an interface.

The supported interactive entry points are:

```text
moneyflow tui
moneyflow web
```

`moneyflow tui` opens the persistent default profile. `moneyflow tui --demo` opens a fresh
temporary profile seeded with synthetic data. The former bare `moneyflow` TUI behavior and the
`moneyflow demo` command are removed without compatibility aliases.

The TUI-specific `--theme` flag belongs to the `tui` subcommand. The hidden fixture test seam also
belongs to the interface subcommand that consumes it, so tests and internal tooling use
`moneyflow tui --fixture PATH` or `moneyflow web --fixture PATH`.

## Guidance

Cobra help, README examples, current user-facing Go-port documentation, and provider connection
completion output use the explicit commands. After a successful provider import, the CLI prints:

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
- `moneyflow tui --theme` validates the selected theme before starting the runner;
- `moneyflow demo` is rejected as an unknown command;
- the hidden fixture seam works independently for TUI and web commands; and
- successful Monarch connection output names the two explicit next steps.

The existing Go verification suite remains the completion gate.
