# Navigation & Search

moneyflow provides multiple views of your transaction data and powerful drill-down capabilities to analyze spending
from different angles.

## View Types

### Aggregate Views

Press `g` to cycle through aggregate views. Aggregate views group your transactions by a specific field (merchant,
category, group, or account) and display summary statistics for each group, including transaction count and total
amount spent.

**Cycle Order**: Merchant → Category → Group → Account → Merchant...

<table>
<tr>
<td width="50%">
<strong>Merchant View</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/cycle-1-merchants.svg" width="100%"
alt="Merchants view">
</td>
<td width="50%">
<strong>Category View</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/cycle-2-categories.svg" width="100%"
alt="Categories view">
</td>
</tr>
<tr>
<td width="50%">
<strong>Group View</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/cycle-3-groups.svg" width="100%" alt="Groups view">
</td>
<td width="50%">
<strong>Account View</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/cycle-4-accounts.svg" width="100%" alt="Accounts view">
</td>
</tr>
</table>

| View | What It Shows | Use For |
|------|---------------|---------|
| **Merchant** | Spending by store/service + top category | See patterns by merchant (e.g., total spent at Amazon) |
| **Category** | Spending by category | Identify which categories consume your budget |
| **Group** | Spending by category group | Monthly budget reviews, broad spending patterns |
| **Account** | Spending by payment method | Reconciliation, per-account spending analysis |

**Columns displayed:**

- **Name, Count, Total** (all aggregate views)
- **Top Category** (Merchant view only) - Shows the most common category for each merchant with percentage
  (e.g., "Groceries 90%"). This helps identify categorization patterns and spot miscategorized transactions.

!!! tip "Top Category Column"
    The Top Category column in Merchant view shows at a glance whether a merchant is properly categorized:

    - **100%** = All transactions use the same category (consistent)
    - **85%** = Mostly one category (likely correct)
    - **60%** = Mixed categorization (may need cleanup)

    Example: "Whole Foods → Groceries 95%" confirms most purchases are correctly categorized.

**Amazon Mode:** View names reflect purchase data instead of financial transactions.

| Default Backend | Amazon Mode | Shows |
|-----------------|-------------|-------|
| Merchant | Item | Product names |
| Category | Category | Product categories |
| Group | Group | Category groups |
| Account | Order ID | Amazon orders |

### Detail View

Press `d` to view all transactions ungrouped in chronological order,
or press `Enter` from any aggregate row to see the transactions for that specific item.

To return to an aggregate view, press `g` or `Escape`.

**Columns displayed:**

- Date
- Merchant
- Category
- Account
- Amount

**Visual indicators:**

| Indicator | Meaning |
|-----------|---------|
| ✓ | Transaction selected for bulk operations |
| H | Transaction hidden from reports |
| * | Transaction has pending edits |

**Capabilities:**

- Edit merchant names, categories, and hide status
- Multi-select for bulk operations
- View full transaction details

![Detail view with indicators](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/detail-view-flags.svg)

## Drill-Down

From any aggregate view, press `Enter` on a row to drill into it and see the individual transactions that make up that aggregate.

![Merchant view with Amazon highlighted](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/merchants-view.svg)

**Example workflow:**

1. **Start in Merchant view** - Press `g` if needed to cycle to Merchants
2. **Navigate to "Amazon"** - Use arrow keys to move cursor
3. **Press `Enter`** - Drill down to see transactions
4. **View results** - All Amazon transactions displayed

![Drilled down into Amazon - transaction detail view](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-detail.svg)

The breadcrumb shows your current path: `Merchants > Amazon`

**Going back:**
Press `Escape` to return to Merchant view with your cursor position and scroll restored.

## Sub-Grouping

Once you've drilled down into a specific item, press `g` to sub-group the filtered data by a different field.
This allows you to analyze the same transactions from multiple perspectives without losing your filter context.

**Example - Analyzing Amazon purchases:**

1. **Drill into Amazon** - From Merchant view, press `Enter` on Amazon row
2. **Press `g`** - View changes to `Merchants > Amazon (by Category)`
   - Shows Amazon spending grouped by category
3. **Press `g` again** - View changes to `Merchants > Amazon (by Group)`
   - Shows Amazon spending grouped by category group
4. **Press `g` again** - View changes to `Merchants > Amazon (by Account)`
   - Shows which payment methods you use at Amazon
5. **Press `g` again** - Returns to detail view
   - Shows all Amazon transactions ungrouped

![Drilled into Merchant, grouped by Category](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/merchants-drill-by-category.svg)

![Drilled into Amazon, grouped by Account](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-group-by-account.svg)

Sub-grouping helps answer analytical questions like:

- "How much did I spend on groceries from Amazon?"
- "Which credit card do I use most at Starbucks?"
- "What categories make up my Target spending?"

When you're in a drilled-down view, pressing `g` cycles through the available sub-groupings. The field you're already
filtered by is automatically excluded from the cycle to avoid redundancy.

## Multi-Level Drill-Down

You can drill down from sub-grouped views to add another level of filtering, creating a multi-level filter hierarchy.

**Example - Finding Amazon grocery transactions:**

1. **Drill into Amazon** - From Merchant view, press `Enter` on "Amazon"
2. **Sub-group by Category** - Press `g` repeatedly until breadcrumb shows "(by Category)"
3. **Drill into Groceries** - Press `Enter` on the "Groceries" row
4. **View results** - Breadcrumb shows: `Merchants > Amazon > Groceries`
   - Now viewing only Amazon grocery transactions

![Multi-level drill-down breadcrumb](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-multi-level.svg)

This powerful feature lets you combine multiple filters to answer very specific questions about your spending.

## Going Back

Press `Escape` to navigate backwards through your drill-down path, removing one filter level at a time.

**From top-level detail view:**

- When viewing all transactions (not drilled down), press `g` or `Escape` to return to an aggregate view
- Both keys restore your previous aggregate view (Merchant, Category, Group, or Account)

**Single-level drill-down with sub-grouping:**

- From `Merchants > Amazon (by Category)`, press `Escape` to return to `Merchants > Amazon` (clears sub-grouping)
- From `Merchants > Amazon`, press `Escape` to return to `Merchants` (clears merchant filter)

**Multi-level drill-down:**

- From `Merchants > Amazon > Groceries`, press `Escape` to return to `Merchants > Amazon` (removes category filter)
- From `Merchants > Amazon`, press `Escape` to return to `Merchants` (removes merchant filter)

**With search active:**

- If search was your most recent action, the first `Escape` press clears the search and returns to your previous view
- Subsequent `Escape` presses navigate backwards through your drill-down levels

Your cursor position and scroll state are preserved when going back, making it easy to explore different views and
return to exactly where you were.

## Sorting

Control how rows are sorted in the current view.

**Cycle sort fields:**

- Press `s` to cycle through the available sort fields for the current view
- Available fields depend on whether you're in an aggregate or detail view

**Reverse sort direction:**

- Press `v` to reverse the sort direction between ascending and descending

**Available sort fields by view type:**

- **Aggregate Views**: Field name (e.g., Merchant, Category), Count (number of transactions), Amount (total spent)
- **Detail Views**: Date, Merchant name, Category, Account, Amount

## Time Navigation

Filter your transactions to specific time periods for focused analysis.

**Quick time filters:**

- `t` - Filter to this month only
- `y` - Filter to this year only
- `a` - Show all time (remove time filter)

**Navigate between periods:**

- `←` (Left arrow) - Move to the previous time period
- `→` (Right arrow) - Move to the next time period

The arrow keys intelligently navigate based on your current time filter. When viewing "This Month", arrows move to the
previous or next month. When viewing "This Year", arrows move to the previous or next year. The breadcrumb displays
your current time period.

**Command-line time filters:**

You can also specify time filters when launching moneyflow for faster startup with large transaction histories:

```bash
moneyflow --year 2025      # Load only 2025 transactions
moneyflow --days 90        # Load last 90 days
moneyflow --month 2025-03  # Load March 2025 only
```

## Search

Press `/` to search and filter transactions by text matching across merchant names, categories, and transaction notes.

![Search modal](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/search-modal.svg)

**Using search:**

1. **Press `/`** - Opens the search modal
2. **Type your query** - Filters as you type (case-insensitive, partial matching)
3. **Press `Enter`** - Applies the search filter
4. **Press `Escape`** - Clears search and returns to previous view

![Search results for "coffee"](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/merchants-search.svg)

Search filters persist as you navigate between different views. The breadcrumb displays "Search: your query" to remind
you that search is active. To clear a search, press `/` again and submit an empty search, or press `Escape` if search
was your most recent action.

## Multi-Select

Select multiple transactions or aggregate groups to perform bulk operations.

**Selecting rows:**

- Press `Space` to toggle selection on the current row
- Press `Ctrl+A` to select all visible rows in the current view
- Selected rows display a checkmark indicator

**Bulk operations available:**

- Rename merchants across multiple transactions
- Change categories for multiple transactions
- Hide or unhide multiple transactions from reports

## Common Use Cases

Here are some practical examples of using moneyflow's navigation features to answer real questions about your spending:

### "What do I buy at Costco?"

1. **Navigate to Merchant view** - Press `g` until you see Merchants
2. **Drill into Costco** - Move cursor to "Costco", press `Enter`
3. **Sub-group by Category** - Press `g` until breadcrumb shows "(by Category)"
4. **View breakdown** - See Groceries $450, Gas $120, etc.

### "Where am I buying groceries?"

1. **Navigate to Category view** - Press `g` until you see Categories
2. **Drill into Groceries** - Move cursor to "Groceries", press `Enter`
3. **Sub-group by Merchant** - Press `g` until breadcrumb shows "(by Merchant)"
4. **View breakdown** - See Whole Foods $890, Safeway $650, Amazon $234

### "How do I use my Chase Sapphire card?"

1. **Navigate to Account view** - Press `g` until you see Accounts
2. **Drill into Chase Sapphire** - Move cursor to "Chase Sapphire", press `Enter`
3. **Sub-group by Category** - Press `g` until breadcrumb shows "(by Category)"
4. **View breakdown** - See spending by category for this card

**Quick Analysis Tip:**

- When drilled down, `g` becomes your pivot tool for viewing the same filtered data from different perspectives
- No need to go back to the top-level view and re-filter
- Combine drill-down with time navigation for powerful analysis: press `t` to filter to this month, drill down to
  analyze current spending, then press `←` to compare with previous months

## Quick Reference

| Key | Action |
|-----|--------|
| `g` | Cycle aggregate views, or return to aggregate view from detail view |
| `d` | Detail view (all transactions) |
| `Enter` | Drill down |
| `Escape` | Go back (or return to aggregate view from detail view) |
| `s` | Cycle sort field |
| `v` | Reverse sort |
| `/` | Search |
| `f` | Filters |
| `Space` | Select row |
| `Ctrl+A` | Select all |
| `m` / `c` / `h` | Edit selected transaction(s) |
| `x` | Delete selected transaction(s) |
| `u` | Undo pending edit |
| `w` | Commit pending edits |
| `t` / `y` / `a` | Time filters |
| `←` / `→` | Previous/next period |

For the complete list of keyboard shortcuts, see [Keyboard Shortcuts](keyboard-shortcuts.md).
