# Filters

Press `f` to open the filter settings modal.

## Available Filters

### Show Transfers

**Default**: Off (transfers excluded)

Transfer transactions (e.g., "Transfer to Savings") are typically internal movements between your accounts,
not income or expenses. By default, moneyflow excludes them from all views.

Toggle this filter to:

- **On**: Include transfer transactions in all views
- **Off**: Exclude transfer transactions (recommended for spending analysis)

### Show Hidden Transactions

**Default**: On (hidden transactions shown)

Transactions can be marked as "hidden from reports" (press `h` on any transaction). This is useful for:

- One-time purchases you want to exclude from spending trends
- Reimbursed expenses
- Corrections or duplicates

**Behavior**:

- **Detail views**: Always show hidden transactions (so you can review and unhide them)
- **Aggregate views**: Respect this filter setting
  - **On**: Include hidden transactions in counts and totals
  - **Off**: Exclude hidden transactions from aggregate calculations

Hidden transactions are marked with an `H` indicator in the detail view.

## Applying Filters

1. Press `f` to open the filter modal
2. Use arrow keys to navigate options
3. Press `Space` or `Enter` to toggle filters
4. Press `Enter` to apply changes

The status bar shows your current filter settings:

- "transfers excluded" or "transfers shown"
- "hidden shown" or "hidden excluded"

## Advanced Filtering

More advanced filtering capabilities (by amount range, date range, merchant patterns, etc.)
are planned for future releases.

For now, use [Search](navigation.md#search) to filter by text matching,
and [Time Navigation](navigation.md#time-navigation) to filter by date.
