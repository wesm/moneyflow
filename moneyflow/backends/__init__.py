"""
Finance backend abstraction layer.

This module provides a pluggable backend system that allows moneyflow to work
with different finance platforms (Monarch Money, YNAB, etc.) through a
common interface.

Usage:
    from moneyflow.backends import get_backend, MonarchBackend, DemoBackend

    # Get a backend by name
    backend = get_backend('monarch')

    # Or instantiate directly
    backend = MonarchBackend()
    backend = DemoBackend()
"""

from typing import Dict, Type

from .amazon import AmazonBackend
from .base import FinanceBackend
from .demo import DemoBackend
from .monarch import MonarchBackend
from .ynab import YNABBackend

# Backend registry: maps backend names to their classes
_BACKEND_REGISTRY: Dict[str, Type[FinanceBackend]] = {
    "monarch": MonarchBackend,
    "demo": DemoBackend,
    "amazon": AmazonBackend,
    "ynab": YNABBackend,
}


def get_backend(name: str, **kwargs) -> FinanceBackend:
    """
    Get a backend instance by name.

    Args:
        name: Backend name (e.g., 'monarch', 'demo')
        **kwargs: Arguments to pass to the backend constructor

    Returns:
        An instance of the requested backend

    Raises:
        ValueError: If the backend name is not registered

    Example:
        backend = get_backend('monarch')
        await backend.login(email='user@example.com', password='***')
    """
    backend_class = _BACKEND_REGISTRY.get(name.lower())
    if backend_class is None:
        available = ", ".join(_BACKEND_REGISTRY.keys())
        raise ValueError(f"Unknown backend '{name}'. Available backends: {available}")
    return backend_class(**kwargs)


def register_backend(name: str, backend_class: Type[FinanceBackend]) -> None:
    """
    Register a custom backend.

    This allows third-party backends to be registered at runtime.

    Args:
        name: Backend name (should be lowercase)
        backend_class: Backend class that implements FinanceBackend

    Example:
        class MyCustomBackend(FinanceBackend):
            # ... implementation ...

        register_backend('mycustom', MyCustomBackend)
        backend = get_backend('mycustom')
    """
    if not issubclass(backend_class, FinanceBackend):
        raise TypeError(f"{backend_class.__name__} must inherit from FinanceBackend")
    _BACKEND_REGISTRY[name.lower()] = backend_class


def list_backends() -> list[str]:
    """
    List all registered backend names.

    Returns:
        List of backend names

    Example:
        >>> list_backends()
        ['monarch', 'demo']
    """
    return list(_BACKEND_REGISTRY.keys())


# Export public API
__all__ = [
    "FinanceBackend",
    "MonarchBackend",
    "DemoBackend",
    "AmazonBackend",
    "YNABBackend",
    "get_backend",
    "register_backend",
    "list_backends",
]
