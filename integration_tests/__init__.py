"""
YNAB Integration Tests

True end-to-end tests against a live YNAB test account.
These tests make real API calls and modify data, so they should
only be run against a dedicated test budget.

Setup:
1. Create a test budget in YNAB (or use an existing empty one)
2. Generate a Personal Access Token at https://app.ynab.com/settings/developer
3. Set environment variable: export YNAB_TEST_API_KEY="your-token-here"
4. Optionally set: export YNAB_TEST_BUDGET_ID="your-budget-id"

Running:
    uv run pytest integration_tests/ -v

Note: These tests are NOT run as part of the regular test suite.
They require a real YNAB account and make destructive changes.
"""
