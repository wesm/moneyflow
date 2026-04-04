"""
Tests for gql library version detection.

Covers:
- Version string parsing with various formats
- Detection of gql v4+ vs v3.x
- Edge cases (pre-releases, build metadata)
- Validation with actual installed gql library versions

Note: These tests run against the installed gql version. Use GitHub Actions
matrix to test against multiple gql versions (3.4.0, 3.5.0, 4.0.0, 4.2.0, etc.)
"""

import inspect
import sys
from unittest.mock import patch

import pytest

from moneyflow.monarchmoney import GQL_V4_PLUS, _detect_gql_v4_plus, _parse_gql_version


class TestParseGqlVersion:
    """Test gql version string parsing."""

    @pytest.mark.parametrize(
        "version_str, expected",
        [
            ("3.5.0", (3, 5, 0)),
            ("4.0.0", (4, 0, 0)),
            ("4.2.0", (4, 2, 0)),
            ("3.4.1", (3, 4, 1)),
            ("4.2.0b0", (4, 2, 0)),
            ("3.5.0b1", (3, 5, 0)),
            ("4.0.0b2", (4, 0, 0)),
            ("4.0.0a1", (4, 0, 0)),
            ("3.6.0a0", (3, 6, 0)),
            ("4.0.0rc1", (4, 0, 0)),
            ("3.5.0rc2", (3, 5, 0)),
            ("3.5.0+local", (3, 5, 0)),
            ("4.0.0+build123", (4, 0, 0)),
            ("4.2.0b0+git.abc123", (4, 2, 0)),
            ("3", (3, 0, 0)),
            ("4.0", (4, 0, 0)),
            ("3.5.0.0", (3, 5, 0)),
            ("4.0.0.1.2", (4, 0, 0)),
        ],
    )
    def test_parse_gql_version(self, version_str, expected):
        """Test parsing various semantic version strings."""
        assert _parse_gql_version(version_str) == expected


class TestVersionComparison:
    """Test version comparison for detecting gql v4+."""

    def test_v3_versions_are_less_than_v4(self):
        """Test that all v3 versions are correctly identified as < v4."""
        v3_versions = ["3.0.0", "3.4.0", "3.4.1", "3.5.0", "3.9.9"]
        for version_str in v3_versions:
            version_tuple = _parse_gql_version(version_str)
            assert version_tuple < (4, 0, 0), f"{version_str} should be < 4.0.0"

    def test_v4_versions_are_greater_or_equal_to_v4(self):
        """Test that all v4 versions are correctly identified as >= v4."""
        v4_versions = ["4.0.0", "4.0.1", "4.1.0", "4.2.0", "4.2.0b0", "4.0.0a1"]
        for version_str in v4_versions:
            version_tuple = _parse_gql_version(version_str)
            assert version_tuple >= (4, 0, 0), f"{version_str} should be >= 4.0.0"

    def test_boundary_version_4_0_0(self):
        """Test the exact boundary version 4.0.0."""
        assert _parse_gql_version("4.0.0") == (4, 0, 0)
        assert _parse_gql_version("4.0.0") >= (4, 0, 0)

    def test_pre_release_4_0_0_counts_as_v4(self):
        """Test that pre-release versions of 4.0.0 are treated as v4."""
        # This is intentional: 4.0.0a1, 4.0.0b0, etc. parse to (4, 0, 0)
        # and should use the v4 API
        assert _parse_gql_version("4.0.0a1") >= (4, 0, 0)
        assert _parse_gql_version("4.0.0b0") >= (4, 0, 0)
        assert _parse_gql_version("4.0.0rc1") >= (4, 0, 0)


class TestEdgeCases:
    """Test edge cases and error handling."""

    def test_parse_empty_string(self):
        """Test parsing empty version string."""
        # Should return (0, 0, 0) since no numeric parts found
        assert _parse_gql_version("") == (0, 0, 0)

    def test_parse_invalid_version(self):
        """Test parsing completely invalid version strings."""
        assert _parse_gql_version("invalid") == (0, 0, 0)
        assert _parse_gql_version("abc.def.ghi") == (0, 0, 0)

    def test_parse_version_starting_with_text(self):
        """Test version strings that start with non-numeric text."""
        # Should stop at first non-numeric part
        assert _parse_gql_version("v3.5.0") == (0, 0, 0)  # 'v' is not numeric
        assert _parse_gql_version("version-4.0.0") == (0, 0, 0)

    def test_parse_version_with_unicode(self):
        """Test version strings with unicode characters."""
        # Should handle gracefully and return partial or zero tuple
        assert _parse_gql_version("3.5.0—special") == (3, 5, 0)


class TestActualGqlLibrary:
    """
    Test version detection with the actual installed gql library.

    These tests validate that our version detection correctly identifies the
    installed gql version and predicts the correct API to use.

    To test against multiple gql versions, run these tests in a GitHub Actions
    matrix with different gql versions installed.
    """

    def test_detect_gql_version_and_api(self):
        """
        Test that version detection correctly identifies the gql version
        and predicts the correct execute_async API signature.

        This is the main integration test that validates:
        1. gql library is installed and has __version__
        2. Version string can be parsed
        3. Detection correctly identifies v3 vs v4+
        4. Detection matches actual API signature
        """
        gql = pytest.importorskip("gql")
        from gql import Client

        # Get actual version
        actual_version = gql.__version__
        parsed_version = _parse_gql_version(actual_version)
        detected_v4_plus = _detect_gql_v4_plus()

        # Verify detection matches version number
        expected_v4_plus = parsed_version >= (4, 0, 0)
        assert detected_v4_plus == expected_v4_plus, (
            f"Version detection mismatch: gql {actual_version} parsed as {parsed_version}, expected GQL_V4_PLUS={expected_v4_plus} but got {detected_v4_plus}"
        )

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
