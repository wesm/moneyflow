"""
Tests for gql library version detection.

Covers:
- Version string parsing with various formats
- Detection of gql v4+ vs v3.x
- Edge cases (pre-releases, build metadata)
"""

import pytest

from moneyflow.monarchmoney import _parse_gql_version


class TestParseGqlVersion:
    """Test gql version string parsing."""

    def test_parse_standard_version(self):
        """Test parsing standard semantic version strings."""
        assert _parse_gql_version("3.5.0") == (3, 5, 0)
        assert _parse_gql_version("4.0.0") == (4, 0, 0)
        assert _parse_gql_version("4.2.0") == (4, 2, 0)
        assert _parse_gql_version("3.4.1") == (3, 4, 1)

    def test_parse_beta_version(self):
        """Test parsing beta versions (e.g., 4.2.0b0)."""
        assert _parse_gql_version("4.2.0b0") == (4, 2, 0)
        assert _parse_gql_version("3.5.0b1") == (3, 5, 0)
        assert _parse_gql_version("4.0.0b2") == (4, 0, 0)

    def test_parse_alpha_version(self):
        """Test parsing alpha versions (e.g., 4.0.0a1)."""
        assert _parse_gql_version("4.0.0a1") == (4, 0, 0)
        assert _parse_gql_version("3.6.0a0") == (3, 6, 0)

    def test_parse_rc_version(self):
        """Test parsing release candidate versions (e.g., 4.0.0rc1)."""
        assert _parse_gql_version("4.0.0rc1") == (4, 0, 0)
        assert _parse_gql_version("3.5.0rc2") == (3, 5, 0)

    def test_parse_version_with_build_metadata(self):
        """Test parsing versions with build metadata (e.g., 3.5.0+local)."""
        assert _parse_gql_version("3.5.0+local") == (3, 5, 0)
        assert _parse_gql_version("4.0.0+build123") == (4, 0, 0)
        assert _parse_gql_version("4.2.0b0+git.abc123") == (4, 2, 0)

    def test_parse_short_version(self):
        """Test parsing versions with missing minor/patch components."""
        assert _parse_gql_version("3") == (3, 0, 0)
        assert _parse_gql_version("4.0") == (4, 0, 0)

    def test_parse_version_with_extra_parts(self):
        """Test that extra parts beyond major.minor.patch are ignored."""
        assert _parse_gql_version("3.5.0.0") == (3, 5, 0)
        assert _parse_gql_version("4.0.0.1.2") == (4, 0, 0)


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
