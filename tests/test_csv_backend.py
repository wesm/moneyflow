"""Tests for CsvFinanceBackend."""

import asyncio
import os
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

from moneyflow.backends import csv_backend
from moneyflow.backends.csv_backend import (
    CsvFinanceBackend,
    _secure_open_db,
    _validate_path_components,
    _validate_path_security,
)
from moneyflow.data import macos_acl
from tests.permission_assertions import assert_owner_only_permissions


@pytest.fixture
def tmp_profile_dir(tmp_path):
    profile = tmp_path / "profiles" / "csv_test"
    profile.mkdir(parents=True)
    return profile


@pytest.fixture
def tmp_config_dir(tmp_path):
    config = tmp_path / "config"
    config.mkdir()
    return str(config)


@pytest.fixture
def chase_backend(tmp_profile_dir, tmp_config_dir):
    return CsvFinanceBackend(
        profile_dir=tmp_profile_dir,
        config_dir=tmp_config_dir,
        institution_name="chase_credit",
    )


class TestCsvFinanceBackend:
    def test_get_backend_type(self, chase_backend):
        assert chase_backend.get_backend_type() == "csv_chase_credit"

    def test_login_initializes_and_validates_storage(self, chase_backend):
        """login() must create and validate storage before anything else
        (e.g. DataManager saving category config) writes into the profile."""
        assert not Path(chase_backend.db_path).exists()
        asyncio.run(chase_backend.login())
        assert Path(chase_backend.db_path).exists()

    def test_login_rejects_symlinked_profile_dir(self, tmp_path, tmp_config_dir):
        real_dir = tmp_path / "real_dir"
        real_dir.mkdir()
        linked_profile = tmp_path / "linked_profile"
        linked_profile.symlink_to(real_dir, target_is_directory=True)
        backend = CsvFinanceBackend(
            profile_dir=linked_profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )

        with pytest.raises(OSError, match="symlink"):
            asyncio.run(backend.login())

    def test_capability_flags(self, chase_backend):
        """read_only=True gates the local category/group managers and the
        local-only edit warning in the TUI; can_write_transactions=True lets
        the commit pipeline persist edits to the local database (matching
        the SimpleFIN capability pattern)."""
        assert chase_backend.read_only is True
        assert chase_backend.can_write_transactions is True
        assert chase_backend.supports_category_sync is False

    def test_make_category_id_matches_import_format(self, chase_backend):
        """Locally created categories must get the same "cat_"-prefixed ids
        that imports generate, or one name would split across two ids."""
        assert chase_backend.make_category_id("Food & Dining") == "cat_food_dining"
        assert chase_backend.make_category_id("Groceries") == "cat_groceries"

    def test_category_alias_roundtrip(self, chase_backend):
        chase_backend.record_category_alias("cat_shopping", "cat_fun", "Fun")
        assert chase_backend.get_category_aliases() == {"cat_shopping": ("cat_fun", "Fun")}

    def test_category_alias_chains_flattened(self, chase_backend):
        """A second rename must repoint earlier aliases so import resolution
        is a single lookup, never a chain walk."""
        chase_backend.record_category_alias("cat_a", "cat_b", "B")
        chase_backend.record_category_alias("cat_b", "cat_c", "C")
        assert chase_backend.get_category_aliases() == {
            "cat_a": ("cat_c", "C"),
            "cat_b": ("cat_c", "C"),
        }

    def test_category_alias_rename_back_removes_self_alias(self, chase_backend):
        chase_backend.record_category_alias("cat_a", "cat_b", "B")
        chase_backend.record_category_alias("cat_b", "cat_a", "A")
        assert chase_backend.get_category_aliases() == {"cat_b": ("cat_a", "A")}

    def test_equivalent_stored_names_consolidate_to_one_payload(
        self, chase_backend, tmp_profile_dir
    ):
        """Stored spellings sharing an id (e.g. "Shopping" and "SHOPPING")
        must produce one payload, preferring the configured name and group."""
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c1", "2026-07-12", -5.0, "STORE", "SHOPPING", "cat_shopping"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c2", "2026-07-13", -6.0, "STORE", "Shopping", "cat_shopping"),
        )
        conn.commit()
        conn.close()
        (tmp_profile_dir / "config.yaml").write_text(
            yaml.safe_dump({"fetched_categories": {"Fun": ["Shopping"]}})
        )

        result = asyncio.run(chase_backend.get_transaction_categories())
        assert len(result["categories"]) == 1
        payload = result["categories"][0]
        assert payload["id"] == "cat_shopping"
        assert payload["name"] == "Shopping"
        assert payload["group"]["name"] == "Fun"

    def test_transactions_resolve_canonical_category_names(self, chase_backend, tmp_profile_dir):
        """Transactions must return the same canonical category name that
        get_transaction_categories consolidates to — otherwise equivalent
        spellings aggregate as separate categories with different groups."""
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c1", "2026-07-12", -5.0, "STORE", "SHOPPING", "cat_shopping"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c2", "2026-07-13", -6.0, "STORE", "Shopping", "cat_shopping"),
        )
        conn.commit()
        conn.close()
        (tmp_profile_dir / "config.yaml").write_text(
            yaml.safe_dump({"fetched_categories": {"Fun": ["Shopping"]}})
        )

        result = asyncio.run(chase_backend.get_transactions(limit=10))
        names = {txn["category"]["name"] for txn in result["results"]}
        assert names == {"Shopping"}

        updated = asyncio.run(chase_backend.update_transaction("c1", merchant_name="NEW"))
        assert updated["updateTransaction"]["transaction"]["category"]["name"] == "Shopping"

    def test_secure_open_db_does_not_mutate_symlinked_profile_dir(self, tmp_path):
        """A symlinked profile directory must be rejected BEFORE any
        permission tightening — otherwise the chmod/DACL change would land
        on whatever directory the symlink points at."""
        real_dir = tmp_path / "real_dir"
        real_dir.mkdir()
        os.chmod(real_dir, 0o755)
        mode_before = os.stat(real_dir).st_mode
        profile = tmp_path / "profile_link"
        profile.symlink_to(real_dir, target_is_directory=True)

        with pytest.raises(OSError, match="symlink"):
            _secure_open_db(str(profile / "transactions.db"))

        assert os.stat(real_dir).st_mode == mode_before

    def test_db_path_derived_from_institution_name(self, chase_backend, tmp_profile_dir):
        expected = str(tmp_profile_dir / "chase_credit_transactions.db")
        assert chase_backend.db_path == expected

    def test_schema_is_created_on_first_connection(self, chase_backend):
        conn = chase_backend._get_connection()
        tables = conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
        ).fetchall()
        conn.close()
        table_names = {row[0] for row in tables}
        assert "transactions" in table_names
        assert "import_history" in table_names

    def test_transactions_table_has_correct_columns(self, chase_backend):
        conn = chase_backend._get_connection()
        cols = conn.execute("PRAGMA table_info(transactions)").fetchall()
        conn.close()
        col_names = {row[1] for row in cols}
        for col in (
            "id",
            "date",
            "amount",
            "merchant",
            "category",
            "category_id",
            "account",
            "notes",
            "extras",
            "hideFromReports",
            "imported_at",
        ):
            assert col in col_names

    def test_insert_and_get_transactions(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, extras) VALUES (?, ?, ?, ?, ?)",
            ("chase_001", "2024-01-15", -12.34, "EXAMPLE GIFT SHOP", '{"raw_category":"Gifts"}'),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.get_transactions(limit=10)
            assert result["totalCount"] == 1
            txn = result["results"][0]
            assert txn["id"] == "chase_001"
            assert txn["date"] == "2024-01-15"
            assert txn["amount"] == -12.34
            assert txn["merchant"]["name"] == "EXAMPLE GIFT SHOP"
            assert txn["raw_category"] == "Gifts"
            assert txn["hideFromReports"] is False
            assert txn["pending"] is False

        asyncio.run(_run())

    def test_update_transaction(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("chase_002", "2026-07-12", -20.0, "OLD MERCHANT"),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.update_transaction(
                "chase_002", merchant_name="NEW MERCHANT"
            )
            assert result["updateTransaction"]["transaction"]["merchant"]["name"] == "NEW MERCHANT"

        asyncio.run(_run())

        conn = chase_backend._get_connection()
        row = conn.execute(
            "SELECT merchant FROM transactions WHERE id = ?", ("chase_002",)
        ).fetchone()
        conn.close()
        assert row[0] == "NEW MERCHANT"

    def test_update_nonexistent_transaction_raises(self, chase_backend):
        """Updating a transaction that does not exist must raise rather than
        silently report success — otherwise the commit pipeline incorrectly
        counts a missed update as persisted."""

        async def _run():
            await chase_backend.update_transaction("chase_does_not_exist", merchant_name="WHATEVER")

        with pytest.raises(ValueError, match="not found|does not exist|no transaction"):
            asyncio.run(_run())

    def test_delete_transaction(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("chase_003", "2026-07-12", -10.0, "TO_DELETE"),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.delete_transaction("chase_003")
            assert result is True

        asyncio.run(_run())

        conn = chase_backend._get_connection()
        exists = conn.execute("SELECT 1 FROM transactions WHERE id = ?", ("chase_003",)).fetchone()
        conn.close()
        assert exists is None

    def test_get_all_merchants(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m1", "2026-07-12", -1.0, "Merch A"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m2", "2026-07-12", -2.0, "Merch A"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m3", "2026-07-12", -3.0, "Merch B"),
        )
        conn.commit()
        conn.close()

        async def _run():
            merchants = await chase_backend.get_all_merchants()
            assert merchants == ["Merch A", "Merch B"]

        asyncio.run(_run())

    def test_get_import_history(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO import_history (filename, record_count, duplicate_count) VALUES (?, ?, ?)",
            ("test.csv", 100, 5),
        )
        conn.commit()
        conn.close()

        history = chase_backend.get_import_history()
        assert len(history) == 1
        assert history[0]["filename"] == "test.csv"
        assert history[0]["record_count"] == 100

    def test_get_database_stats(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("s1", "2026-01-01", -50.0, "M"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("s2", "2026-12-31", -30.0, "M"),
        )
        conn.commit()
        conn.close()

        stats = chase_backend.get_database_stats()
        assert stats["total_transactions"] == 2
        assert stats["total_amount"] == -80.0
        assert stats["earliest_date"] == "2026-01-01"
        assert stats["latest_date"] == "2026-12-31"

    def test_db_file_has_restrictive_permissions(self, chase_backend):
        chase_backend._get_connection().close()
        assert_owner_only_permissions(Path(chase_backend.db_path), 0o600)

    def test_rejects_symlinked_database_path(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "symlink_profile"
        profile.mkdir()
        real_db = profile / "real_chase_credit_transactions.db"
        symlink_db = profile / "chase_credit_transactions.db"
        symlink_db.symlink_to(real_db)

        backend = CsvFinanceBackend(
            profile_dir=profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )
        with pytest.raises(OSError):
            _validate_path_security(backend.db_path)

    def test_rejects_dangling_symlinked_database_path(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "dangling_symlink_profile"
        profile.mkdir(mode=0o700)
        dangling_database = profile / "chase_credit_transactions.db"
        dangling_database.symlink_to(profile / "missing_database.db")

        backend = CsvFinanceBackend(
            profile_dir=profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )

        with pytest.raises(OSError, match="symlink"):
            _validate_path_security(backend.db_path)

    def test_rejects_profile_path_with_symlinked_intermediate_component(
        self, tmp_path, tmp_config_dir
    ):
        real_parent = tmp_path / "real_parent"
        profile = real_parent / "profile"
        profile.mkdir(parents=True)
        symlinked_parent = tmp_path / "symlinked_parent"
        symlinked_parent.symlink_to(real_parent, target_is_directory=True)

        backend = CsvFinanceBackend(
            profile_dir=symlinked_parent / "profile",
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )

        with pytest.raises(OSError, match="symlink"):
            backend._get_connection()

    @pytest.mark.skipif(os.name != "posix", reason="POSIX mode-bit check")
    def test_rejects_group_writable_parent_directory(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "insecure_profile"
        profile.mkdir()
        profile.chmod(0o777)

        backend = CsvFinanceBackend(
            profile_dir=profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )
        with pytest.raises(OSError):
            _validate_path_security(backend.db_path)

    def test_allows_windows_path_with_posix_mode_bits(self, tmp_path, monkeypatch):
        profile = tmp_path / "windows_profile"
        profile.mkdir(mode=0o755)
        database_path = profile / "transactions.db"

        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        _validate_path_security(str(database_path))

    def test_validate_path_components_skips_posix_mode_check_on_windows(
        self, tmp_path, monkeypatch
    ):
        """On Windows, the path-components walker must not raise on group/other
        writable mode bits — those checks are POSIX-only."""
        # Make the parent group-writable (would fail the POSIX check).
        profile = tmp_path / "windows_profile"
        profile.mkdir(mode=0o755)
        profile.chmod(0o775)

        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        # Should not raise.
        _validate_path_components(profile)

    def test_secure_open_db_creates_missing_file_on_windows(self, tmp_path, monkeypatch):
        """First-time init on Windows must not call os.lstat before the file exists."""
        profile = tmp_path / "windows_profile"
        profile.mkdir(parents=True, mode=0o700)
        db_path = profile / "transactions.db"
        assert not db_path.exists()

        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        # Should not raise FileNotFoundError; file should be created.
        _secure_open_db(str(db_path))
        assert db_path.exists()

    def test_secure_open_db_uses_file_utils_helpers_on_windows(self, tmp_path, monkeypatch):
        """Windows branch must use the project file_utils helpers (which know
        about owner-only DACLs) rather than Path.touch + os.chmod."""
        profile = tmp_path / "windows_profile"
        profile.mkdir(parents=True, mode=0o700)
        db_path = profile / "transactions.db"
        assert not db_path.exists()

        # Force the function into the Windows branch.
        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        ensure_called = {"ensure_restrictive_directory": 0, "open_restrictive_file": 0}

        def fake_ensure(path, *, parents=False):
            ensure_called["ensure_restrictive_directory"] += 1
            Path(path).mkdir(parents=parents, exist_ok=True)

        def fake_open(path, **kwargs):
            ensure_called["open_restrictive_file"] += 1
            p = Path(path)
            p.parent.mkdir(parents=True, exist_ok=True)
            p.touch()
            return os.open(str(p), os.O_RDWR)

        monkeypatch.setattr(csv_backend, "ensure_restrictive_directory", fake_ensure)
        monkeypatch.setattr(csv_backend, "open_restrictive_file", fake_open)

        _secure_open_db(str(db_path))

        # The owner-only helpers were used for both the directory and the
        # file, not the touch/chmod fallback.
        assert ensure_called["ensure_restrictive_directory"] >= 1
        assert ensure_called["open_restrictive_file"] >= 1
        assert db_path.exists()

    def test_secure_open_db_tightens_existing_file_on_windows(self, tmp_path, monkeypatch):
        """An existing database must also go through the owner-only helper,
        which re-applies a restrictive DACL — a permissive DACL left on an
        existing file would otherwise be silently retained."""
        profile = tmp_path / "windows_profile"
        profile.mkdir(parents=True, mode=0o700)
        db_path = profile / "transactions.db"
        db_path.touch(mode=0o600)

        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        calls: list[dict] = []
        real_open = csv_backend.open_restrictive_file

        def counting_open(path, **kwargs):
            calls.append(kwargs)
            return real_open(path, **kwargs)

        monkeypatch.setattr(csv_backend, "open_restrictive_file", counting_open)

        _secure_open_db(str(db_path))

        assert calls == [{"read_write": True}]

    def test_get_connection_retightens_permissions_on_windows(
        self, tmp_profile_dir, tmp_config_dir, monkeypatch
    ):
        """On Windows every connection must re-apply the owner-only DACL to
        the database and its parent, not only the first initialization."""
        monkeypatch.setattr(csv_backend, "_requires_posix_mode_checks", lambda: False)

        calls: list[str] = []
        real_open = csv_backend.open_restrictive_file

        def counting_open(path, **kwargs):
            calls.append(str(path))
            return real_open(path, **kwargs)

        monkeypatch.setattr(csv_backend, "open_restrictive_file", counting_open)

        backend = CsvFinanceBackend(
            profile_dir=tmp_profile_dir,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )
        backend._get_connection().close()
        first_count = len(calls)
        backend._get_connection().close()
        assert len(calls) > first_count

    def test_get_connection_rejects_path_redirected_during_connect(
        self, chase_backend, monkeypatch
    ):
        """If the database path is redirected to a different file while the
        SQLite connection is being established, the connection must be
        rejected rather than silently operating on the replacement.

        On POSIX the post-connect device/inode comparison detects the swap.
        On Windows the held verification descriptor blocks the deletion
        itself (sharing violation), so the swap cannot even occur."""
        chase_backend._get_connection().close()
        db_path = Path(chase_backend.db_path)
        real_connect = csv_backend.sqlite3.connect

        def redirecting_connect(path, *args, **kwargs):
            conn = real_connect(path, *args, **kwargs)
            # Swap the database for a different file (new inode) mid-connect.
            try:
                db_path.unlink()
                db_path.touch(mode=0o600)
            except OSError:
                conn.close()
                raise
            return conn

        monkeypatch.setattr(csv_backend.sqlite3, "connect", redirecting_connect)

        expected_error = "redirected" if os.name == "posix" else ""
        with pytest.raises(OSError, match=expected_error):
            chase_backend._get_connection()

    def test_configured_categories_merged_with_transaction_categories(
        self, chase_backend, tmp_profile_dir
    ):
        """Profile-configured categories must appear in the picker even when
        no transaction uses them, and transaction-derived categories pick up
        their configured group name."""
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c1", "2026-07-12", -5.0, "STORE", "Groceries", "cat_Groceries"),
        )
        conn.commit()
        conn.close()

        config = {
            "fetched_categories": {
                "Essentials": ["Groceries", "Utilities"],
                "Fun": ["Travel"],
            }
        }
        (tmp_profile_dir / "config.yaml").write_text(yaml.safe_dump(config))

        result = asyncio.run(chase_backend.get_transaction_categories())
        by_name = {cat["name"]: cat for cat in result["categories"]}

        assert set(by_name) == {"Groceries", "Utilities", "Travel"}
        # Transaction-derived category keeps its stored id and gains a group
        assert by_name["Groceries"]["id"] == "cat_Groceries"
        assert by_name["Groceries"]["group"]["name"] == "Essentials"
        # Configured-but-unused categories are exposed with stable ids
        assert by_name["Utilities"]["id"] == "cat_utilities"
        assert by_name["Utilities"]["group"]["name"] == "Essentials"
        assert by_name["Travel"]["group"]["name"] == "Fun"

    def test_transaction_categories_without_profile_config(self, chase_backend):
        """Without a profile config, transaction-derived categories are
        returned unchanged (placeholder group, as before)."""
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, category, category_id) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            ("c1", "2026-07-12", -5.0, "STORE", "Groceries", "cat_Groceries"),
        )
        conn.commit()
        conn.close()

        result = asyncio.run(chase_backend.get_transaction_categories())
        assert result["categories"] == [
            {"id": "cat_Groceries", "name": "Groceries", "group": {"id": "", "type": "expense"}}
        ]


@pytest.mark.skipif(sys.platform != "darwin", reason="macOS extended ACLs")
class TestMacosExtendedAcls:
    """POSIX mode bits are not the whole access-control story on macOS: an
    extended ACL can grant another local account access to a 0600 file."""

    def _add_acl(self, path: Path) -> None:
        subprocess.run(
            ["chmod", "+a", "everyone allow read,write", str(path)],
            check=True,
        )

    def test_inherited_acl_is_stripped_from_new_database(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "acl_profile"
        profile.mkdir(mode=0o700)
        # An inheritable ACE on the parent propagates to files created in it.
        subprocess.run(
            ["chmod", "+a", "everyone allow read,write,file_inherit", str(profile)],
            check=True,
        )
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=tmp_config_dir, institution_name="chase_credit"
        )
        # The profile directory's own ACL is rejected outright.
        with pytest.raises(OSError, match="extended ACL"):
            backend._get_connection()

        subprocess.run(["chmod", "-N", str(profile)], check=True)
        conn = backend._get_connection()
        conn.close()
        assert not macos_acl.has_extended_acl(Path(backend.db_path))

    def test_acl_added_after_init_is_rejected_on_next_connection(self, chase_backend):
        chase_backend._get_connection().close()
        self._add_acl(Path(chase_backend.db_path))

        with pytest.raises(OSError, match="extended ACL"):
            chase_backend._get_connection()
