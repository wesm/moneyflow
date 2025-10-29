# Keyboard Shortcuts

moneyflow is designed to be used entirely with the keyboard. Here's your complete reference.

---

## Essential Shortcuts

| Key | Action | Context |
|-----|--------|---------|
| ++question++ | Show help screen | Any |
| ++q++ | Quit (with confirmation) | Any |
| ++ctrl+c++ | Force quit | Any |
| ++w++ | Review & commit changes | Any |

---

## View Navigation

### Cycle Through Views

| Key | Action |
|-----|--------|
| ++g++ | Cycle grouping (Merchant → Category → Group → Account) |
| ++d++ | Detail view (all transactions) |
| ++shift+d++ | Find duplicates |

### Direct View Access

| Key | View |
|-----|------|
| ++shift+a++ | Accounts |

### Drill Down

| Key | Action |
|-----|--------|
| ++enter++ | Drill down into selected row |
| ++escape++ | Go back to previous view |

---

## Time Navigation

### Quick Jumps

| Key | Time Period |
|-----|-------------|
| ++y++ | Current year |
| ++t++ | Current month |
| ++a++ | All time |

### Period Navigation

| Key | Action |
|-----|--------|
| ++left++ | Previous period (month or year) |
| ++right++ | Next period (month or year) |

**Smart navigation**: If viewing a full year, arrows move by year. If viewing a month, arrows move by month.

---

## Editing Transactions

### Single Transaction (Detail View)

| Key | Action |
|-----|--------|
| ++m++ | Edit merchant name |
| ++c++ | Edit category |
| ++h++ | Hide/unhide from reports |
| ++x++ | Delete transaction (with confirmation) |
| ++i++ | View full transaction details |

### Multi-Select

| Key | Action |
|-----|--------|
| ++space++ | Toggle selection (shows ✓) |
| ++m++ | Edit merchant for all selected |
| ++c++ | Edit category for all selected |
| ++h++ | Hide/unhide all selected |
| ++x++ | Delete all selected (with confirmation) |

!!! example "Bulk Workflow"
    1. Press ++space++ on multiple transactions (shows ✓)
    2. Press ++c++ to edit category for all
    3. Select new category
    4. Press ++w++ to review
    5. Press ++enter++ to commit

### Undo

| Key | Action |
|-----|--------|
| ++u++ | Undo most recent pending edit |

Removes the most recent edit from the pending changes queue. Press multiple times to undo edits in reverse order.
Shows notification with field type and remaining edit count.

---

## Bulk Edit from Aggregate View

### Single Group

When viewing **Merchants**, **Categories**, **Groups**, or **Accounts**:

| Key | Action |
|-----|--------|
| ++m++ | Edit merchant for ALL transactions in selected row |
| ++c++ | Edit category for ALL transactions in selected row |
| ++enter++ | Drill down to see individual transactions |

This lets you rename a merchant or recategorize hundreds of transactions in one operation.

### Multi-Select Groups

Press ++space++ to select multiple groups, then edit all their transactions at once:

| Key | Action |
|-----|--------|
| ++space++ | Toggle group selection (shows ✓) |
| ++m++ | Edit merchant for ALL transactions in ALL selected groups |
| ++c++ | Edit category for ALL transactions in ALL selected groups |

!!! example "Multi-Select Workflow"
    1. Merchants view
    2. ++space++ on "Amazon" → ✓
    3. ++space++ on "eBay" → ✓
    4. ++space++ on "Etsy" → ✓
    5. Press ++c++ → Select "Online Shopping"
    6. All transactions from 3 merchants recategorized!

Works in all aggregate views and sub-grouped views.

---

## Sorting

| Key | Action | Context |
|-----|--------|---------|
| ++s++ | Toggle sort field | Any view |
| ++v++ | Reverse sort direction (↑/↓) | Any view |

**In aggregate views** (Merchant/Category/Group):

- ++s++ toggles between Count and Amount

**In detail view** (transactions):

- ++s++ cycles through: Date, Merchant, Category, Account, Amount (repeats)

---

## Search & Filters

| Key | Action |
|-----|--------|
| ++slash++ | Search transactions |
| ++f++ | Show filter modal (transfers, hidden items) |

### In Search Modal

- **Type** to filter in real-time
- ++enter++ to apply search
- ++escape++ to cancel

**To clear an active search:** Press ++slash++ to open search, delete all text, then press ++enter++ with empty input.

---

## Arrow Key Navigation

| Key | Action |
|-----|--------|
| ++up++ / ++k++ | Move cursor up |
| ++down++ / ++j++ | Move cursor down |
| ++page-up++ | Jump up multiple rows |
| ++page-down++ | Jump down multiple rows |
| ++home++ | Jump to top |
| ++end++ | Jump to bottom |

---

## Workflow Shortcuts

### Common Workflows

**Rename a merchant:**

1. ++g++ (until Merchants view)
2. Navigate to merchant
3. ++m++ (edit merchant)
4. Type new name, ++enter++
5. ++w++ (review), ++enter++ (commit)

**Edit categories for transactions:**

1. ++u++ (all transactions)
2. ++space++ on each transaction to select
3. ++c++ (edit category)
4. Type to filter categories, ++enter++ to select
5. ++w++ (review), ++enter++ (commit)

**Monthly spending review:**

1. ++t++ (this month)
2. ++g++ (cycle to categories)
3. ++enter++ on a category to drill down
4. ++left++ to view previous month
5. ++right++ to return

---

## In-Modal Shortcuts

When in a modal dialog (edit merchant, select category, etc.):

| Key | Action |
|-----|--------|
| ++enter++ | Confirm/Submit |
| ++escape++ | Cancel |
| ++tab++ | Next field |
| ++shift+tab++ | Previous field |
| ++up++ / ++down++ | Navigate list items |

### Category Selector

- **Type** to filter categories in real-time
- ++up++ / ++down++ to navigate matches
- ++enter++ to select

---

## Pro Tips

!!! tip "Speed Up Editing"
    - Stay in detail view (++u++) for rapid transaction editing
    - Use ++space++ to queue multiple edits before committing
    - The cursor stays in place after edits - keep pressing ++m++ or ++c++

!!! tip "Time Navigation"
    - ++left++/++right++ maintain context (month-to-month, year-to-year)
    - ++y++ is your "reset" - always returns to current year

!!! tip "Review Before Committing"
    - ++w++ shows ALL pending changes before saving
    - Review screen shows old → new values
    - Press ++escape++ to cancel, ++enter++ to commit

---

## Cheat Sheet

Print this for reference:

```text
Views:       g (cycle)  d (detail)  D (duplicates)
Time:        y (year)   t (month)  a (all)  ←/→ (navigate)
Edit:        m (merchant)  c (category)  h (hide)  x (delete)  u (undo)
Select:      Space (multi-select)  Ctrl+A (select all)
Sort:        s (toggle field)  v (reverse)
Navigate:    Enter (drill down)  Escape (go back)
Other:       / (search)  f (filter)  w (commit)  ? (help)  q (quit)
```

---

## Can't Remember a Shortcut?

Press ++question++ any time to see the help screen with all available shortcuts for your current view.
