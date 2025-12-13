# YNAB Integration Tests

True end-to-end tests against a live YNAB test account.

## Purpose

These tests verify the correct functioning of the YNAB backend integration
by making real API calls. They are designed to test:

- Payee renaming and cascade to transactions
- Batch transaction reassignment (payee merging)
- Duplicate payee detection
- Transaction counting by payee
- Cache invalidation

## Setup

1. **Create a test budget in YNAB**
   - Go to [YNAB](https://app.ynab.com) and create a new budget specifically for testing
   - **Add at least one on-budget account** (e.g., "Test Checking")
     - Go to Settings → Accounts → Add Account
     - Choose "Checking" or any on-budget account type
     - This is required to create test transactions
   - This budget will have test data created and deleted during tests
   - Do NOT use your personal budget!

2. **Generate a Personal Access Token**
   - Go to [YNAB Developer Settings](https://app.ynab.com/settings/developer)
   - Create a new Personal Access Token
   - Save this token securely

3. **Set environment variables**

   ```bash
   export YNAB_TEST_API_KEY="your-personal-access-token"

   # Optional: specify a budget ID (uses first budget if not set)
   export YNAB_TEST_BUDGET_ID="your-test-budget-id"
   ```

## Running Tests

```bash
# Run all integration tests
uv run pytest integration_tests/ -v

# Run a specific test file
uv run pytest integration_tests/test_ynab_payee_operations.py -v

# Run a specific test
uv run pytest integration_tests/test_ynab_payee_operations.py::TestPayeeRenaming::test_rename_payee_cascades_to_transactions -v

# Skip integration tests (for regular development)
uv run pytest tests/ -v  # Only runs unit tests
```

## Test Isolation

- Each test creates transactions with a unique prefix (e.g., `__test_a1b2c3d4_`)
- Transactions created during tests are automatically deleted in cleanup
- Payees cannot be deleted via YNAB API, but test payees are easily identifiable

## What These Tests Verify

### Payee Renaming
- Creating multiple transactions with the same payee
- Using `batch_update_merchant` to rename the payee
- Verifying all transactions reflect the new name (cascade works)
- Verifying no-op renames (same name) don't make API calls

### Batch Transaction Reassignment
- Creating transactions under two different payees
- "Merging" one payee into another (target already exists)
- Verifying transactions are reassigned to target payee
- Verifying the batch reassign API is used (not individual updates)

### Duplicate Payee Detection
- Detecting when a payee name is unique
- Handling nonexistent payee lookups gracefully

### Transaction Count by Payee
- Counting transactions for a given payee name
- Returning 0 for nonexistent payees

### Cache Invalidation
- Verifying transaction cache is cleared after payee updates
- Ensuring subsequent fetches get fresh data

## Troubleshooting

### "YNAB_TEST_API_KEY not set"
Set the environment variable as described in Setup.

### "Test budget has no accounts"
Your test budget needs at least one on-budget account to create transactions.
- Go to https://app.ynab.com/settings/accounts
- Add a checking account or any on-budget account type
- Run the tests again

### Tests are slow
These tests make real API calls, so they're slower than unit tests.
Consider running them separately from the main test suite.

### Leftover test payees
Payees with the `__test_` prefix are from tests. YNAB doesn't support
deleting payees, but you can ignore them or manually hide them.

### Rate limiting
YNAB has API rate limits. If you see 429 errors, wait a few minutes
and try again. The tests include small delays to help avoid this.
