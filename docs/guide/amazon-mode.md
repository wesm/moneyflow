# Amazon purchases

Import Amazon order-history CSV files to review purchases, assign categories locally,
and identify products behind bank charges. Moneyflow does not log into Amazon or send
edits to Amazon.

## Get your order history

Request your order history through Amazon's account privacy tools. Download the prepared
archive and unzip it. Use the original files named `Retail.OrderHistory.*.csv`, including
files in numbered subdirectories. A ZIP file itself is not an import source.

Keep the original exports. They are needed to rebuild a profile and do not contain edits
made only in Moneyflow. Existing Python users should read [moving to Go](../getting-started/transition.md).

## Import purchases

In `moneyflow tui` or `moneyflow web`, choose **Add profile**, then **Amazon**. Name the
profile and confirm its currency and decimal places. Choose the unzipped directory in the
terminal, or select the order-history CSV files in the browser.

The command-line alternative creates the named profile when it does not exist:

```bash
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders" --currency USD --scale 2
moneyflow tui --profile "Amazon Orders"
```

Amounts use the profile's currency and scale: USD with scale 2 stores `12.34` as exactly
`1234` minor units. Those settings cannot change after the first successful import.
Conflicting currencies reject the whole import; use separate profiles.

To copy another profile's committed categories and groups on the first import, add
`--clone-taxonomy-from NAME_OR_ID`. This is a one-time copy, not ongoing synchronization.
New purchases start in Uncategorized.

## Edit and import again

Use `m` for merchant names, `c` for categories, `h` for report visibility, and `w`
to review and commit. All Amazon edits stay local. See [editing](editing.md) for bulk
selection, undo/redo, and deletion.

Press `r` to choose another export, or repeat the import command with the same profile.

- Reimporting the same purchases does not duplicate them.
- Existing identities preserve local edits through status updates and unambiguous corrections.
- Orders present in the new export are reconciled from that export.
- Orders absent from the export are retained, so importing a shorter period does not erase history.
- Cancelled rows can retire items from the same observed order.
- Invalid rows stop the import before it changes the profile.

There is no automatic Amazon refresh. Download and import another export when needed.
Import results report inserted, updated, restored, and retired counts; review unexpected changes.

## Match bank charges to products

Import Amazon data into the same Moneyflow catalog as your finance profiles. Compatible
profiles must use the same currency and scale. Moneyflow reads imported order data locally;
matching does not call Amazon or change a bank transaction.

In a finance profile, search for Amazon or drill into an Amazon-like merchant. When every
visible row qualifies, an **Amazon** column shows matching products. Press `i` for order
and item details. Product-name search also supplements ordinary transaction search for
Amazon-like merchants.

Matching uses a seven-day date window and these passes, in order:

1. Match the charge to the whole order within 0.02 currency units. Scales below 2 require equality.
2. Look for a smaller charge consistent with gift-card use. The difference must be at most
   the larger of 15 currency units or 10% of the order total.
3. Match individual items when the earlier passes found no order match.

The first successful pass wins across compatible profiles. Matches are suggestions, not
proof that a bank charge belongs to an order. Inspect the dates, amounts, and products.
No currency conversion is performed.

## What files are accepted?

Required column names are `Order ID`, `Order Date`, `Product Name`, `Quantity`,
`Total Owed`, `Order Status`, and `Shipment Status`. Optional columns include `ASIN`,
`Unit Price`, and `Currency`. A blank quantity means 1. Missing ASIN values use a
derived item identity. Moneyflow parses amounts exactly and rejects values it cannot represent.

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
For a row error, inspect the filename, record number, and column shown to the importing user.
Do not post original order files or account details in an issue; create a synthetic example.

## If results look wrong

If no files are found, select the unzipped directory or original order-history CSV files.
If currency validation fails, confirm both the export currency and the profile settings.
If a bank charge has no match, check that the Amazon profile is in the same catalog and
that the amounts, currency, scale, merchant, and dates qualify.

Automated coverage includes parsing, cancellation, reconciliation, local-edit preservation,
matching, product search, CLI/TUI/API flows, and browser reimport. It does not prove that every
regional or future Amazon export format is accepted.
