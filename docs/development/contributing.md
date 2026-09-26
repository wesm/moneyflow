# Contribute to Moneyflow

Report bugs with reproduction steps, the Moneyflow version, and your operating system.
Use synthetic examples. Do not attach real transactions, provider credentials, profile
databases, or unsanitized screenshots.

For a code change:

1. Read [AGENTS.md](https://github.com/wesm/moneyflow/blob/main/AGENTS.md) and the relevant owning guide.
2. Set up the [Go and web development tools](developing.md).
3. Add a focused behavior test, observe its failure, and implement the change.
4. Run the relevant verification commands and update the owning documentation.
5. Describe the user-visible result and any remaining limitations in the pull request.

Keep changes scoped. Shared behavior belongs in the application service, not duplicated
in TUI and web. Documentation changes follow [the publishing guide](https://github.com/wesm/moneyflow/blob/main/docs/README.md).
