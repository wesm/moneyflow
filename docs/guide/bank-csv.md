# Import bank CSV files

Import Chase credit-card exports into a separate Moneyflow profile. Browse and edit the
transactions in TUI, web, or MCP. Commits stay local; Moneyflow never writes to the bank.
This feature is available in this Go checkout, not the previously published Python release.

## Import your files

1. List the available mappings:

   ```bash
   moneyflow provider import list
   ```

2. Import a file into a named CSV profile:

   ```bash
   moneyflow provider import institution chase_credit ./Chase.csv --profile "Card imports" --account "Main card"
   ```

3. Open the profile using the command printed by the importer. You can also select it
   in `moneyflow web` or pass its ID to `moneyflow mcp --profile ID`.

`--profile` is required. An unused name creates a profile; an existing name or ID must
select a CSV profile. The importer refuses other profile kinds. Creating the profile holds
the catalog lock until the first file is saved; other profile commands may report busy.

Use the same `--account` label for repeated exports from one card and different labels for
different cards. Omitting it uses `Chase Credit Card`. A blank label is an error. Labels
are normalized for identity, so changing capitalization does not create a separate account.

You can pass a directory instead of a file. Moneyflow walks it recursively and processes
`Chase*.csv` files in sorted path order. A directly selected file can have any name.
Symlinks are not followed. Each file commits separately: if a later file fails, earlier
successful files remain imported. The command reports their counts and exits with an error.

## What does the Chase mapping accept?

`chase_credit` is the only built-in mapping. It reads `Transaction Date` as `MM/DD/YYYY`,
`Description` as merchant, and signed `Amount` as USD with two decimal places. Expenses
are negative. Amounts must be exact; values needing rounding are rejected.

`Category` and `Memo` are optional. `Post Date` and `Type` are retained as metadata when
present. Missing categories become Uncategorized. There is no browser upload or custom
mapping configuration in this version; importing starts from the command line.

## What happens when I import again?

Run the same command to refresh the profile. There is no background bank connection.
An unchanged, previously clean file does not change the profile. `--force` processes it
again but does not override local edits.

Overlapping exports share transactions with the same account, date, signed amount,
currency, scale, and trimmed merchant text. Category, notes, and filename do not define a
transaction. Identical purchases within one file remain distinct: the first occurrence
matches the first occurrence in another file, the second matches the second, and so on.
Without bank transaction IDs, Moneyflow cannot distinguish otherwise identical purchases
that appear separately in different files. Review those cases manually.

When you replace a previously imported file with a corrected export:

- Rows with the same source identity keep their IDs. Source category changes respect
  committed local overrides; notes and metadata remain source-owned.
- Changing the source merchant, date, amount, or account creates a new identity and a new
  transaction. Local merchant edits do not change source identity. Edited originals may
  remain alongside the corrected rows, as described below.
- A missing, unedited transaction is removed only when no other imported file still
  contains it. If it returns later, Moneyflow restores its original ID.
- Missing transactions with local merchant/category edits, hidden state, or active pending
  edits are retained. The summary warns about possible duplicates. Moneyflow does not
  guess which replacement row should inherit those edits.
- Transactions you explicitly deleted and committed stay deleted when reimported.

File identity includes its absolute path, mapping, and normalized account label. Moving or
renaming a file, or importing it from a different path on another computer, creates a new
file identity. Overlapping transactions still match, but the old file's claims remain;
the new file cannot remove transactions on behalf of the old one.

Active pending edits are preserved. A processed import can discard inactive redo operations;
the summary reports the count. Review and commit local changes through the ordinary
[editing workflow](editing.md).

## What if a file contains errors?

Invalid dates, merchants, categories, or amounts skip the affected row and report its file,
record, column, and reason without echoing its values. Valid rows still import. A file with
skipped rows never removes previously imported rows and is retried even when its bytes are
unchanged.

Missing required headers, duplicate headers, malformed CSV, invalid UTF-8, I/O failures,
and exceeded limits fail the whole file without saving a partial snapshot. A valid header
with no data rows initializes an empty CSV profile; it can also remove unedited rows from
an earlier clean version of that same file.

| Limit | Maximum |
| --- | --- |
| Files per run | 256 |
| Data records per run, including skipped rows | 1,000,000 |
| Columns | 128 |
| Bytes per file | 64 MiB |
| Bytes per run | 512 MiB |
| Bytes per logical CSV record | 1 MiB |
| Bytes per field | 16 KiB |

## Move a CSV profile to another installation

Use [JSONL profile transfer](../getting-started/transition.md#transfer-a-go-profile-with-jsonl),
not transaction export. It preserves the source identities, latest file records, file-to-row
links, local override flags, and deletion records needed for repeat imports. It does not
retain a log of every import attempt. JSONL has its own combined-record limit, which counts
these records as well as transactions.

The Python source exporter remains unbuilt. Reimporting Python's original bank files is
not a transfer of Python-only edits or deletions; retain the old data until that bridge is
implemented and checked.
