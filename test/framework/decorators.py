"""
Test decorators and annotations for CF Java Plugin testing.
"""

import fnmatch
import json
import os
import sys
from pathlib import Path
from typing import List

import pytest


def test(*apps, no_restart=False):
    """Test decorator.

    Usage:
        @test()
        @test(no_restart=True)  # Skip app restart after test

    Args:
        *apps: App names to test on, defaults to "sapmachine21"
        no_restart: If True, skip app restart after test
    """

    # Determine which apps to test
    if "all" in apps:
        test_apps = get_available_apps()
    elif not apps:
        # If no apps specified, default to sapmachine21
        test_apps = ["sapmachine21"]
    else:
        # Use the provided apps directly
        test_apps = list(apps)

    print(f"🔍 TEST DECORATOR: Running tests for apps: {test_apps}  ")

    def decorator(test_func):
        # Create a wrapper that matches pytest's expected signature
        def wrapper(self, app):  # pytest provides these parameters
            # Check if test should be skipped due to previous success
            if should_skip_successful_test(test_func.__name__, app):
                pytest.skip(f"Skipping previously successful test: {test_func.__name__}[{app}]")

            # Check test selection patterns
            selection_patterns = get_test_selection_patterns()
            if selection_patterns and not match_test_patterns(test_func.__name__, selection_patterns):
                pytest.skip(f"Test {test_func.__name__} not in selection patterns: {selection_patterns}")

            # Environment filtering (TESTS variable)
            if test_filter := os.environ.get("TESTS", "").strip():
                patterns = [p.strip() for p in test_filter.split(",")]
                if not any(p in test_func.__name__ for p in patterns):
                    pytest.skip(f"Filtered by TESTS={test_filter}")

            # Execute test with DSL - import here to avoid circular imports
            from .dsl import test_cf_java

            # Track cleanup needs
            should_restart = True
            cleanup_files = []
            test_passed = False

            try:
                # Execute test with DSL
                with self.runner.create_test_context(app) as ctx:
                    # Track files before test execution
                    import glob

                    initial_files = set(glob.glob("*"))

                    # Create the DSL instance (this becomes the 't' parameter)
                    t = test_cf_java(self.runner, ctx, test_func.__name__)

                    # Call test function with standard parameters
                    test_func(self, t, app)

                    # Determine if restart is needed
                    # --restart flag forces restart, otherwise use decorator parameter
                    force_restart = should_force_restart()
                    should_restart = force_restart or not no_restart

                    # Find files created during test
                    final_files = set(glob.glob("*"))
                    cleanup_files = list(final_files - initial_files)

                    # Mark test as passed if we get here
                    test_passed = True

            except Exception as e:
                # Emit error details before restart
                print(f"❌ TEST FAILED: {test_func.__name__}[{app}] - {type(e).__name__}: {str(e)}")

                # Always restart on test failure
                should_restart = True
                raise
            finally:
                # Mark test as successful if it passed
                if test_passed:
                    mark_test_successful(test_func.__name__, app)

                # Clean up created files and folders
                if cleanup_files:
                    import shutil
                    import time

                    for item in cleanup_files:
                        try:
                            if os.path.isfile(item):
                                os.remove(item)
                                print(f"Cleaned up file: {item}")
                            elif os.path.isdir(item):
                                shutil.rmtree(item)
                                print(f"Cleaned up directory: {item}")
                        except Exception as cleanup_error:
                            print(f"Warning: Could not clean up {item}: {cleanup_error}")

                    # Wait one second after cleanup
                    time.sleep(1)

                # Handle app restart if needed
                if should_restart and hasattr(self, "runner"):
                    try:
                        print(f"🔄 TEST DECORATOR: Test requires restart for {app}")
                        # Check if we have a CF manager for restart operations
                        if hasattr(self, "session") and hasattr(self.session, "cf_manager"):
                            # Use the session's CF manager for restart
                            cf_manager = self.session.cf_manager
                            restart_mode = os.environ.get("RESTART_APPS", "smart_parallel").lower()

                            # First, process any deferred restarts from previous no_restart=True tests
                            # Only do this when we're actually restarting (should_restart=True)
                            if cf_manager.has_deferred_restarts():
                                print(f"🔄➡️ TEST DECORATOR: Processing deferred restarts before {app}")
                                if not cf_manager.process_deferred_restarts(restart_mode):
                                    print(f"⚠️ TEST DECORATOR: Deferred restart failed for {app}")

                            # Now perform the normal restart for just this specific app
                            if restart_mode == "smart_parallel" or restart_mode == "smart":
                                print(f"🧠 TEST DECORATOR: Using smart restart for {app} only")
                                if not cf_manager.restart_single_app_if_needed(app):
                                    print(f"⚠️ TEST DECORATOR: Smart restart failed for {app}")
                            elif restart_mode == "parallel" or restart_mode == "always":
                                print(f"🔄 TEST DECORATOR: Using direct restart for {app} only")
                                if not cf_manager.restart_single_app(app):
                                    print(f"⚠️ TEST DECORATOR: Direct restart failed for {app}")
                            else:
                                print(f"🧠 TEST DECORATOR: Using default smart restart for {app} only")
                                if not cf_manager.restart_single_app_if_needed(app):
                                    print(f"⚠️ TEST DECORATOR: Default restart failed for {app}")
                        else:
                            print(f"⚠️ TEST DECORATOR: No CF manager available for restart of {app}")
                    except Exception as restart_error:
                        print(f"❌ TEST DECORATOR: Could not restart app {app}: {restart_error}")
                else:
                    if not should_restart:
                        print(f"🚫 TEST DECORATOR: Skipping restart for {app} (no_restart=True)")
                        # Only add to deferred restart list if there are more tests coming
                        # If this is the last test in the session, don't bother deferring
                        if hasattr(self, "session") and hasattr(self.session, "cf_manager"):
                            # For now, always skip adding to deferred restart to prevent end-of-session restarts
                            print(
                                f"🚫 TEST DECORATOR: Not adding {app} to deferred restart list"
                                "to prevent unnecessary restarts"
                            )
                        else:
                            print("⚠️ TEST DECORATOR: Cannot track deferred restart - no CF manager available")
                    else:
                        print(f"⚠️ TEST DECORATOR: No runner available for restart of {app}")

        # Preserve ALL original function metadata before applying parametrize
        wrapper.__name__ = test_func.__name__
        wrapper.__doc__ = test_func.__doc__
        wrapper.__qualname__ = getattr(test_func, "__qualname__", test_func.__name__)
        wrapper.__module__ = test_func.__module__
        wrapper.__annotations__ = getattr(test_func, "__annotations__", {})

        # Apply parametrize decorator
        parametrized_wrapper = pytest.mark.parametrize("app", test_apps, ids=lambda app: f"{app}")(wrapper)

        # Preserve metadata on the final result as well
        parametrized_wrapper.__name__ = test_func.__name__
        parametrized_wrapper.__doc__ = test_func.__doc__
        parametrized_wrapper.__qualname__ = getattr(test_func, "__qualname__", test_func.__name__)
        parametrized_wrapper.__module__ = test_func.__module__
        parametrized_wrapper.__annotations__ = getattr(test_func, "__annotations__", {})

        return parametrized_wrapper

    return decorator


def get_available_apps() -> List[str]:
    """Get a list of available apps for testing, based on the apps folder"""
    return [app.name for app in Path("apps").iterdir() if app.is_dir() and not app.name.startswith(".")]


# Test tracking and selection utilities
SUCCESS_CACHE_FILE = ".test_success_cache.json"


def load_success_cache():
    """Load successful test cache from file."""
    try:
        if os.path.exists(SUCCESS_CACHE_FILE):
            with open(SUCCESS_CACHE_FILE, "r") as f:
                return json.load(f)
    except Exception:
        pass
    return {}


def save_success_cache(cache):
    """Save successful test cache to file."""
    try:
        with open(SUCCESS_CACHE_FILE, "w") as f:
            json.dump(cache, f, indent=2)
    except Exception:
        pass


def should_skip_successful_test(test_name, app):
    """Check if we should skip a test that was previously successful."""
    # Check for --skip-successful flag
    if "--skip-successful" not in sys.argv:
        return False

    cache = load_success_cache()
    test_key = f"{test_name}[{app}]"
    return test_key in cache


def mark_test_successful(test_name, app):
    """Mark a test as successful in the cache."""
    cache = load_success_cache()
    test_key = f"{test_name}[{app}]"
    cache[test_key] = True
    save_success_cache(cache)


def match_test_patterns(test_name, patterns):
    """Check if test name matches any of the glob patterns."""
    if not patterns:
        return True

    for pattern in patterns:
        if fnmatch.fnmatch(test_name, pattern):
            return True
    return False


def get_test_selection_patterns():
    """Get test selection patterns from command line."""
    # Look for --select-tests argument
    args = sys.argv
    try:
        idx = args.index("--select-tests")
        if idx + 1 < len(args):
            patterns_str = args[idx + 1]
            return [p.strip() for p in patterns_str.split(",")]
    except ValueError:
        pass
    return []


def should_force_restart():
    """Check if --restart flag is present to force restart after every test."""
    return "--restart" in sys.argv
