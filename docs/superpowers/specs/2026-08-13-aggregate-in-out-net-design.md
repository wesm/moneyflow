# Aggregate In, Out, and Net Columns

## Goal

Show separate In, Out, and Net amounts in every aggregate table so users can see cash-flow direction for each merchant, category, group, account, or time bucket without drilling into its transactions.

## Scope

This change applies to top-level aggregate views and aggregate sub-groupings for Merchant, Category, Group, Account, and Time. The top-right In/Out/Net summary remains unchanged. Transaction detail tables are not affected.

## Data Model

Each aggregate row will calculate three amounts from transactions that are not hidden from reports:

- `amount_in`: sum of positive amounts
- `amount_out`: sum of negative amounts
- `total`: net amount, calculated as In plus Out

The explicit `amount_in` and `amount_out` names avoid Python keyword conflicts and make their meaning clear to non-table consumers. The existing `total` field remains the internal Net value. Keeping that field preserves current sorting and percentage behavior without introducing a migration across aggregate consumers.

Transaction Count continues to include hidden transactions, matching current behavior. Hidden transactions do not contribute to In, Out, or Net.

Time aggregation will fill missing periods with zero for all three amounts.

## Table Presentation

Aggregate tables will use this order:

1. Grouping field, such as Merchant or Period
2. Count
3. In
4. Out
5. Net
6. `%`
7. Existing view-specific columns, such as Top Category or backend-computed columns
8. Flags

The amount headings include the configured currency symbol and are right-aligned. In uses the existing positive amount format, Out uses the existing negative amount format, and Net uses the existing signed amount format. This matches the top-right summary. The shared formatter renders zero as `+0.00` in every amount column, including Out; it does not synthesize a negative zero.

Amount sorting continues to sort by Net. The amount sort arrow appears only on the Net heading. This feature does not add separate In or Out sort modes.

The `%` column keeps its current behavior. It is calculated from Net, with positive-Net rows compared against total positive Net and negative-Net rows compared against the absolute total negative Net.

## Compatibility

Merchant top-category calculation and backend-computed columns remain unchanged. Aggregate drill-down, multi-selection, pending-edit flags, and row identity continue to use the grouping field rather than amount columns.

Empty aggregate data produces headings with no rows. A group with no visible cash flow displays zero amounts. Existing aggregate consumers can continue to use `total` as Net.

## Testing

Behavior tests will verify:

- field aggregation calculates In, Out, and Net for mixed cash flow;
- hidden transactions remain in Count but are excluded from all amount columns;
- merchant, category, group, account, and time aggregations expose the new values;
- missing time periods fill In, Out, and Net with zero;
- formatter headings and rows render In, Out, Net, and the existing `%` column in order;
- merchant and backend-computed columns retain their positions after the new amount columns;
- amount sorting still uses Net and marks the Net heading;
- empty aggregate views retain the complete column structure.

Manual verification will inspect the Merchant and Time views at a narrow terminal width to confirm that Textual's existing horizontal table behavior keeps every column reachable and readable.

Documentation will describe aggregate tables as showing Count, In, Out, Net, and `%`.
