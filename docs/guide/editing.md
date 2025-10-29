# Editing Transactions

Learn how to efficiently edit your transactions in moneyflow using keyboard-driven bulk operations.

## Single Transaction Edits

In detail view, edit individual transactions:

| Key | Action |
|-----|--------|
| ++m++ | Edit merchant name |
| ++c++ | Edit category |
| ++h++ | Hide/unhide from reports |
| ++x++ | Delete transaction (with confirmation) |

The cursor stays in place after editing, so you can quickly edit multiple transactions by pressing the same key repeatedly.

All edits are queued (not committed immediately) so you can review them before saving.

**Note:** Delete is different - it executes immediately after confirmation (not queued). This is because deletion is
irreversible and should be done carefully.

### Edit Merchant Modal

When you press ++m++ to edit a merchant, the edit modal shows:

1. **Input field** with the current merchant name (pre-filled)
2. **Autocomplete suggestions** showing existing merchants that match your typing
3. **Your exact input as "create new" option** - displayed in quotes: `"Your Input"`

**Selecting existing merchants:**

- Type to filter the list of existing merchants
- If exactly one match: press `Enter` to auto-select it
- If multiple matches: use arrow keys to select, then `Enter`

**Creating new merchants:**

- Your typed input always appears in the list as `"Your Input"` (in quotes)
- Press down arrow to navigate to this option
- Press `Enter` to create a new merchant with exactly that name

**Example workflow - Consolidating "Amazon" merchants:**

Scenario: You have "Amazon.com", "AMZN Mktp US", and want to rename both to "Amazon"

1. Navigate to first Amazon transaction, press ++m++
2. Type "Amazon" → see:
   - "Amazon.com" (existing match)
   - `"Amazon"` (create new)
3. Press down arrow twice → highlights `"Amazon"`
4. Press `Enter` → creates new merchant "Amazon"
5. Navigate to next transaction, press ++m++
6. Type "Amazon" → now see:
   - "Amazon" (existing - the one you just created)
   - "Amazon.com" (existing)
   - `"Amazon"` (create new - but you want the existing one now)
7. Press `Enter` → auto-selects existing "Amazon" merchant

This workflow gives you precise control over merchant names while making it easy to select existing ones.

<!-- TODO: Add screenshot of edit merchant modal showing autocomplete list with "create new" option -->

## Multi-Select in Detail View

Select multiple transactions to edit them all at once:

![Multi-select transactions with checkmarks](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-detail-multi-select.svg)

1. Press ++space++ on each transaction you want to edit
   - A `✓` checkmark appears
2. Press ++m++ to rename merchant for all selected
3. Or press ++c++ to recategorize all selected
4. Or press ++h++ to hide/unhide all selected
5. Or press ++x++ to delete all selected (with confirmation)

### Example: Recategorize 3 transactions

1. Navigate to transactions
2. ++space++ on transaction 1 → ✓
3. ++space++ on transaction 2 → ✓
4. ++space++ on transaction 3 → ✓
5. Press ++c++ → Select category modal
6. Choose category → All 3 transactions queued for update

## Bulk Edit from Aggregate Views

### Single Group Edit

From any aggregate view (Merchants, Categories, Groups, Accounts), press ++m++, ++c++, or ++h++ to edit ALL
transactions in that group:

![Bulk edit merchant modal](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-bulk-edit-merchant.svg)

### Example: Rename a merchant (all transactions)

1. Press ++g++ until "Merchants" view
2. Navigate to "AMZN*ABC123"
3. Press ++m++ → Edit merchant modal
4. Type "Amazon" and press ++enter++
5. ALL transactions for that merchant are renamed

![Edit category selection](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/drill-down-edit-category.svg)

### Multi-Select Groups (NEW!)

You can now select **multiple groups** and bulk edit all their transactions at once:

### Example: Recategorize multiple merchants

1. Press ++g++ until "Merchants" view
2. Press ++space++ on "Amazon" → ✓ appears
3. Press ++space++ on "Walmart" → ✓ appears
4. Press ++space++ on "Target" → ✓ appears
5. Press ++c++ → Edit category modal
6. Select "Shopping" → ALL transactions from all 3 merchants recategorized!

<!-- TODO: Add screenshot of multiple selected merchants with ✓ marks -->

**Available bulk operations:**

- ++m++ - Rename merchant for all transactions in selected groups
- ++c++ - Change category for all transactions in selected groups
- ++h++ - Hide/unhide all transactions in selected groups from reports

This works in **all aggregate views**:

- **Merchants view** - Select multiple merchants, edit all their transactions
- **Categories view** - Select multiple categories, edit all their transactions
- **Groups view** - Select multiple groups, edit all their transactions
- **Accounts view** - Select multiple accounts, edit all their transactions
- **Sub-grouped views** - Select multiple sub-groups, edit all their transactions

**Visual indicators:**

- `✓` - Group is selected
- `*` - Group has pending edits
- `✓*` - Group is selected AND has pending edits

<!-- TODO: Add screenshot showing ✓ and * indicators in same view -->

## Workflow Examples

### Clean Up All Coffee Purchases

**Goal:** Rename all coffee-related merchants to consistent names

1. Press ++slash++ → search "coffee"
2. Merchants view shows filtered results
3. ++space++ on "STARBUCKS*123" → ✓
4. ++space++ on "Starbucks Coffee" → ✓
5. ++space++ on "SBUX*456" → ✓
6. Press ++m++ → Edit merchant
7. Type "Starbucks" → All renamed
8. ++escape++ → Clear search
9. See consolidated "Starbucks" merchant

<!-- TODO: Add before/after screenshots of merchant cleanup -->

### Recategorize Online Shopping

**Goal:** Move Amazon, eBay, and Etsy to "Online Shopping" category

1. ++g++ to Merchants view
2. ++space++ on "Amazon" → ✓
3. ++space++ on "eBay" → ✓
4. ++space++ on "Etsy" → ✓
5. Press ++c++ → Edit category
6. Type "online" to filter → Select "Online Shopping"
7. All transactions from 3 merchants updated

### Analyze Then Edit

**Goal:** Find expensive groceries from specific stores

1. ++g++ to Categories → ++enter++ on "Groceries"
2. ++g++ to see "(by Merchant)"
3. ++space++ select expensive merchants
4. Press ++c++ to recategorize to "Dining Out" (maybe they weren't groceries)

## Undo Pending Edits

Made a mistake while queueing edits? Press `u` to undo the most recent pending edit.

**How it works:**

- **Removes last edit** - Edits are undone in reverse order (last in, first out)
- **Press multiple times** - Each `u` press removes one more edit
- **Preserves position** - Cursor and scroll stay where they are
- **Shows feedback** - Notification displays: "Undone {Field} edit (N remaining)"
- **Pending only** - Committed changes cannot be undone

**Example workflow:**

1. **Edit merchant** - Change transaction 1 merchant → queued
2. **Edit merchant** - Change transaction 2 merchant → queued
3. **Edit category** - Change transaction 3 category → queued
4. **Press `u`** - Undoes category edit (most recent)
5. **Press `u`** - Undoes merchant edit on transaction 2
6. **Press `u`** - Undoes merchant edit on transaction 1

**When to use:**

This is useful when you've queued multiple edits and realize one was a mistake,
without having to discard all your pending changes.

## Review Before Commit

All edits are queued locally until you commit:

1. Press ++w++ to review all pending changes
2. See table showing: Type | Transaction | Field | Old Value → New Value
3. Press ++enter++ to commit
4. Or press ++escape++ to cancel and return without committing

<!-- TODO: Add screenshot of review changes screen -->

The `*` indicator shows which transactions/groups have pending edits before you commit.
If you need to remove edits before reviewing, use `u` to undo them one by one.

## Tips

!!! tip "Multi-Select Strategy"
    - Use ++space++ liberally - select all items you want to change
    - Edit once instead of editing each item individually
    - Especially powerful for cleaning up messy merchant names

!!! tip "Combine with Search"
    - Search to filter
    - Multi-select from results
    - Bulk edit
    - Clear search to see full results

!!! tip "Aggregate View Power"
    - Select entire groups (merchants/categories) with one ++space++
    - Edit hundreds of transactions across multiple groups in seconds
    - Much faster than selecting individual transactions

!!! tip "Visual Feedback"
    - `✓` shows what you've selected
    - `*` shows what has pending edits
    - Both can appear together: `✓*`
    - Review screen shows all changes before committing

## Summary

| Context | Key | Action |
|---------|-----|--------|
| Any view | ++space++ | Select current row |
| Detail view | ++m++ / ++c++ / ++h++ | Edit selected transaction(s) |
| Detail view | ++x++ | Delete selected transaction(s) |
| Aggregate view | ++m++ / ++c++ | Edit transactions in current group |
| Aggregate view (multi-select) | ++m++ / ++c++ | Edit transactions in ALL selected groups |
| Any view | ++u++ | Undo most recent pending edit |
| Any view | ++w++ | Review pending changes |
| Review screen | ++enter++ | Commit all changes |
| Review screen | ++escape++ | Cancel (keep edits queued) |

Multi-select works consistently across all views for maximum productivity.
