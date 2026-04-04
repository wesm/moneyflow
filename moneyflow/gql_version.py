def _parse_gql_version(version_str: str) -> tuple:
    """
    Parse a gql version string into a comparable tuple of (major, minor, patch).

    Examples:
        "3.5.0" -> (3, 5, 0)
        "4.0.0" -> (4, 0, 0)
        "4.2.0b0" -> (4, 2, 0)
        "3.4.1a1" -> (3, 4, 1)

    Args:
        version_str: Version string from gql.__version__

    Returns:
        Tuple of (major, minor, patch) as integers
    """
    # Remove build metadata (e.g., "+local")
    version_str = version_str.split("+")[0]

    # Replace pre-release markers with dots to split them out
    version_str = version_str.replace("a", ".").replace("b", ".").replace("rc", ".")

    # Extract numeric parts only
    version_parts = []
    for part in version_str.split("."):
        try:
            version_parts.append(int(part))
        except ValueError:
            break  # Stop at first non-numeric part

    # Return first 3 parts (major, minor, patch), pad with 0s if needed
    return tuple(version_parts[:3] + [0] * (3 - len(version_parts)))


def _detect_gql_v4_plus() -> bool:
    """
    Detect if the installed gql library is version 4.0+.

    In gql 3.x: execute_async(document=query, ...)
    In gql 4.0+: execute_async(request=query, ...)

    Returns:
        True if gql >= 4.0.0, False otherwise
    """
    try:
        import gql as gql_module

        version_tuple = _parse_gql_version(gql_module.__version__)
        return version_tuple >= (4, 0, 0)
    except (ImportError, AttributeError, ValueError):
        # Fallback: assume older version if we can't detect
        return False


# Detect gql version to handle API changes in v4.0+
GQL_V4_PLUS = _detect_gql_v4_plus()
