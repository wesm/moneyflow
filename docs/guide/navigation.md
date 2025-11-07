# Navigation & Search

moneyflow provides multiple views of your transaction data and powerful drill-down capabilities to analyze spending
from different angles.

## View Types

### Aggregate Views

Press `g` to cycle through aggregate views. Aggregate views group your transactions by a specific field (merchant,
category, group, account, or time) and display summary statistics for each group, including transaction count and total
amount spent.

**Cycle Order**: Merchant → Category → Group → Account → Time → Merchant...

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
<tr>
<td width="50%">
<strong>TIME View (by Years)</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/cycle-5-time-years.svg" width="100%"
alt="Time view by years">
</td>
<td width="50%">
<strong>TIME View (by Months)</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/time-view-months.svg" width="100%"
alt="Time view by months">
</td>
</tr>
<tr>
<td width="50%">
<strong>TIME View (by Days)</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/time-view-days.svg" width="100%"
alt="Time view by days">
</td>
<td width="50%">
</td>
</tr>
</table>

| View | What It Shows | Use For |
|------|---------------|---------|
| **Merchant** | Spending by store/service + top category | See patterns by merchant (e.g., total spent at Amazon) |
| **Category** | Spending by category | Identify which categories consume your budget |
| **Group** | Spending by category group | Monthly budget reviews, broad spending patterns |
| **Account** | Spending by payment method | Reconciliation, per-account spending analysis |
| **Time** | Spending by time period (years, months, or days) | Analyze spending trends over time, year-over-year comparisons |

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
5. **Press `g` again** - View changes to `Merchants > Amazon (by Year)`
   - Shows Amazon spending trends over time (press `t` to cycle granularity)
6. **Press `g` again** - Returns to detail view
   - Shows all Amazon transactions ungrouped

![Drilled into Merchant, grouped by Category](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/merchants-drill-by-category.svg)

![Drilled into Amazon, grouped by Account](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-group-by-account.svg)

Sub-grouping helps answer analytical questions like:

- "How much did I spend on groceries from Amazon?"
- "Which credit card do I use most at Starbucks?"
- "What categories make up my Target spending?"

When you're in a drilled-down view, pressing `g` cycles through the available sub-groupings:

**Sub-grouping cycle:** Category → Group → Account → Time → Detail → Category...

The field you're already filtered by is automatically excluded from the cycle to avoid redundancy.

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

## Time as an Aggregate Dimension

Time is a first-class aggregate dimension, just like Merchant, Category, Group, or Account. You can view spending
grouped by time periods, drill into specific years or months, and combine time with other dimensions for powerful
temporal analysis.

### TIME View

Press `g` to cycle to the TIME view, which shows your transactions aggregated by time period. Toggle through three
granularity levels to adjust the time grouping:

- **Press `t`** - Cycle through granularities (Year, then Month, then Day, then back to Year)

**Example workflow:**

1. **Press `g` until you reach TIME view** - Shows all years in your dataset
2. **Press `t`** - Toggle to monthly view - Shows all months with data
3. **Press `t` again** - Toggle to daily view - Shows all days with data
4. **Press `Enter` on a specific period** - Drill into that time period
5. **Press `g` to sub-group** - Pivot by Merchant/Category/etc within that period

### Drilling Into Time Periods

From TIME view, press `Enter` on any year, month, or day to drill down and see only transactions from that period.

<table>
<tr>
<td width="50%">
<strong>Drilled Into Year</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/time-drill-down-year.svg" width="100%"
alt="Drilled into specific year">
</td>
<td width="50%">
<strong>Drilled Into Month</strong><br>
<img src="https://raw.githubusercontent.com/wesm/moneyflow-assets/main/time-drill-down-month.svg" width="100%"
alt="Drilled into specific month">
</td>
</tr>
</table>

**Example:**

1. **In TIME view** (showing years 2023, 2024, 2025)
2. **Press `Enter` on 2024** → Breadcrumb shows `Time > 2024`
3. **Press `g`** → View by Merchants within 2024
4. **Press `g` again** → Cycle through Categories, Groups, Accounts, Time (by month)
5. **Press `Escape`** → Back to `Time > 2024` (detail view)
6. **Press `Escape`** → Back to TIME view (all years)

### Time + Other Dimensions

Combine time with other dimensions for multi-faceted analysis:

**Time-first analysis** (`Time > 2024 > Merchants`):

1. Drill into a year/month
2. Sub-group by Merchant/Category/Account
3. Analyze spending within that time period

**Dimension-first with time sub-grouping** (`Merchants > Amazon > by Year`):

1. Drill into a merchant/category
2. Press `g` to sub-group by Year or Month
3. See spending trends over time for that dimension

### Navigate Between Time Periods

When drilled into a specific time period, use arrow keys to navigate forward/backward:

- **`←` (Left arrow)** - Previous period (e.g., from 2024 to 2023, or from Mar 2024 to Feb 2024)
- **`→` (Right arrow)** - Next period (e.g., from 2024 to 2025, or from Mar 2024 to Apr 2024)
- **`a`** - Clear time period selection (shortcut for Escape)

Arrow keys only work when drilled into a time period. The navigation respects your current granularity (years vs months).

### Command-Line Data Loading

For Monarch Money and YNAB backends, you can fetch only recent data for faster startup:

```bash
moneyflow --year 2025           # Fetch from 2025-01-01 onwards
moneyflow --since 2024-06-01    # Fetch from specific date onwards
moneyflow --mtd                 # Fetch month-to-date only
```

!!! note "API Fetching vs View Filtering"
    These flags control what data is **fetched from the API**, not what you see in the view.
    Once data is loaded, all of it is visible by default. Use TIME view to analyze specific periods.

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

### "How has my spending changed over time?"

1. **Navigate to TIME view** - Press `g` until you see Years
2. **Review annual totals** - See 2023: $45,000, 2024: $52,000, 2025: $48,000
3. **Toggle to months** - Press `t` to see monthly breakdown
4. **Drill into a month** - Press `Enter` on "Mar 2024" to see that month's transactions
5. **Navigate months** - Use `←` `→` to move between months

### "How has my Amazon spending trended?"

1. **Navigate to Merchant view** - Press `g` until you see Merchants
2. **Drill into Amazon** - Move cursor to "Amazon", press `Enter`
3. **Sub-group by Year** - Press `g` until breadcrumb shows "(by Year)"
4. **View year-over-year** - See 2023: $2,500, 2024: $3,200, 2025: $2,800
5. **Toggle to months** - Press `t` to see monthly Amazon spending
6. **Drill deeper** - Press `Enter` on a specific year to see all Amazon transactions from that year

**Quick Analysis Tip:**

- When drilled down, `g` becomes your pivot tool for viewing the same filtered data from different perspectives
- No need to go back to the top-level view and re-filter
- Time works symmetrically: drill into Time then pivot by dimension, OR drill into dimension then sub-group by time

## Quick Reference

| Key | Action |
|-----|--------|
| `g` | Cycle aggregate views (includes TIME), or return to aggregate view from detail view |
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
| `t` | Cycle time granularity (Year → Month → Day) in TIME view |
| `a` | Clear time period drill-down |
| `←` / `→` | Navigate time periods (when drilled into time) |

For the complete list of keyboard shortcuts, see [Keyboard Shortcuts](keyboard-shortcuts.md).
