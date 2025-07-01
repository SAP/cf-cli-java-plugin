"""
Test runner implementation using pytest for CF Java Plugin testing.

Environment Variables for controlling test behavior:
- DEPLOY_APPS: Controls app deployment behavior
  - "always": Always deploy apps, even if they exist
  - "never": Skip app deployment entirely
  - "if_needed": Only deploy if apps don't exist (default)

- RESTART_APPS: Controls app restart behavior between tests
  - "always": Always restart apps between tests
  - "never": Never restart apps between tests
  - "smart": Only restart if needed based on app status
  - "parallel": Use parallel restart for faster execution
  - "smart_parallel": Use smart parallel restart (default, recommended for speed)

- DELETE_APPS: Controls app cleanup after tests
  - "true": Delete apps after test session
  - "false": Keep apps deployed for faster subsequent runs (default)

- CF_COMMAND_STATS: Controls CF command performance tracking
  - "true": Enable detailed timing and statistics for all CF commands
  - "false": Disable command timing (default)
"""

import os
from typing import Any, Dict, List

import pytest

from .core import CFConfig, CFJavaTestRunner, CFManager, FluentAssertions


class CFJavaTestSession:
    """Manages the test session lifecycle."""

    def __init__(self):
        self.config = CFConfig()
        self.cf_manager = CFManager(self.config)
        self.runner = CFJavaTestRunner(self.config)
        self.assertions = FluentAssertions()
        self._initialized = False
        self._cf_logged_in = False

    def setup_session(self):
        """Setup the test session."""
        if self._initialized:
            return

        # Only print setup message once per session instance
        if not hasattr(self, "_setup_printed"):
            print("Setting up CF Java Plugin test session...")
            self._setup_printed = True

        # Check if we have required CF configuration
        if not self.config.username or not self.config.password:
            print("Warning: No CF credentials configured. Skipping CF operations.")
            print("Set CF_USERNAME and CF_PASSWORD environment variables or update test_config.yml")
            self._initialized = True
            return

        # Login to CF
        try:
            if self.cf_manager.login():
                self._cf_logged_in = True
            else:
                print("Warning: Failed to login to CF. Skipping app operations.")
                self._initialized = True
                return
        except Exception as e:
            print(f"Warning: CF login failed with error: {e}. Skipping app operations.")
            self._initialized = True
            return

        # Only proceed with app operations if logged in
        if self._cf_logged_in:
            # Check if apps should be deployed
            deploy_behavior = os.environ.get("DEPLOY_APPS", "if_needed").lower()

            if deploy_behavior == "always":
                print("Deploying applications...")
                try:
                    if not self.cf_manager.deploy_apps():
                        print("Warning: Failed to deploy test applications. Some tests may fail.")
                except Exception as e:
                    print(f"Warning: App deployment failed with error: {e}")
            elif deploy_behavior != "never":  # "if_needed" or default
                try:
                    if not self.cf_manager.deploy_apps_if_needed():
                        print("Warning: Failed to deploy some test applications. Some tests may fail.")
                except Exception as e:
                    print(f"Warning: App deployment check failed with error: {e}")

            # Start applications if they're not running
            try:
                if not self.cf_manager.start_apps_if_needed():
                    print("Warning: Failed to start some test applications. Some tests may fail.")
            except Exception as e:
                print(f"Warning: App startup failed with error: {e}")

        self._initialized = True
        print("Test session setup complete.")

    def teardown_session(self):
        """Teardown the test session."""
        # Always skip deferred restarts at session teardown to prevent unwanted restarts at the end
        if hasattr(self, "cf_manager") and self.cf_manager and self.cf_manager.has_deferred_restarts():
            print("�📋 SESSION TEARDOWN: Skipping deferred restarts (never restart at end of test suite)")
            # Clear the deferred restart list without processing
            self.cf_manager.clear_deferred_restart_apps()

        # Print CF command statistics before cleanup (always try if stats are enabled)
        # Check if CF_COMMAND_STATS is enabled and we have any CF commands tracked globally
        stats_enabled = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"
        if stats_enabled:
            # Import and create GlobalCFCommandStats instance - now with file persistence
            try:
                from .core import GlobalCFCommandStats

                global_stats = GlobalCFCommandStats()

                if global_stats.has_stats():
                    print("\n🔍 CF_COMMAND_STATS is enabled, printing global command statistics...")
                    global_stats.print_summary()
                else:
                    print("\n🔍 CF_COMMAND_STATS is enabled, but no CF commands were tracked.")

                # Clean up temporary stats file
                GlobalCFCommandStats.cleanup_temp_files()

            except Exception as e:
                print(f"Warning: Failed to print CF command statistics: {e}")
        elif getattr(self, "_cf_logged_in", False) and hasattr(self, "cf_manager") and self.cf_manager:
            # Fallback: print stats if we were logged in (original behavior for backward compatibility)
            try:
                self.runner.print_cf_command_summary()
            except Exception as e:
                print(f"Warning: Failed to print CF command statistics: {e}")

        # Always clean up temporary state files (login and restart tracking)
        try:
            from .core import CFManager

            CFManager.cleanup_state_files()
        except Exception as e:
            print(f"Warning: Failed to clean up state files: {e}")

        # Clean up temporary directories
        self.runner.cleanup()

        # Delete applications only if explicitly requested
        delete_apps = os.environ.get("DELETE_APPS", "false").lower() == "true"

        if getattr(self, "_cf_logged_in", False) and delete_apps:
            print("Deleting deployed applications...")
            self.cf_manager.delete_apps()

    def setup_test(self):
        """Setup before each test."""
        # Skip app operations if not logged in
        if not getattr(self, "_cf_logged_in", False):
            print("Skipping app restart - not logged in to CF")
            return

        # Check if we should restart apps between tests
        restart_behavior = os.environ.get("RESTART_APPS", "smart_parallel").lower()

        if restart_behavior == "never":
            return

        # Skip session-level restarts entirely - let test decorator handle all restart logic
        # This prevents double restarts and respects no_restart=True test settings
        print("🔄⏭️ SESSION: Skipping session-level restart - test decorator will handle restart logic")
        return

    def run_test_for_apps(self, test_func, apps: List[str]):
        """Run a test function for specified apps."""
        results = {}

        for app_name in apps:
            print(f"Running {test_func.__name__} for {app_name}")

            with self.runner.create_test_context(app_name) as context:
                try:
                    # Call the test function with app context
                    test_func(self, app_name, context)
                    results[app_name] = "PASSED"
                except Exception as e:
                    results[app_name] = f"FAILED: {str(e)}"
                    raise  # Re-raise for pytest to handle

        return results


# Global session instance for sharing across modules
_global_test_session = None


def set_global_test_session(session: "CFJavaTestSession"):
    """Set the global test session."""
    global _global_test_session
    _global_test_session = session


def get_test_session() -> CFJavaTestSession:
    """Get the current test session, creating one if needed."""
    # Return global session if available
    if _global_test_session is not None:
        return _global_test_session

    # Fallback: create a new session (but cache it to avoid multiple instances)
    if not hasattr(get_test_session, "_cached_session"):
        get_test_session._cached_session = CFJavaTestSession()

        # Try to initialize if not in pytest context
        import sys

        if not hasattr(sys, "_called_from_test"):
            try:
                get_test_session._cached_session.setup_session()
            except Exception as e:
                print(f"Warning: Could not initialize test session: {e}")

    return get_test_session._cached_session


class TestBase:
    """Base class for CF Java Plugin tests with helpful methods."""

    @property
    def session(self) -> CFJavaTestSession:
        """Get the current test session."""
        return get_test_session()

    @property
    def runner(self) -> CFJavaTestRunner:
        """Get the test runner."""
        return self.session.runner

    @property
    def assert_that(self) -> FluentAssertions:
        """Get assertion helpers."""
        return self.session.assertions

    def run_cf_java(self, command: str, app_name: str, **kwargs) -> Any:
        """Run a cf java command with app name substitution."""
        full_command = f"cf java {command}"
        return self.runner.run_command(full_command, app_name=app_name, **kwargs)

    def run_commands(self, commands: List[str], app_name: str, **kwargs) -> Any:
        """Run a sequence of commands."""
        return self.runner.run_command(commands, app_name=app_name, **kwargs)


def test_with_apps(app_names: List[str]):
    """Parametrize test to run with specified apps."""
    return pytest.mark.parametrize("app_name", app_names)


def create_test_class(test_methods: Dict[str, Any]) -> type:
    """Dynamically create a test class with specified methods."""

    class DynamicTestClass(TestBase):
        pass

    # Add test methods to the class
    for method_name, method_func in test_methods.items():
        setattr(DynamicTestClass, method_name, method_func)

    return DynamicTestClass
