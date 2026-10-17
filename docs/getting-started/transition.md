# Moving from Python to Go

Go Moneyflow replaces the Python app. It can't open Python profiles, so you start with new Go
profiles and fill them from your providers and files. Your Python data stays where you put it,
untouched.

## What carries over

| Your Python data | In Go |
| --- | --- |
| Monarch and YNAB transactions, and edits you already sent to them | Come back when you reconnect the provider |
| Amazon order history | Comes back when you reimport the original CSV files |
| Chase bank CSV transactions | Come back when you reimport the original CSV files |
| SimpleFIN transactions | Come back when you reconnect; available history may differ |
| Local-only edits: Amazon and CSV merchant names, categories, hidden rows, deletions | Do not carry over; reapply them in Go |
| Unsent Python edits | Do not carry over |

## Before you switch

1. Stop the Python app and back up its whole data directory.
2. Commit the Python edits you want to send to Monarch or YNAB. Those come back through the
   provider.
3. Write down or [export](../guide/export.md) any local-only changes you want to reapply.
4. Find your original Amazon and bank CSV files. You need them to reimport.

## Move Python data out of `~/.moneyflow`

Go keeps its profiles in `~/.moneyflow` (or `MONEYFLOW_HOME`), the same directory the Python
app used. They can't share it. Go refuses a directory with a top-level `accounts.json`,
`config.yaml`, `credentials.enc`, `merchants.json`, or `moneyflow.log`. It stops before
changing anything, and the error names the directory and the command that moves it.

Move the directory yourself:

1. Stop every Moneyflow process, Python and Go.
2. Move the Python data aside:

   ```bash
   mv ~/.moneyflow ~/.moneyflow-python
   ```

3. If you tried a Go build that kept profiles in `~/.moneyflow/v2`, move them back:

   ```bash
   mv ~/.moneyflow-python/v2 ~/.moneyflow
   ```

   Those builds named every new profile "Moneyflow".
   [Rename it](go.md#rename-a-profile) after its provider if you like:

   ```bash
   moneyflow profile rename --profile Moneyflow --name Monarch
   ```

Keep `~/.moneyflow-python` until you have checked your Go profiles.

## Rebuild your profiles

1. Run `moneyflow web` or `moneyflow tui` and choose **Add profile**.
2. Reconnect [Monarch](../guide/monarch.md), [YNAB](../guide/ynab.md), or
   [SimpleFIN](../guide/simplefin.md).
3. Reimport your [Amazon CSV files](../guide/amazon-mode.md) and
   [Chase CSV files](../guide/bank-csv.md).
4. Check dates, totals, categories, hidden rows, and Amazon matches against your Python data.

## Check which program runs

Both apps are called `moneyflow`. Run `command -v moneyflow` on Linux and macOS, or
`Get-Command moneyflow` in PowerShell, to see which one your shell finds. The Go app prints its
version with `moneyflow version`.

Go uses subcommands: `moneyflow tui`, `moneyflow web`, and `moneyflow mcp`. The Python
commands `moneyflow --demo`, `moneyflow amazon`, and `moneyflow simplefin` do not exist in Go.
Use `moneyflow tui --demo` and the [command reference](../reference/cli.md).

## Transfer a Go profile with JSONL

Use JSONL transfer to copy a Go profile to another machine or catalog, or to carry it to a
release that can't open the old database. Export writes one JSON record per line; import
creates a new profile from the file.

1. Commit or undo pending edits. Finish or reconcile unfinished provider writes.
2. Close every Moneyflow process that uses the profile: web, TUI, and MCP.
3. Export with a binary that can open the profile:

   ```bash
   moneyflow profile export --profile "Example Profile" --output profile.jsonl
   ```

4. Import with the destination binary:

   ```bash
   moneyflow profile import --input profile.jsonl --name "Imported Profile"
   ```

5. Compare the reported record counts and totals per currency. Open the new profile and check
   categories, hidden rows, and Amazon matches.
6. Reconnect providers in the new profile. Keep the source profile and the file until you
   have checked the result.

To move between catalogs, set `MONEYFLOW_HOME` separately for each command. Import always
creates a new profile ID and refuses a name that is already in use. It checks the saved counts
and totals before it publishes the profile. Other profile commands wait while an import runs;
if one reports that the catalog is busy, retry when the import finishes.

The file holds **unencrypted financial data**, including notes and provider record IDs. Don't
attach it to public issues. Export makes the file readable only by you and never overwrites an
existing file. The parent directory must already exist, and its permissions are left alone. On
filesystems that can't create the file without replacing one, export fails and removes its
temporary file.

**Transferred:** committed edits, retired and merged entities, deletion records, provider
identities, YNAB split details and write restrictions, Amazon import data and local edits, and
bank CSV source identities, local overrides, latest file records and file-to-row links. A later
CSV reimport reconciles against the transferred data.

**Not transferred:** credentials, tokens, undo and redo history, write batches, scheduler
state, and past import logs. Export reports inactive redo steps it left out, without removing
them from the source. Import adds any missing system categories and reports them separately.

Amazon's category hierarchy is kept, but its link to a taxonomy-source profile is not. Set
that link again in the new catalog if you need it.

**Limits:** 512 MiB per file, 1 MiB per record, and 1,000,000 records in total. The count
includes accounts, categories, provider metadata, Amazon items, and CSV source and file
records, not only transactions. A larger profile can't be exported; Moneyflow doesn't truncate
or split it. Import rejects unknown fields or versions, incomplete files, duplicate records,
invalid references, and inconsistent amounts.

## When a release can't open a profile

Each release creates one current database schema and refuses profiles it can't read. It never
migrates a profile in place. Choose **Start fresh** to set up a separate profile and keep the
old files, or export the profile with the older binary and import it with the new one. See
[incompatible profiles](../reference/troubleshooting.md#the-profile-schema-is-incompatible).

Export before you upgrade: a newer binary can't export a schema it doesn't understand. Keep a
backup and the older binary. Don't delete database, WAL, or SHM files while Moneyflow runs.
