# Implementation Plan: User Prompt for YNAB Batch Scope

## Overview

When a user renames a merchant in YNAB, and the payee has more transactions than selected, prompt the user to choose between:

- **Rename all** - Bulk payee rename (affects all transactions with that payee)
- **Rename selected only** - Individual transaction updates (affects only queued edits)

## Step 1: Add method to count transactions by payee

**File:** `moneyflow/ynab_client.py`

```python
def get_transaction_count_by_payee(self, payee_name: str) -> int:
    """Return count of transactions with the given payee name."""
    payee_result = self._find_or_create_payee(payee_name)
    if not payee_result["payee"]:
        return 0

    transactions_api = ynab.TransactionsApi(self.api_client)
    response = transactions_api.get_transactions_by_payee(
        budget_id=self.budget_id,
        payee_id=payee_result["payee"].id
    )
    return len(response.data.transactions)
```

## Step 2: Add method to backend base class (optional)

**File:** `moneyflow/backends/base.py`

```python
def get_transaction_count_by_merchant(self, merchant_name: str) -> int | None:
    """Return count of transactions with merchant, or None if not supported."""
    return None  # Default: not supported
```

## Step 3: Create prompt screen

**File:** `moneyflow/screens/batch_scope_screen.py`

```python
class BatchScopeScreen(ModalScreen):
    """Prompt user to choose batch scope for merchant rename."""

    def __init__(
        self,
        merchant_name: str,
        selected_count: int,
        total_count: int
    ):
        self.merchant_name = merchant_name
        self.selected_count = selected_count
        self.total_count = total_count
        super().__init__()

    def compose(self) -> ComposeResult:
        yield Label(f"Rename '{self.merchant_name}'")
        yield Label(
            f"You selected {self.selected_count} transactions, "
            f"but {self.total_count} exist with this payee."
        )
        yield Button("Rename all {total_count}", id="all")
        yield Button("Rename selected {selected_count} only", id="selected")
        yield Button("Cancel", id="cancel")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        self.dismiss(event.button.id)  # "all", "selected", or "cancel"
```

## Step 4: Modify commit flow to check and prompt

**File:** `moneyflow/data_manager.py`

Add a pre-commit check method:

```python
async def check_batch_scope(
    self, edits: List[TransactionEdit]
) -> Dict[Tuple[str, str], Dict[str, int]]:
    """
    Check if any merchant renames would affect more transactions than selected.

    Returns:
        Dict mapping (old_merchant, new_merchant) to:
        {"selected": count_in_queue, "total": count_on_backend}
    """
    if not hasattr(self.mm, "get_transaction_count_by_merchant"):
        return {}

    merchant_edits = [e for e in edits if e.field == "merchant"]

    # Group by (old, new)
    groups: Dict[Tuple[str, str], List] = {}
    for edit in merchant_edits:
        key = (edit.old_value, edit.new_value)
        groups.setdefault(key, []).append(edit)

    result = {}
    for (old_name, new_name), group_edits in groups.items():
        total = await asyncio.to_thread(
            self.mm.get_transaction_count_by_merchant, old_name
        )
        if total and total > len(group_edits):
            result[(old_name, new_name)] = {
                "selected": len(group_edits),
                "total": total
            }

    return result
```

## Step 5: Modify app.py commit flow

**File:** `moneyflow/app.py`

Before committing, check and prompt:

```python
async def _review_and_commit(self) -> None:
    # ... existing code ...

    if should_commit:
        # Check for batch scope mismatches (YNAB only)
        scope_mismatches = await self.data_manager.check_batch_scope(
            self.data_manager.pending_edits
        )

        # Track user choices: which renames should use individual updates
        use_individual_updates: Set[Tuple[str, str]] = set()

        for (old_name, new_name), counts in scope_mismatches.items():
            choice = await self.push_screen(
                BatchScopeScreen(
                    old_name,
                    counts["selected"],
                    counts["total"]
                ),
                wait_for_dismiss=True
            )

            if choice == "cancel":
                return  # Abort commit
            elif choice == "selected":
                use_individual_updates.add((old_name, new_name))
            # "all" → use batch (default behavior)

        # Pass user choices to commit
        success, failure, bulk_renames = await self._commit_with_retry(
            self.data_manager.pending_edits,
            skip_batch_for=use_individual_updates  # New parameter
        )
```

## Step 6: Modify commit_pending_edits to respect user choice

**File:** `moneyflow/data_manager.py`

```python
async def commit_pending_edits(
    self,
    edits: List[Any],
    skip_batch_for: Set[Tuple[str, str]] | None = None
) -> Tuple[int, int, Set[Tuple[str, str]]]:
    """
    Args:
        skip_batch_for: Set of (old, new) merchant renames to process
            individually instead of using batch update.
    """
    skip_batch_for = skip_batch_for or set()

    # ... existing code ...

    for (old_name, new_name), group_edits in merchant_groups.items():
        # User chose individual updates for this rename
        if (old_name, new_name) in skip_batch_for:
            failed_batch_edits.extend(group_edits)
            continue

        # Try batch update as before
        result = await asyncio.to_thread(
            self.mm.batch_update_merchant, old_name, new_name
        )
        # ... rest of existing logic ...
```

## Step 7: Update tests

Add tests for:

- `get_transaction_count_by_payee` in `test_ynab_backend.py`
- `check_batch_scope` in `test_data_manager.py`
- `BatchScopeScreen` in new `test_batch_scope_screen.py`
- Commit flow with `skip_batch_for` parameter

## Summary

| File | Changes |
|------|---------|
| `ynab_client.py` | Add `get_transaction_count_by_payee()` |
| `backends/base.py` | Add optional `get_transaction_count_by_merchant()` |
| `screens/batch_scope_screen.py` | New prompt screen |
| `data_manager.py` | Add `check_batch_scope()`, modify `commit_pending_edits()` |
| `app.py` | Add pre-commit prompt flow |
| Tests | New tests for all components |
