"""
Integration tests for YNAB payee operations.

Tests the key functionality added in the batch payee updates PR:
1. Payee renaming (cascades to all transactions)
2. Batch transaction reassignment (when merging payees)
3. Duplicate payee detection
4. Transaction count by payee

These tests create real transactions and payees in a test YNAB budget,
verify the operations work correctly, then clean up.
"""

import time
from typing import Any, Callable, Dict, List

from moneyflow.ynab_client import YNABClient


class TestPayeeRenaming:
    """
    Tests for renaming payees and verifying the cascade to transactions.

    This is the core optimization in the PR: instead of updating each
    transaction individually (N API calls), we update the payee once
    and YNAB cascades the change to all transactions automatically.
    """

    def test_rename_payee_cascades_to_transactions(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        get_payee_by_name: Callable[[str], Dict[str, Any]],
        get_transactions_by_payee: Callable[[str], List[Dict[str, Any]]],
        test_payee_prefix: str,
    ):
        """
        Test that renaming a payee updates all transactions using that payee.

        This verifies the core optimization from the PR:
        - Create multiple transactions with the same payee
        - Use batch_update_merchant to rename the payee
        - Verify all transactions now show the new payee name
        """
        old_name = "OldMerchant_CascadeTest"
        new_name = "NewMerchant_CascadeTest"
        full_old_name = f"{test_payee_prefix}{old_name}"
        full_new_name = f"{test_payee_prefix}{new_name}"

        # Create 3 transactions with the same payee
        txn1 = create_test_transaction(old_name, -10.00)
        txn2 = create_test_transaction(old_name, -20.00)
        txn3 = create_test_transaction(old_name, -30.00)

        # Verify all transactions have the same payee_id
        assert txn1["payee_id"] == txn2["payee_id"] == txn3["payee_id"]
        original_payee_id = txn1["payee_id"]

        # Get the payee to verify it exists
        payee = get_payee_by_name(full_old_name)
        assert payee is not None
        assert payee["id"] == original_payee_id

        # Use batch_update_merchant to rename the payee
        result = ynab_client.batch_update_merchant(full_old_name, full_new_name)

        # Verify the batch update succeeded
        assert result["success"] is True
        assert result["method"] == "payee_update"
        assert result["payee_id"] == original_payee_id

        # Allow time for YNAB to propagate the change
        time.sleep(0.5)

        # Verify the old payee name no longer exists
        old_payee = get_payee_by_name(full_old_name)
        assert old_payee is None, f"Old payee '{full_old_name}' should not exist after rename"

        # Verify the new payee name exists
        new_payee = get_payee_by_name(full_new_name)
        assert new_payee is not None
        assert new_payee["id"] == original_payee_id, "Payee ID should remain the same after rename"

        # Verify all transactions now have the new payee name
        transactions = get_transactions_by_payee(original_payee_id)
        assert len(transactions) >= 3  # May have more if tests weren't cleaned up

        for txn in transactions:
            if txn["id"] in [txn1["id"], txn2["id"], txn3["id"]]:
                assert txn["payee_name"] == full_new_name, (
                    f"Transaction {txn['id']} should have new payee name"
                )

    def test_rename_to_same_name_is_noop(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that renaming a payee to the same name is a no-op.

        This is an optimization added in the PR to avoid unnecessary API calls
        and prevent potential issues with duplicate payees.
        """
        payee_name = "SameName_NoOpTest"
        full_name = f"{test_payee_prefix}{payee_name}"

        # Create a transaction to create the payee
        create_test_transaction(payee_name, -15.00)

        # Try to rename to the same name
        result = ynab_client.batch_update_merchant(full_name, full_name)

        # Verify it's recognized as a no-op
        assert result["success"] is True
        assert result["method"] == "no_change"
        assert result["transactions_affected"] == 0
        assert "same" in result["message"].lower()


class TestBatchPayeeReassignment:
    """
    Tests for batch reassigning transactions from one payee to another.

    This handles the case where you want to "merge" two payees - the target
    payee already exists, so instead of renaming (which would create a duplicate),
    we reassign all transactions from the old payee to the existing target payee.
    """

    def test_batch_reassign_when_target_exists(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        get_payee_by_name: Callable[[str], Dict[str, Any]],
        get_transactions_by_payee: Callable[[str], List[Dict[str, Any]]],
        test_payee_prefix: str,
    ):
        """
        Test batch reassignment when the target payee already exists.

        Scenario:
        - User has transactions with "Amazon.com/abc123"
        - User wants to rename them to "Amazon" (which already exists)
        - Instead of creating a duplicate "Amazon" payee, we should
          reassign all transactions from "Amazon.com/abc123" to the
          existing "Amazon" payee
        """
        source_name = "SourceMerchant_MergeTest"
        target_name = "TargetMerchant_MergeTest"
        full_source = f"{test_payee_prefix}{source_name}"
        full_target = f"{test_payee_prefix}{target_name}"

        # Create transactions for the target payee first (it must exist)
        target_txn = create_test_transaction(target_name, -100.00)
        target_payee_id = target_txn["payee_id"]

        # Create transactions for the source payee
        source_txn1 = create_test_transaction(source_name, -25.00)
        source_txn2 = create_test_transaction(source_name, -35.00)
        source_payee_id = source_txn1["payee_id"]

        # Verify we have two different payees
        assert source_payee_id != target_payee_id

        # Use batch_update_merchant to "merge" source into target
        result = ynab_client.batch_update_merchant(full_source, full_target)

        # Verify batch reassignment was used
        assert result["success"] is True
        assert result["method"] == "batch_reassign"
        assert result["payee_id"] == target_payee_id
        assert result["transactions_affected"] == 2

        # Allow time for YNAB to propagate the change
        time.sleep(0.5)

        # Verify source transactions now belong to target payee
        target_transactions = get_transactions_by_payee(target_payee_id)
        target_txn_ids = [t["id"] for t in target_transactions]

        assert source_txn1["id"] in target_txn_ids
        assert source_txn2["id"] in target_txn_ids
        assert target_txn["id"] in target_txn_ids

        # Verify transactions have the target payee name
        for txn in target_transactions:
            if txn["id"] in [source_txn1["id"], source_txn2["id"]]:
                assert txn["payee_name"] == full_target

    def test_batch_reassign_empty_source(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test batch reassignment when source payee has no transactions.

        This can happen if transactions were already moved or deleted.
        """
        source_name = "EmptySource_Test"
        target_name = "ExistingTarget_Test"
        full_source = f"{test_payee_prefix}{source_name}"
        full_target = f"{test_payee_prefix}{target_name}"

        # Create target payee with a transaction
        create_test_transaction(target_name, -50.00)

        # Create source payee, then delete its transaction
        source_txn = create_test_transaction(source_name, -10.00)
        ynab_client.delete_transaction(source_txn["id"])

        # Wait a moment for the deletion to propagate
        time.sleep(0.5)

        # Try to merge source into target
        result = ynab_client.batch_update_merchant(full_source, full_target)

        # Should either succeed with 0 transactions or report payee not found
        # (depending on whether YNAB keeps the payee after deleting all transactions)
        if result["success"]:
            assert result["transactions_affected"] == 0


class TestDuplicatePayeeDetection:
    """
    Tests for detecting duplicate payees.

    YNAB can end up with multiple payees with the same name (duplicates).
    The PR adds detection to warn users and prevent unsafe batch operations.
    """

    def test_find_payee_detects_single_payee(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that _find_or_create_payee correctly finds a unique payee.
        """
        payee_name = "UniquePayee_DetectionTest"
        full_name = f"{test_payee_prefix}{payee_name}"

        # Create a transaction to create the payee
        create_test_transaction(payee_name, -20.00)

        # Find the payee
        result = ynab_client._find_or_create_payee(full_name)

        assert result["payee"] is not None
        assert result["payee"].name == full_name
        assert result["duplicates_found"] is False
        assert result["duplicate_ids"] == []

    def test_batch_update_rejects_duplicate_target_payee(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that batch_update_merchant rejects merges when target has duplicates.

        Note: YNAB's API typically prevents creating duplicate payees, so this
        test verifies the defensive check is in place even though duplicates
        are rare in practice (usually only happen via manual UI operations or
        imports). We test this by mocking the duplicate detection response.
        """
        from unittest.mock import patch

        source_name = "SourcePayee_DuplicateTargetTest"
        target_name = "TargetPayee_DuplicateTargetTest"
        full_source = f"{test_payee_prefix}{source_name}"
        full_target = f"{test_payee_prefix}{target_name}"

        # Create source payee via transaction
        create_test_transaction(source_name, -10.00)

        # Create target payee via transaction
        target_txn = create_test_transaction(target_name, -20.00)

        # Mock _find_or_create_payee to simulate duplicate target payees
        original_find = ynab_client._find_or_create_payee

        def mock_find_payee(merchant_name: str):
            result = original_find(merchant_name)
            # If this is the target payee, simulate duplicates
            if merchant_name == full_target and result["payee"]:
                result["duplicates_found"] = True
                result["duplicate_ids"] = [result["payee"].id, "fake-duplicate-id"]
            return result

        with patch.object(ynab_client, "_find_or_create_payee", side_effect=mock_find_payee):
            # Try to merge source into target (which has duplicates)
            result = ynab_client.batch_update_merchant(full_source, full_target)

        # Should fail with duplicate target error
        assert result["success"] is False
        assert result["method"] == "duplicate_target_payees_found"
        assert "duplicate" in result["message"].lower()
        assert "duplicate_ids" in result
        assert len(result["duplicate_ids"]) == 2

    def test_batch_update_with_nonexistent_payee(
        self,
        ynab_client: YNABClient,
        test_payee_prefix: str,
    ):
        """
        Test batch_update_merchant when the source payee doesn't exist.

        This verifies proper error handling for the "payee not found" case.
        """
        nonexistent_name = f"{test_payee_prefix}NonExistentPayee_{time.time()}"
        new_name = f"{test_payee_prefix}NewName_{time.time()}"

        result = ynab_client.batch_update_merchant(nonexistent_name, new_name)

        assert result["success"] is False
        assert result["method"] == "payee_not_found"
        assert result["payee_id"] is None
        assert "not found" in result["message"].lower()


class TestTransactionCountByPayee:
    """
    Tests for the get_transaction_count_by_payee method.

    This is used to determine if a batch merchant rename would affect
    more transactions than are currently selected in the UI.
    """

    def test_count_transactions_by_payee(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that get_transaction_count_by_payee returns correct count.
        """
        payee_name = "CountTest_Payee"
        full_name = f"{test_payee_prefix}{payee_name}"

        # Create 4 transactions with the same payee
        create_test_transaction(payee_name, -10.00)
        create_test_transaction(payee_name, -20.00)
        create_test_transaction(payee_name, -30.00)
        create_test_transaction(payee_name, -40.00)

        # Count transactions
        count = ynab_client.get_transaction_count_by_payee(full_name)

        assert count == 4

    def test_count_nonexistent_payee_returns_zero(
        self,
        ynab_client: YNABClient,
        test_payee_prefix: str,
    ):
        """
        Test that counting transactions for a nonexistent payee returns 0.
        """
        nonexistent_name = f"{test_payee_prefix}NonExistent_{time.time()}"

        count = ynab_client.get_transaction_count_by_payee(nonexistent_name)

        assert count == 0


class TestPayeeUpdateValidation:
    """
    Tests for payee update validation.
    """

    def test_update_payee_with_valid_name(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        get_payee_by_name: Callable[[str], Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that update_payee works with a valid name.
        """
        old_name = "ValidUpdate_Old"
        new_name = "ValidUpdate_New"
        full_old = f"{test_payee_prefix}{old_name}"
        full_new = f"{test_payee_prefix}{new_name}"

        # Create transaction to create payee
        txn = create_test_transaction(old_name, -15.00)
        payee_id = txn["payee_id"]

        # Update the payee name directly
        result = ynab_client.update_payee(payee_id, full_new)

        assert result is True

        # Verify the name changed
        time.sleep(0.5)
        old_payee = get_payee_by_name(full_old)
        new_payee = get_payee_by_name(full_new)

        assert old_payee is None
        assert new_payee is not None
        assert new_payee["id"] == payee_id

    def test_update_payee_rejects_empty_name(self, ynab_client: YNABClient):
        """
        Test that update_payee rejects empty names.
        """
        # Use a fake payee_id since we expect validation to fail before API call
        result = ynab_client.update_payee("fake-payee-id", "")

        assert result is False

    def test_update_payee_rejects_too_long_name(self, ynab_client: YNABClient):
        """
        Test that update_payee rejects names over 500 characters.
        """
        long_name = "A" * 501
        result = ynab_client.update_payee("fake-payee-id", long_name)

        assert result is False


class TestCacheInvalidation:
    """
    Tests for transaction cache invalidation after payee operations.
    """

    def test_cache_invalidated_after_batch_update(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that transaction cache is invalidated after batch_update_merchant.

        This ensures subsequent get_transactions calls fetch fresh data
        with the updated payee names.
        """
        old_name = "CacheTest_Old"
        new_name = "CacheTest_New"
        full_old = f"{test_payee_prefix}{old_name}"
        full_new = f"{test_payee_prefix}{new_name}"

        # Create a transaction
        create_test_transaction(old_name, -25.00)

        # Fetch transactions to populate cache
        ynab_client.get_transactions()
        assert ynab_client._transaction_cache is not None

        # Perform batch update
        ynab_client.batch_update_merchant(full_old, full_new)

        # Verify cache was invalidated
        assert ynab_client._transaction_cache is None

    def test_cache_invalidated_after_payee_update(
        self,
        ynab_client: YNABClient,
        create_test_transaction: Callable[..., Dict[str, Any]],
        test_payee_prefix: str,
    ):
        """
        Test that transaction cache is invalidated after update_payee.
        """
        old_name = "CachePayee_Old"
        new_name = "CachePayee_New"
        full_new = f"{test_payee_prefix}{new_name}"

        # Create a transaction
        txn = create_test_transaction(old_name, -30.00)
        payee_id = txn["payee_id"]

        # Fetch transactions to populate cache
        ynab_client.get_transactions()
        assert ynab_client._transaction_cache is not None

        # Update payee directly
        ynab_client.update_payee(payee_id, full_new)

        # Verify cache was invalidated
        assert ynab_client._transaction_cache is None
