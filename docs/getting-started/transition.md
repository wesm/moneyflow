# Moving from Python to Go

Go Moneyflow moves data between profiles with JSONL export and import into a fresh profile.
JSONL stores one JSON record per line. Moneyflow never upgrades an existing database in place
and keeps no chain of database migrations.

**Go-to-Go transfer is available in this checkout. The Python source exporter is not
implemented yet.** Reconnecting providers and reimporting Amazon files do not transfer
Python-only edits. Until that bridge exists, keep the Python application and its data, moved
to `~/.moneyflow-python` as described below. Run `moneyflow profile --help` to see whether
your installed build includes transfer.

## What the preview supports today

Go Moneyflow uses separate profiles. It does not import, overwrite, move, or delete your Python
profiles, credentials, caches, Amazon database, or local edits. The steps below let you try the
preview; they are not the completed cutover process.

The last Python implementation stays in Git history and in previously published Python
packages; see [Python v1](python-transition.md) to run it. The Go installer replaces a binary,
not your data. Keep the old application until you have checked the new profiles.

## Before you switch

1. Stop the Python application and back up its complete data directory.
2. Review pending Python edits. Commit only changes you intend to send to a provider.
3. Export any local-only changes you need to keep for reference.
4. Keep any original Amazon and bank CSV exports you still have. Missing files do not stop
   you from trying Go, but they can limit recovery of saved Python state.

Go does not import Python export files as profiles. The current
[transaction exports](../guide/export.md) are analysis files, not the JSONL cutover format or
full profile backups.

## Move Python data out of `~/.moneyflow`

Go keeps its catalog in `~/.moneyflow` by default; `MONEYFLOW_HOME` selects another folder.
The Python app used `~/.moneyflow` too, and the two cannot share a folder. Go refuses any
catalog folder that holds a top-level `accounts.json`, `config.yaml`, `credentials.enc`,
`merchants.json`, or `moneyflow.log`. It stops before creating or changing anything there,
and its error names the folder and the command that moves it aside.

Moneyflow never moves your data. Move the folder yourself:

1. Stop every Moneyflow process, Python and Go.
2. Move the Python data aside:

   ```bash
   mv ~/.moneyflow ~/.moneyflow-python
   ```

3. Earlier Go builds kept their catalog in `~/.moneyflow/v2`, which is now inside the moved
   folder. If you used one, move that catalog to the current location:

   ```bash
   mv ~/.moneyflow-python/v2 ~/.moneyflow
   ```

Keep `~/.moneyflow-python` until the Python exporter exists.

Earlier Go builds named a new profile "Moneyflow". To name it after its provider instead,
[rename it](go.md#rename-a-profile):

```bash
moneyflow profile rename --profile Moneyflow --name Monarch
```

## Transfer a Go profile with JSONL

1. Commit or undo active edits. Finish or reconcile unfinished provider writes.
2. Close the TUI, web, MCP, and any other Moneyflow process using the source profile.
3. Export it with a binary that understands the source database:

   ```bash
   moneyflow profile export --profile "Example Profile" --output profile.jsonl
   ```

4. Create a new profile with the destination binary:

   ```bash
   moneyflow profile import --input profile.jsonl --name "Imported Profile"
   ```

5. Compare the reported record counts and exact totals by currency and scale. Open the
   imported profile and check categories, hidden rows, and Amazon matches.
6. Authorize provider access separately. Keep the source and the export until you have
   checked the result.

To move between catalogs, set `MONEYFLOW_HOME` separately for each command. The importer always
creates a new profile ID and refuses a duplicate display name. It checks the saved counts and
totals through the application before it publishes the new profile. Other profile commands
wait while the importer fills the database; a busy message means retry after it finishes.

The file holds **unencrypted financial data**, including notes and remote record identifiers.
Moneyflow makes the file private to its owner, never replaces an existing output, and leaves
the parent directory's permissions unchanged. The parent must already exist. On filesystems
that cannot publish without replacement, export fails and removes its temporary file. Do not
attach profile exports to public issues.

Transfer keeps committed edits, retired and merged entities, deletion records, provider
identities, YNAB split facts and write restrictions, Amazon import data and local edits, and
bank CSV source identities and local override flags. It includes CSV's latest file records
and file-to-row links, so a later reimport can reconcile the transferred data.

Transfer does not copy credentials, tokens, undo/redo history, write batches, scheduler state,
or past import-attempt logs. Export reports excluded inactive redo operations without removing
them from the source. Import adds any missing protected system categories and reports those
additions separately.

Amazon's saved category hierarchy is kept, but its link to a taxonomy-source profile is not.
Rebind that link separately if you need it in the new catalog.

Both commands enforce 512 MiB per file, 1 MiB per record, and 1,000,000 combined data
records. The record count includes accounts, categories, provider metadata, Amazon items, and
CSV source and file records and links, not just transactions. Moneyflow cannot export a larger
profile; it does not truncate or split one. Import rejects unknown fields or versions,
incomplete files, duplicate records, invalid references, and inconsistent exact-money values.

## Rebuild your profiles

- Reconnect Monarch and YNAB to download their current provider data.
- [Reimport the original Amazon CSV files](../guide/amazon-mode.md). Python-only merchant
  names, categories, hide flags, and deletions are not copied. Reapply them in Go as needed.
- Connect SimpleFIN in a new Go profile. Its available history may differ from your old local
  history; keep the old data until you have compared them.
- [Import Chase bank CSV files](../guide/bank-csv.md) into a new CSV profile. This does not
  carry over Python-only edits or deletion records.

Check dates, totals, categories, hidden rows, and Amazon matches before you rely only on Go.
Provider-saved edits can return through provider import; unsent or local-only edits cannot.

## Check which program runs

Both versions use the name `moneyflow`. Run `command -v moneyflow` on Unix or
`Get-Command moneyflow` in PowerShell to see which executable your shell selects. The Go
command `moneyflow version` prints its build commit.

Go uses explicit subcommands: `moneyflow tui`, `moneyflow web`, and `moneyflow mcp`. The old
`moneyflow --demo`, `moneyflow amazon`, and `moneyflow simplefin` commands are not aliases
in Go.

## What about older Go previews?

Moneyflow installs one current schema into an empty database. It does not migrate older
databases and refuses incompatible profiles. The Go build offers **Start fresh** to
set up a separate profile and keep the existing files. See
[incompatible profiles](../reference/troubleshooting.md#the-profile-schema-is-incompatible)
for setup and recovery choices.

Export before you switch to an incompatible binary: a newer binary cannot export an older
schema it does not understand. Keep a full profile backup and the source-compatible binary.
Do not delete database, WAL, or SHM files from a running process.

## What remains before cutover?

The Python source exporter must write the existing JSONL format without changing the source
files. It is not built yet. It must keep recoverable saved data and report unsupported sources
instead of silently dropping local edits or deletion records. It must require explicit
currency and decimal precision and convert source values exactly. When exact facts are
unavailable, it must report the affected record, never round historical floating-point amounts
or change the source. This bridge stays separate from the Go application's runtime and normal
build dependencies.

Two source-data recovery gaps are tracked separately:

- [Amazon recovery (#171)](https://github.com/wesm/moneyflow/issues/171): Python merchant
  edits overwrite the original product text, and deleted Amazon rows have no saved deletion
  record. The database alone cannot recover those facts.
- [Chase CSV identity transfer (#172)](https://github.com/wesm/moneyflow/issues/172):
  Python saves different source IDs from Go. Deleted rows keep only their hashed ID and
  deletion time, and merchant edits can remove fields needed to rebuild a Go source ID.
  Reimporting files does not replace transferring that saved state.

These are follow-ups, not promises of future recovery. Keep the complete Python data directory
and any original exports. A full switch still needs the exporter and preservation checks for
the profile types you use; Go-to-Go transfer alone is not enough.
