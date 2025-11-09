# YNAB Tracking Accounts - Auto-Hide Transactions

**Status:** Draft
**Created:** 2025-11-09
**Author:** System

## Overview

Automatically mark transactions from YNAB tracking accounts as hidden in moneyflow. Tracking accounts (off-budget accounts) in YNAB include investment accounts, mortgages, and other assets/liabilities that don't directly affect the budget but are tracked for net worth calculations.

## Background

### YNAB Account Types

YNAB has three types of accounts:

1. **Budget Accounts** (`on_budget=True`)
   - Checking accounts
   - Savings accounts
   - Credit cards
   - Lines of credit
   - Transactions affect the budget

2. **Tracking Accounts** (`on_budget=False`)
   - Investment accounts (401k, IRA, brokerage)
   - Mortgages and loans
   - Hard assets (cars, jewelry, real estate)
   - Other liabilities
   - Transactions do NOT affect the budget
   - Used only for net worth tracking

### YNAB API Account Model

The Account model exposes the `on_budget` boolean property:

```python
class Account:
    id: str
    name: str
    type: AccountType
    on_budget: bool  # True = budget account, False = tracking account
    closed: bool
    balance: int
    # ... other properties
```

## Problem Statement

Currently, moneyflow displays all transactions from YNAB regardless of whether they come from budget or tracking accounts. For users focused on budget analysis, tracking account transactions (investments, mortgages, etc.) are noise that should be filtered out by default.

## Proposed Solution

Automatically set `hideFromReports=True` for all transactions that belong to tracking accounts (`on_budget=False`).

### User Benefits

1. **Cleaner reports** - Only budget-affecting transactions shown by default
2. **Better insights** - Focus on actual spending vs. investment movements
3. **Consistent behavior** - Aligns with YNAB's budget-centric philosophy
4. **Manual override available** - Users can still toggle visibility if needed

## Implementation Plan

### 1. Add Account Caching to YNABClient

**Location:** `moneyflow/ynab_client.py`

Add account cache to store account metadata:

```python
class YNABClient:
    def __init__(self):
        # ... existing code ...
        self._account_cache: Optional[Dict[str, Dict[str, Any]]] = None
```

### 2. Fetch Accounts During Login

**Location:** `moneyflow/ynab_client.py:login()`

Fetch all accounts and build the cache:

```python
def login(self, access_token: str) -> None:
    # ... existing authentication code ...

    # Fetch accounts and build cache
    self._fetch_and_cache_accounts()
```

### 3. Create Account Fetching Method

**Location:** `moneyflow/ynab_client.py`

```python
def _fetch_and_cache_accounts(self) -> None:
    """
    Fetch all accounts from YNAB and cache account metadata.

    Caches account information including on_budget status to determine
    if transactions should be hidden from reports.
    """
    self._ensure_authenticated()

    accounts_api = ynab.AccountsApi(self.api_client)
    response = accounts_api.get_accounts(budget_id=self.budget_id)

    self._account_cache = {
        account.id: {
            "id": account.id,
            "name": account.name,
            "on_budget": account.on_budget,
            "closed": account.closed,
            "type": str(account.type) if account.type else "unknown",
        }
        for account in response.data.accounts
    }

    logger.info(
        f"Cached {len(self._account_cache)} accounts "
        f"({sum(1 for a in self._account_cache.values() if not a['on_budget'])} tracking)"
    )
```

### 4. Update Transaction Conversion

**Location:** `moneyflow/ynab_client.py:_convert_transaction()`

Modify the `hideFromReports` logic to include tracking account check:

```python
def _convert_transaction(self, txn: Any) -> Dict[str, Any]:
    """
    Convert a YNAB transaction to moneyflow-compatible format.

    Transactions are hidden from reports if:
    1. They are deleted (txn.deleted)
    2. They are transfers (txn.transfer_account_id is not None)
    3. They belong to a tracking account (on_budget=False)
    """
    # Check if transaction belongs to a tracking account
    is_tracking_account = False
    if self._account_cache and txn.account_id in self._account_cache:
        is_tracking_account = not self._account_cache[txn.account_id]["on_budget"]

    return {
        "id": txn.id,
        "date": str(txn.var_date),
        "amount": float(txn.amount) / 1000.0,
        "merchant": {
            "id": txn.payee_id or "unknown",
            "name": txn.payee_name or "Unknown",
        },
        "category": {
            "id": txn.category_id or "uncategorized",
            "name": txn.category_name or "Uncategorized",
        },
        "account": {
            "id": txn.account_id,
            "displayName": txn.account_name,
        },
        "notes": txn.memo or "",
        "hideFromReports": (
            txn.deleted
            or txn.transfer_account_id is not None
            or is_tracking_account  # NEW: Hide tracking account transactions
        ),
        "pending": txn.cleared == "uncleared",
        "isRecurring": False,
    }
```

### 5. Clear Account Cache When Needed

Update `_invalidate_cache()` to also clear account cache when transactions change (in case account assignments change):

```python
def _invalidate_cache(self) -> None:
    """Clear transaction and account caches."""
    self._transaction_cache = None
    self._cache_params = None
    # Note: Account cache is NOT cleared here - accounts rarely change
```

Update `close()` to clear account cache:

```python
def close(self) -> None:
    """Close the API client and clear all state."""
    self.api_client = None
    self.access_token = None
    self.budget_id = None
    self._transaction_cache = None
    self._cache_params = None
    self._account_cache = None  # NEW
```

## Testing Strategy

### Unit Tests

**Location:** `tests/test_ynab_backend.py` or new `tests/test_ynab_tracking_accounts.py`

#### Test 1: Account Caching on Login

```python
def test_login_caches_accounts():
    """Test that login fetches and caches account information."""
    client = YNABClient()
    # Mock the API responses
    client.login("test-token")

    assert client._account_cache is not None
    assert len(client._account_cache) > 0
```

#### Test 2: Tracking Account Transactions Hidden

```python
def test_tracking_account_transactions_hidden():
    """Test that transactions from tracking accounts are hidden."""
    client = YNABClient()
    client.login("test-token")

    # Set up mock account cache
    client._account_cache = {
        "account-budget": {"id": "account-budget", "on_budget": True, "name": "Checking"},
        "account-tracking": {"id": "account-tracking", "on_budget": False, "name": "401k"},
    }

    # Mock transaction from tracking account
    tracking_txn = MockTransaction(
        account_id="account-tracking",
        deleted=False,
        transfer_account_id=None,
    )

    converted = client._convert_transaction(tracking_txn)
    assert converted["hideFromReports"] is True
```

#### Test 3: Budget Account Transactions Visible

```python
def test_budget_account_transactions_visible():
    """Test that transactions from budget accounts remain visible."""
    client = YNABClient()
    client.login("test-token")

    # Set up mock account cache
    client._account_cache = {
        "account-budget": {"id": "account-budget", "on_budget": True, "name": "Checking"},
    }

    # Mock transaction from budget account
    budget_txn = MockTransaction(
        account_id="account-budget",
        deleted=False,
        transfer_account_id=None,
    )

    converted = client._convert_transaction(budget_txn)
    assert converted["hideFromReports"] is False
```

#### Test 4: Missing Account Cache Handling

```python
def test_missing_account_cache_no_error():
    """Test that missing account cache doesn't cause errors."""
    client = YNABClient()
    client._account_cache = None  # Simulate missing cache

    txn = MockTransaction(account_id="unknown-account")
    converted = client._convert_transaction(txn)

    # Should not crash, and should not hide transaction
    assert converted["hideFromReports"] is False
```

#### Test 5: Unknown Account ID Handling

```python
def test_unknown_account_id_not_hidden():
    """Test that transactions from unknown accounts are not hidden."""
    client = YNABClient()
    client._account_cache = {
        "known-account": {"id": "known-account", "on_budget": True},
    }

    txn = MockTransaction(account_id="unknown-account")
    converted = client._convert_transaction(txn)

    # Unknown accounts should not be hidden by default
    assert converted["hideFromReports"] is False
```

#### Test 6: Deleted Transactions Still Hidden

```python
def test_deleted_transactions_still_hidden():
    """Test that deleted transactions remain hidden regardless of account type."""
    client = YNABClient()
    client._account_cache = {
        "account-budget": {"id": "account-budget", "on_budget": True},
    }

    deleted_txn = MockTransaction(
        account_id="account-budget",
        deleted=True,
    )

    converted = client._convert_transaction(deleted_txn)
    assert converted["hideFromReports"] is True
```

#### Test 7: Transfer Transactions Still Hidden

```python
def test_transfer_transactions_still_hidden():
    """Test that transfer transactions remain hidden regardless of account type."""
    client = YNABClient()
    client._account_cache = {
        "account-budget": {"id": "account-budget", "on_budget": True},
    }

    transfer_txn = MockTransaction(
        account_id="account-budget",
        transfer_account_id="other-account",
    )

    converted = client._convert_transaction(transfer_txn)
    assert converted["hideFromReports"] is True
```

#### Test 8: Combined Hide Conditions

```python
def test_multiple_hide_conditions():
    """Test that hideFromReports is True if ANY condition is met."""
    client = YNABClient()
    client._account_cache = {
        "tracking": {"id": "tracking", "on_budget": False},
    }

    # Tracking account + deleted + transfer = still hidden (not "double hidden")
    txn = MockTransaction(
        account_id="tracking",
        deleted=True,
        transfer_account_id="other",
    )

    converted = client._convert_transaction(txn)
    assert converted["hideFromReports"] is True
```

### Integration Tests

#### Test 9: End-to-End Workflow

```python
@pytest.mark.integration
def test_ynab_tracking_accounts_e2e():
    """Test complete workflow from login to transaction fetching."""
    backend = YNABBackend()
    await backend.login(password=os.getenv("YNAB_TOKEN"))

    transactions = await backend.get_transactions(limit=1000)

    # Verify some transactions are hidden (assuming test account has tracking accounts)
    all_txns = transactions["allTransactions"]["results"]
    hidden_count = sum(1 for txn in all_txns if txn["hideFromReports"])

    assert hidden_count > 0, "Expected some hidden transactions from tracking accounts"
```

### Manual Testing Checklist

- [ ] Test with real YNAB account containing both budget and tracking accounts
- [ ] Verify transactions from 401k/IRA accounts are hidden
- [ ] Verify transactions from checking/savings accounts remain visible
- [ ] Verify mortgage/loan transactions are hidden
- [ ] Test toggle visibility (H key) still works for tracking account transactions
- [ ] Test filtering by hidden status (`hidden_from_reports` parameter)
- [ ] Verify performance - account fetching should not slow down login significantly

## Performance Considerations

### Account Fetching

- **When:** Once during login
- **Cost:** Single API call to `GET /budgets/{budget_id}/accounts`
- **Typical size:** 5-20 accounts for most users
- **Impact:** Negligible (< 100ms)

### Account Cache Lookup

- **When:** For every transaction during conversion
- **Cost:** O(1) dictionary lookup
- **Impact:** Negligible (< 1μs per transaction)

### Memory Usage

- **Account cache size:** ~500 bytes per account
- **Typical usage:** 5-20 accounts = 2.5-10 KB
- **Impact:** Negligible

## Edge Cases and Considerations

### 1. Account Cache Missing

**Scenario:** Account cache is `None` or account_id not in cache

**Handling:** Default to `False` (not hidden) to avoid incorrectly hiding transactions

```python
is_tracking_account = False
if self._account_cache and txn.account_id in self._account_cache:
    is_tracking_account = not self._account_cache[txn.account_id]["on_budget"]
```

### 2. Closed Accounts

**Scenario:** User has closed tracking accounts with historical transactions

**Handling:** No special handling needed - closed accounts still have `on_budget` status

### 3. Account Type Changes

**Scenario:** User converts budget account to tracking account in YNAB

**Handling:** Account cache is refreshed on next login. Consider adding a refresh mechanism if this is common.

### 4. Multiple Budgets

**Scenario:** User switches between multiple budgets

**Handling:** Account cache is tied to `budget_id`, cleared on logout/close

### 5. User Wants to See Tracking Transactions

**Scenario:** User wants to analyze investment or mortgage transactions

**Handling:** User can toggle visibility with `H` key or filter by `hidden_from_reports=False`

## Backward Compatibility

### Existing Behavior

Currently, YNAB backend sets `hideFromReports` based on:

```python
"hideFromReports": txn.deleted or txn.transfer_account_id is not None
```

### New Behavior

Add tracking account check:

```python
"hideFromReports": (
    txn.deleted
    or txn.transfer_account_id is not None
    or is_tracking_account
)
```

### Impact

- **Breaking change?** No - this is a behavior change, not an API change
- **User-visible impact:** More transactions hidden by default
- **Reversibility:** Users can toggle visibility with `H` key

## Future Enhancements

### 1. Per-Account Hide Configuration

Allow users to configure which accounts to hide via `config.yaml`:

```yaml
ynab:
  hide_tracking_accounts: true  # Default
  hide_specific_accounts:
    - "401k Main"
    - "Mortgage"
  show_specific_accounts:
    - "Investment Brokerage"  # Override for specific tracking account
```

### 2. Account Type Filtering

Add UI option to filter by account type:

- Show only budget accounts
- Show only tracking accounts
- Show all accounts

### 3. Account Selection in UI

Add account selector to allow drilling down by account:

```
Transactions > By Account > Checking ($X,XXX)
                         > Savings ($Y,YYY)
                         > 401k ($ZZ,ZZZ) [hidden by default]
```

### 4. Net Worth View

Create a separate view for tracking accounts focused on net worth:

```
Net Worth
├── Assets
│   ├── 401k: $50,000
│   ├── IRA: $30,000
│   └── Home: $400,000
└── Liabilities
    └── Mortgage: -$300,000
─────────────────────
Total: $180,000
```

## Documentation Updates

### README.md

Add section under YNAB backend documentation:

```markdown
### Tracking Accounts

moneyflow automatically hides transactions from YNAB tracking accounts (off-budget accounts) by default. This includes:

- Investment accounts (401k, IRA, brokerage)
- Mortgages and loans
- Asset accounts (cars, real estate)

To view hidden transactions, press `H` to toggle visibility.
```

### docs/guide/ynab.md

Add detailed explanation of tracking account handling and how to work with them.

## Implementation Checklist

- [ ] Add `_account_cache` to `YNABClient.__init__`
- [ ] Implement `_fetch_and_cache_accounts()` method
- [ ] Call `_fetch_and_cache_accounts()` from `login()`
- [ ] Update `_convert_transaction()` to check `on_budget` status
- [ ] Update `close()` to clear account cache
- [ ] Write unit tests (8 tests outlined above)
- [ ] Write integration test
- [ ] Manual testing with real YNAB account
- [ ] Update README.md
- [ ] Update docs/guide/ynab.md
- [ ] Run full test suite (`uv run pytest -v`)
- [ ] Run type checker (`uv run pyright moneyflow/`)
- [ ] Run linters (`uv run ruff check && ruff format --check`)
- [ ] Check coverage (`uv run pytest --cov`)

## Review Criteria

Before merging:

- [ ] All tests pass (including new tests)
- [ ] Type checking passes with no errors
- [ ] Code coverage maintained or increased
- [ ] Manual testing confirms expected behavior
- [ ] Documentation is clear and complete
- [ ] No performance degradation
- [ ] Edge cases are handled gracefully

## Questions and Open Issues

1. **Should we fetch accounts lazily or eagerly?**
   - **Decision:** Eager (during login) - simpler, one-time cost, avoids edge cases

2. **Should we refresh account cache periodically?**
   - **Decision:** No - only on login. Account properties rarely change.

3. **Should we expose account type in the UI?**
   - **Decision:** Not in v1. Consider for future enhancement.

4. **What if user wants tracking transactions visible by default?**
   - **Decision:** Not configurable in v1. Use `H` key to toggle. Consider config option in future.

## Related Issues

- Issue #XX: Add support for filtering by account type
- Issue #XX: Add net worth view for tracking accounts

## References

- [YNAB API Documentation - Accounts](https://api.ynab.com)
- [YNAB Support - Account Types](https://support.ynab.com/en_us/account-types-an-overview-BkmGM0qCq)
- [YNAB SDK Python - Account Model](https://github.com/ynab/ynab-sdk-python/blob/main/docs/Account.md)
