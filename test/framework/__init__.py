"""
Framework for CF Java Plugin testing.

This package provides a comprehensive testing framework for the CF Java Plugin,
including test runners, assertions, DSL, and utilities.
"""

# Core testing infrastructure
from .core import CFConfig, CFJavaTestRunner, CFManager, FluentAssertions

# Test decorators and markers
from .decorators import test

# Fluent DSL for test writing
from .dsl import CFJavaTest, test_cf_java

# Main test runner and base classes
from .runner import CFJavaTestSession, TestBase, create_test_class, get_test_session, test_with_apps

__all__ = [
    # Core components
    "CFJavaTestRunner",
    "CFManager",
    "FluentAssertions",
    "CFConfig",
    # Decorators
    "test",
    # DSL
    "CFJavaTest",
    "test_cf_java",
    # Runner
    "CFJavaTestSession",
    "TestBase",
    "test_with_apps",
    "create_test_class",
    "get_test_session",
]
