"""
Type stubs for enhanced pytest completion in CF Java Plugin tests.
"""

from typing import Any, Callable, Dict, List, Optional, TypeVar, Union

# Type variables
F = TypeVar("F", bound=Callable[..., Any])

class FixtureRequest:
    """Pytest fixture request object."""

    def __init__(self) -> None: ...

class Config:
    """Pytest configuration object."""

    def __init__(self) -> None: ...

class Session:
    """Pytest session object."""

    def __init__(self) -> None: ...

class Item:
    """Pytest test item."""

    def __init__(self) -> None: ...
    def add_marker(self, marker: Any) -> None: ...

class MarkDecorator:
    """Pytest mark decorator."""

    def __call__(self, func: F) -> F: ...

class Mark:
    """Pytest mark namespace."""

    def skip(self, reason: str = "") -> MarkDecorator: ...
    def parametrize(self, argnames: str, argvalues: List[Any]) -> MarkDecorator: ...

# Global pytest objects
mark: Mark

# Pytest hooks
def pytest_configure(config: Config) -> None: ...
def pytest_sessionstart(session: Session) -> None: ...
def pytest_sessionfinish(session: Session, exitstatus: int) -> None: ...
def pytest_runtest_setup(item: Item) -> None: ...
def pytest_collection_modifyitems(config: Config, items: List[Item]) -> None: ...

# Pytest fixture
def fixture(
    scope: str = "function",
    params: Optional[List[Any]] = None,
    autouse: bool = False,
    ids: Optional[List[str]] = None,
    name: Optional[str] = None,
) -> Callable[[F], F]: ...
