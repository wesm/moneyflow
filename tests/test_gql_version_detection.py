"""
Tests for gql library version detection.

Covers:
- Detection of gql v4+ vs v3.x
- Edge cases (pre-releases, build metadata)
- Validation with actual installed gql library versions
"""

import inspect
import sys
from unittest.mock import MagicMock, patch

import pytest

from moneyflow.backends.gql_version import GQL_V4_PLUS, _detect_gql_v4_plus


class TestVersionDetection:
    """Test gql version detection logic."""

    @pytest.mark.parametrize(
        "version_str, expected",
        [
            ("3.5.0", False),
            ("4.0.0", True),
            ("4.2.0", True),
            ("3.4.1", False),
            ("4.2.0b0", True),
            ("3.5.0b1", False),
            ("4.0.0b2", True),
            ("4.0.0a1", True),
            ("3.6.0a0", False),
            ("4.0.0rc1", True),
            ("3.5.0rc2", False),
            ("3.5.0+local", False),
            ("4.0.0+build123", True),
            ("4.2.0b0+git.abc123", True),
            ("3", False),
            ("4.0", True),
            ("3.5.0.0", False),
            ("4.0.0.1.2", True),
            ("", False),
            ("invalid", False),
            ("v3.5.0", False),
        ],
    )
    def test_detect_gql_v4_plus(self, version_str, expected):
        """Test detection with various version strings."""
        mock_gql = MagicMock()
        mock_gql.__version__ = version_str
        with patch.dict(sys.modules, {"gql": mock_gql}):
            assert _detect_gql_v4_plus() == expected


class TestActualGqlLibrary:
    """
    Test version detection with the actual installed gql library.

    These tests validate that our version detection correctly identifies the
    installed gql version and predicts the correct API to use.
    """

    def test_detect_gql_version_and_api(self):
        """
        Test that version detection correctly identifies the gql version
        and predicts the correct execute_async API signature.
        """
        gql = pytest.importorskip("gql")
        from gql import Client

        actual_version = gql.__version__
        detected_v4_plus = _detect_gql_v4_plus()

        # Verify detection matches actual API signature
        sig = inspect.signature(Client.execute_async)
        params = list(sig.parameters.keys())
        first_param = params[1] if params and params[0] == "self" else params[0]

        if detected_v4_plus:
            assert first_param == "request", (
                f"gql {actual_version} detected as v4+ but execute_async first param is '{first_param}' (expected 'request')"
            )
        else:
            assert first_param == "document", (
                f"gql {actual_version} detected as v3.x but execute_async first param is '{first_param}' (expected 'document')"
            )

        # Verify that Client and AIOHTTPTransport can be constructed
        from gql.transport.aiohttp import AIOHTTPTransport

        transport = AIOHTTPTransport(
            url="https://api.monarchmoney.com/graphql",
            headers={"authorization": "Token dummy"},
            timeout=10,
        )
        client = Client(
            transport=transport,
            fetch_schema_from_transport=False,
            execute_timeout=10,
        )
        assert client is not None

    def test_detect_gql_v4_plus_without_gql(self):
        """Test detection behavior when gql is not installed."""
        with patch.dict(sys.modules, {"gql": None}):
            assert _detect_gql_v4_plus() is False

    def test_global_constant_matches_detection(self):
        """Test that the global GQL_V4_PLUS constant matches runtime detection."""
        detected = _detect_gql_v4_plus()
        assert GQL_V4_PLUS == detected, (
            f"Global GQL_V4_PLUS ({GQL_V4_PLUS}) doesn't match _detect_gql_v4_plus() ({detected})"
        )
