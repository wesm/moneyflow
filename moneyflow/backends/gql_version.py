import re


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

        match = re.match(r"^(\d+)", gql_module.__version__)
        return int(match.group(1)) >= 4 if match else False
    except (ImportError, AttributeError, ValueError, TypeError):
        # Fallback: assume older version if we can't detect
        return False


# Detect gql version to handle API changes in v4.0+
GQL_V4_PLUS = _detect_gql_v4_plus()
