# Import bank CSV files

Import Chase credit-card CSV exports into a separate Moneyflow profile, then review and edit
the transactions in the browser, the terminal, or an MCP client. Every edit commits locally;
Moneyflow never writes to the bank. Reimporting a corrected or overlapping export keeps your
committed edits and deletions.

## What stays local?

All of it. Commits write only to the local profile. Moneyflow has no bank connection, stores no
bank credentials, and refreshes only when you import again.

## Import your files

Importing starts from the command line. The browser and terminal **Add profile** lists have no
bank CSV option, and there is no browser upload.

1. List the available mappings:

   ```bash
   moneyflow provider import list
   ```

2. Import a file into a named CSV profile:

   ```bash
   moneyflow provider import institution chase_credit ./Chase.csv --profile "Card imports" --account "Main card"
   ```

3. Open the profile with the command the importer prints, such as
   `moneyflow tui --profile profile_…`. You can also run `moneyflow web` and choose it in the
   profile list, or pass its name or ID to `moneyflow mcp --profile`.

`--profile` is required. An unused name creates a CSV profile; an existing name or ID must
select a CSV profile, and the importer refuses other kinds. While it creates the profile, the
importer holds the catalog lock until the first file is saved, so other profile commands can
report busy. Rename the profile later with `moneyflow profile rename --profile "Card imports"
--name "Example Card"`.

Use the same `--account` label for repeated exports from one card, and different labels for
different cards. Without it, the label is `Chase Credit Card`. A blank label is an error. Labels
are normalized for identity, so changing capitalization does not create a separate account.

You can pass a directory instead of a file. Moneyflow walks it recursively and processes
`Chase*.csv` files in sorted path order. A file you name directly can have any name. Symlinks
are not followed. Each file commits separately: if a later file fails, earlier files stay
imported, and the command reports their counts and exits with an error.

## Edit the transactions

The browser and the terminal share the editing keys: `m` for merchant names, `c` for categories,
`h` for report visibility, `x` to delete, and `w` to review and commit. See the
[editing guide](editing.md) for selection, undo, and redo.

## What does the Chase mapping accept?

`chase_credit` is the only built-in mapping. It reads:

| Column | Use |
| --- | --- |
| `Transaction Date` | Date, as `MM/DD/YYYY` |
| `Description` | Merchant |
| `Amount` | Signed USD amount with two decimal places; expenses are negative |
| `Category` | Optional; missing categories become Uncategorized |
| `Memo` | Optional notes |
| `Post Date`, `Type` | Kept as metadata when present |

Amounts must be exact; values that need rounding are rejected. Custom mappings are not
configurable.

## What happens when I import again?

Run the same command to refresh the profile. An unchanged file that imported cleanly before
does not change the profile. `--force` processes it again but does not override local edits.

Overlapping exports share a transaction when the account, date, signed amount, currency, scale,
and trimmed merchant text match. Category, notes, and filename do not define a transaction.
Identical purchases within one file stay distinct: the first occurrence matches the first
occurrence in another file, the second matches the second, and so on. Without bank transaction
IDs, Moneyflow cannot tell apart identical purchases that appear separately in different files.
Review those cases yourself.

When you replace an imported file with a corrected export:

- Rows with the same source identity keep their IDs. Source category changes respect committed
  local overrides; notes and metadata remain source-owned.
- Changing the source merchant, date, amount, or account creates a new identity and a new
  transaction. Local merchant edits do not change source identity. Edited originals can remain
  beside the corrected rows, as described below.
- A missing, unedited transaction is removed only when no other imported file still contains
  it. If it returns later, Moneyflow restores its original ID.
- Missing transactions with local merchant or category edits, hidden state, or active pending
  edits are kept. The summary warns about possible duplicates. Moneyflow does not guess which
  replacement row should inherit those edits.
- Transactions you deleted and committed stay deleted when reimported.

File identity includes the absolute path, the mapping, and the normalized account label. Moving
or renaming a file, or importing it from another path on another computer, creates a new file
identity. Overlapping transactions still match, but the old file's claims remain; the new file
cannot remove transactions on behalf of the old one.

Active pending edits are kept. A processed import can discard inactive redo operations; the
summary reports the count.

## What if a file contains errors?

An invalid date, merchant, category, or amount skips that row. The report names its file,
record, column, and reason without echoing its values. Valid rows still import. A file with
skipped rows never removes earlier rows, and is retried even when its bytes are unchanged.

Missing required headers, duplicate headers, malformed CSV, invalid UTF-8, I/O failures, and
exceeded limits fail the whole file without saving a partial snapshot. A valid header with no
data rows initializes an empty CSV profile. It can also remove unedited rows from an earlier
clean version of the same file.

| Limit | Maximum |
| --- | --- |
| Files per run | 256 |
| Data records per run, including skipped rows | 1,000,000 |
| Columns | 128 |
| Bytes per file | 64 MiB |
| Bytes per run | 512 MiB |
| Bytes per logical CSV record | 1 MiB |
| Bytes per field | 16 KiB |

If Moneyflow refuses the data folder because it holds Python app files, see
[moving Python data](../getting-started/transition.md#move-python-data-out-of-moneyflow).

## Move a CSV profile to another installation

Use [JSONL profile transfer](../getting-started/transition.md#transfer-a-go-profile-with-jsonl),
not transaction export. It keeps the source identities, latest file records, file-to-row links,
local override flags, and deletion records that repeat imports need. It does not keep a log of
every import attempt. JSONL has its own combined-record limit, which counts these records as
well as transactions.

The Python source exporter is not built yet. Reimporting Python's original bank files does not
transfer Python-only edits or deletions; keep the old data until that bridge exists and has
been checked.
