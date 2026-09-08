# Monarch Money

Choose Monarch in the profile wizard, or run:

```bash
./bin/moneyflow provider connect monarch --profile "Example Profile"
```

The wizard collects missing import settings, account credentials and MFA information. It supports
a Base32 TOTP secret and password-protected saved credentials. A valid saved session can avoid
another credential prompt. Follow progress through import before treating setup as complete.

Go imports posted transactions, including both visible and hidden partitions. It excludes pending
bank transactions deliberately. An explicitly scoped initial import is available for faster preview
iteration; use `provider connect monarch --help` for those options and their pristine-profile rule.

## Refresh and edit

TUI/web refresh in the background on the documented cadence; `r` requests refresh. MCP refresh
is explicit. Full reconciliation has identity, pagination and deletion-plausibility checks. A
large apparent removal requires confirmation; correctness failures cannot be overridden.

Merchant/category updates, hide changes and deletion use the durable write batch after review and
commit. Taxonomy administration remains in Monarch. Provider rules can override requested fields;
completion reports overrides and subsequent refresh reconciles remote truth.

If credentials expire, use Reconnect or the CLI connection workflow. A retained profile binding
cannot silently switch to a different Monarch household. Preserve the database and vault when
recovering; never delete the shared configuration root. See [batch recovery](editing.md#a-batch-needs-attention).
