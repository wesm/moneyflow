# Amazon purchases

Import Amazon order-history CSV files to review purchases, assign categories, and see which
products sit behind the Amazon charges in your bank or card profiles. Moneyflow does not log in
to Amazon. Every edit stays in Moneyflow; nothing is sent to Amazon.

## What stays local?

All of it. Merchant names, categories, report visibility, deletions, and category or group
changes commit to the local profile only. Reimporting an export keeps those edits; see
[import again](#edit-and-import-again).

## Get your order history

1. Request your order history through Amazon's account privacy tools.
2. Download the prepared archive and unzip it.
3. Use the original files named `Retail.OrderHistory.*.csv`, including the ones in numbered
   subdirectories. A ZIP file is not an import source.

Keep the original exports. You need them to rebuild a profile, and they do not contain edits
made in Moneyflow. Existing Python users should read [moving to Go](../getting-started/transition.md).

## Import purchases

The browser and the terminal use the same steps:

1. Run `moneyflow web` or `moneyflow tui`.
2. Choose **Add profile** (`a`), then **Amazon orders** in the browser or **Amazon order
   history** in the terminal (`a`).
3. Enter a profile name.
4. Confirm the currency and decimal places. `USD` and `2` are preselected.
5. Choose the source:
   - In the browser, select the order-history CSV files, or the unzipped export directory.
   - In the terminal, enter the path of the unzipped export directory.

Both interfaces have an optional advanced field, under **Advanced options** in the browser,
that copies categories and groups from another profile, as `--clone-taxonomy-from` does below.

From the command line, `provider import amazon` takes the unzipped directory. It creates the
named profile when it does not exist:

```bash
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders" --currency USD --scale 2
moneyflow tui --profile "Amazon Orders"
```

Without `--profile`, an import into an empty data folder creates a profile named **Amazon**.
Rename it with `moneyflow profile rename --profile Amazon --name "Amazon Orders"`.

Amounts use the profile's currency and scale: USD with scale 2 stores `12.34` as exactly
`1234` minor units. Those settings cannot change after the first successful import.
Conflicting currencies reject the whole import; use separate profiles.

To copy another profile's committed categories and groups on the first import, add
`--clone-taxonomy-from NAME_OR_ID`. This is a one-time copy, not ongoing synchronization.
New purchases start in Uncategorized.

## Edit and import again

The browser and the terminal share the editing keys: `m` for merchant names, `c` for
categories, `h` for report visibility, and `w` to review and commit. See [editing](editing.md)
for bulk selection, undo and redo, and deletion.

To import a newer export, press `r`, or select **Import Amazon orders** in the browser's
sidebar. You can also repeat the import command with the same profile.

- Reimporting the same purchases does not duplicate them.
- Existing identities keep their local edits through status updates and unambiguous corrections.
- Orders in the new export are reconciled from that export.
- Orders absent from the export are kept, so importing a shorter period does not erase history.
- Cancelled rows can retire items from the same order.
- Invalid rows stop the import before it changes the profile.

Moneyflow never refreshes Amazon data on its own. Import results report inserted, updated,
restored, and retired counts; review unexpected changes.

## Match bank charges to products

Import Amazon data into the same Moneyflow data folder as your finance profiles. Compatible
profiles use the same currency and scale. Matching reads the imported order data locally; it
does not call Amazon or change a bank transaction.

In a finance profile, search for Amazon or drill into an Amazon-like merchant. When every
visible row qualifies:

- The terminal shows an **Amazon** column with the matching products.
- The browser shows a match mark in the row's flags: `✓` for a high- or medium-confidence
  match and `~` for a likely one. Hover the mark to see the first product and the confidence;
  click it to open the transaction information.

Press `i` in either interface for order and item details. Product-name search also
supplements ordinary transaction search for Amazon-like merchants.

Matching uses a seven-day date window and these passes, in order:

1. Match the charge to the whole order within 0.02 currency units. Scales below 2 require equality.
2. Look for a smaller charge consistent with gift-card use. The difference must be at most
   the larger of 15 currency units or 10% of the order total.
3. Match individual items when the earlier passes found no order match.

The first successful pass wins across compatible profiles. A match is a suggestion, not proof
that a charge belongs to an order. Inspect the dates, amounts, and products. Moneyflow does not
convert currencies.

## What files are accepted?

Required columns are `Order ID`, `Order Date`, `Product Name`, `Quantity`, `Total Owed`,
`Order Status`, and `Shipment Status`. Optional columns include `ASIN`, `Unit Price`, and
`Currency`. A blank quantity means 1. A missing ASIN gets a derived item identity. Amounts are
parsed exactly; values Moneyflow cannot represent are rejected.

| Limit | Maximum |
| --- | --- |
| Files | 256 |
| Logical records | 1,000,000 |
| Columns | 128 |
| One file | 64 MiB |
| Combined files | 512 MiB |
| One record | 1 MiB |
| One field | 16 KiB |

Duplicate file contents and conflicting overlapping order records are rejected.

## If results look wrong

- **No files found:** select the unzipped directory or the original order-history CSV files.
- **Currency validation fails:** check the export currency and the profile settings.
- **A row is rejected:** the error shows the filename, record number, and column.
- **A bank charge has no match:** check that the Amazon profile is in the same data folder, and
  that the amounts, currency, scale, merchant, and dates qualify.
- **Moneyflow refuses the data folder:** it holds Python app files. See
  [moving Python data](../getting-started/transition.md#move-python-data-out-of-moneyflow).

Do not post original order files or account details in an issue. Create a synthetic example.

Automated tests cover parsing, cancellation, reconciliation, local-edit preservation, matching,
product search, and the CLI, terminal, API, and browser import flows. They do not prove that
every regional or future Amazon export format is accepted.
