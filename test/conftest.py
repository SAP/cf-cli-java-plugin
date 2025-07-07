"""
Pytest configuration and hooks for CF Java Plugin testing.
"""

import os
import signal
import sys

import pytest

# Add the test directory to Python path for absolute imports
test_dir = os.path.dirname(os.path.abspath(__file__))
if test_dir not in sys.path:
    sys.path.insert(0, test_dir)

# noqa: E402
from framework.runner import CFJavaTestSession

# Global test session instance
test_session = None

# Track HTML report configuration
html_report_enabled = False
html_report_path = None

# Track failures for handling interruptions
_test_failures = []
_interrupt_count = 0  # Track number of interrupts for graduated response
_active_test = None  # Track currently running test for better interrupt messages

# Track apps that need restart on failure (regardless of no_restart=True)
_apps_need_restart_on_failure = set()


def pytest_addoption(parser):
    """Add custom command line options."""
    parser.addoption(
        "--no-initial-restart",
        action="store_true",
        default=False,
        help="Skip restarting all apps at the start of the test suite",
    )


# Set up signal handlers to improve interrupt behavior
def handle_interrupt(signum, frame):
    """Custom signal handler for SIGINT to ensure failures are reported."""
    global _interrupt_count

    _interrupt_count += 1

    # Print a message about the interrupt
    if _interrupt_count == 1:
        print("\n🛑 Test execution interrupted by user (Ctrl+C)")
        if _active_test:
            print(f"   Currently running test: {_active_test}")

        # Let Python's default handler take over after our custom handling
        # This will raise KeyboardInterrupt in the main thread
        signal.default_int_handler(signum, frame)
    else:
        # Second Ctrl+C - force immediate exit
        print("\n🛑 Second interrupt detected - forcing immediate termination")

        # Attempt to clean up resources
        try:
            if "test_session" in globals() and test_session:
                print("   Attempting cleanup of test resources...")
                try:
                    test_session.teardown_session()
                    print("   ✅ Test session cleaned up successfully")
                except Exception as e:
                    print(f"   ⚠️ Failed to clean up test session: {e}")
        except Exception:
            print("   ⚠️ Error during cleanup - continuing to force exit")
            pass

        # Display helpful message before exit
        print("\n💡 To debug what was happening:")
        print("   1. Run the specific test with verbose output: ./test.py run <test_name> -v")
        print("   2. Or use fail-fast mode: ./test.py --failed -x")

        # Force immediate exit - extreme case
        os.exit(130)  # 130 is the standard exit code for SIGINT


# Register our custom interrupt handler
signal.signal(signal.SIGINT, handle_interrupt)


def pytest_xdist_make_scheduler(config, log):
    """Configure pytest-xdist scheduler to group tests by app name.

    This ensures that tests for the same app never run in parallel,
    preventing interference between test cases on the same application.
    """
    # Import here to avoid dependency issues when xdist is not available
    try:
        from xdist.scheduler import LoadScopeScheduling

        class AppGroupedScheduling(LoadScopeScheduling):
            """Custom scheduler that groups tests by app parameter."""

            def _split_scope(self, nodeid):
                """Split scope to group by app name from test parameters."""
                # Extract app name from test node ID
                # Format: test_file.py::TestClass::test_method[app_name]
                if "[" in nodeid and "]" in nodeid:
                    # Extract the parameter part (e.g., "sapmachine21")
                    param_part = nodeid.split("[")[-1].rstrip("]")
                    # Use the app name as the scope to group tests
                    return param_part
                # Fallback to default behavior for tests without parameters
                return super()._split_scope(nodeid)

        return AppGroupedScheduling(config, log)
    except ImportError:
        # If xdist is not available, return None to use default scheduling
        return None


def pytest_configure(config):
    """Configure pytest session."""
    global test_session, html_report_enabled, html_report_path
    test_session = CFJavaTestSession()

    # Set the global session in runner module to avoid duplicate sessions
    from framework.runner import set_global_test_session

    set_global_test_session(test_session)

    # Check if HTML reporting is enabled
    html_report_path = config.getoption("--html", default=None)
    html_report_enabled = html_report_path is not None

    if html_report_enabled:
        print(f"📊 Live HTML reporting enabled: {html_report_path}")

    # Check if parallel execution is requested
    if config.getoption("-n", default=None) or config.getoption("--numprocesses", default=None):
        print("🚀 Parallel execution configured with app-based grouping")
        print("   Tests for the same app will run on the same worker to prevent interference")


def pytest_runtest_protocol(item, nextitem):
    """Hook for the test execution protocol."""
    # Let pytest handle execution normally without extra verbose output
    return None


def pytest_sessionstart(session):
    """Start of test session."""
    if test_session and not getattr(test_session, "_initialized", False):
        try:
            test_session.setup_session()
        except Exception as e:
            print(f"Warning: Failed to setup test session: {e}")
            print("Tests will continue but may fail without proper CF setup.")

    # Handle initial app restart unless --no-initial-restart is specified
    if not session.config.getoption("--no-initial-restart"):
        _restart_all_apps_at_start()


def _restart_all_apps_at_start():
    """Restart all apps at the start of the test suite."""
    if test_session and test_session._cf_logged_in:
        try:
            print("🔄 INITIAL RESTART: Restarting all apps at test suite start...")
            # Use the same restart mode as configured
            restart_mode = os.environ.get("RESTART_APPS", "smart_parallel").lower()

            # Use a safer restart approach that won't hang
            if restart_mode in ["smart"]:
                # For smart mode, check if restart is actually needed
                success = test_session.cf_manager.restart_apps_if_needed()
            elif restart_mode == "smart_parallel":
                success = test_session.cf_manager.restart_apps_if_needed_parallel()
            elif restart_mode == "parallel":
                success = test_session.cf_manager.restart_apps_parallel()
            elif restart_mode == "always":
                success = test_session.cf_manager.restart_apps()
            elif restart_mode != "never":
                # Default to smart mode for safety
                success = test_session.cf_manager.restart_apps_if_needed()
            else:
                return
            if success:
                print("✅ INITIAL RESTART: All apps restarted successfully")
            else:
                print("⚠️ INITIAL RESTART: Some apps may not have restarted properly")

        except Exception as e:
            print(f"⚠️ INITIAL RESTART: Failed to restart apps at start: {e}")
            # Continue with tests even if restart fails
            pass


def pytest_sessionfinish(session, exitstatus):
    """End of test session."""
    if test_session:
        try:
            test_session.teardown_session()
        except Exception as e:
            print(f"Warning: Failed to teardown test session: {e}")


def pytest_runtest_setup(item):
    """Setup before each test."""
    global _active_test
    # Track the currently running test for better interrupt handling
    _active_test = item.nodeid


def pytest_collection_modifyitems(config, items):
    """Modify collected test items based on decorators and filters, and clean up display names."""
    filtered_items = []

    for item in items:
        test_func = item.function

        # Check if test should be skipped
        if hasattr(test_func, "_skip") and test_func._skip:
            reason = getattr(test_func, "_skip_reason", "Skipped by decorator")
            item.add_marker(pytest.mark.skip(reason=reason))
            continue

        # Clean up the node ID to remove decorator source location
        if hasattr(item, "nodeid") and "<- framework/decorators.py" in item.nodeid:
            item.nodeid = item.nodeid.replace(" <- framework/decorators.py", "")

        # Also clean up the item name if it has the decorator reference
        if hasattr(item, "name") and "<- framework/decorators.py" in item.name:
            item.name = item.name.replace(" <- framework/decorators.py", "")

        filtered_items.append(item)

    items[:] = filtered_items


def pytest_runtest_logreport(report):
    """Clean up test reports to remove decorator source locations and track failures."""
    global _active_test

    # Clean up node IDs
    if hasattr(report, "nodeid") and report.nodeid:
        if "<- framework/decorators.py" in report.nodeid:
            report.nodeid = report.nodeid.replace(" <- framework/decorators.py", "")

    # Track failures for interruption handling
    if report.when == "call" and report.failed:
        _test_failures.append(report.nodeid)

    # Track test completion to clear the active test reference
    if report.when == "teardown":
        if _active_test == report.nodeid:
            _active_test = None


def pytest_terminal_summary(terminalreporter, exitstatus, config):
    """Enhanced terminal summary with HTML report info and live reporting cleanup."""
    # Original functionality: customize terminal output to remove decorator references
    # Enhanced: Add HTML report information and handle KeyboardInterrupt

    # Special handling for keyboard interruption - ensure summary is shown
    # Display HTML report information if enabled
    if html_report_enabled and html_report_path:
        if os.path.exists(html_report_path):
            abs_path = os.path.abspath(html_report_path)
            print(f"\n📊 HTML Report: file://{abs_path}")
            print("   Open this file in your browser to view detailed results")
        else:
            print(f"\n⚠️  HTML report not found at: {html_report_path}")

    # Display failure summary advice
    if exitstatus != 0:
        print("\n💡 Tip: Use './test.py all --failed' to re-run only failed tests")
        print("   Or './test.py run <test_name>' to run a specific test")


def pytest_runtest_logstart(nodeid, location):
    """Hook called at the start of running each test."""
    # Clean up the nodeid for live display
    if "<- framework/decorators.py" in nodeid:
        # Unfortunately we can't modify nodeid here as it's read-only
        # This is a limitation of pytest's architecture
        pass


@pytest.fixture(scope="session")
def cf_session():
    """Pytest fixture to access the CF test session."""
    global test_session
    if test_session is None:
        test_session = CFJavaTestSession()
        test_session.setup_session()
    return test_session


@pytest.fixture(autouse=True)
def cleanup_tmp_after_test(request):
    """Cleanup all remote files and folders created during the test after each test, and on interruption."""
    _cleanup_remote_files_on_interrupt()


# Also clean up on interruption (SIGINT)
def _cleanup_remote_files_on_interrupt():
    if test_session:
        try:
            for app in test_session.get_apps_with_tracked_files():
                remote_paths = test_session.get_and_clear_created_remote_files(app)
                for remote_path in remote_paths:
                    os.system(f"cf ssh {app} -c 'rm -rf {remote_path}' > /dev/null 2>&1")
        except Exception:
            pass


_original_sigint_handler = signal.getsignal(signal.SIGINT)


def _sigint_handler(signum, frame):
    _cleanup_remote_files_on_interrupt()
    if callable(_original_sigint_handler):
        _original_sigint_handler(signum, frame)


signal.signal(signal.SIGINT, _sigint_handler)


@pytest.fixture(autouse=True)
def cleanup_remote_tmp_before_test(request):
    """Clean up /tmp on the remote app container before every test."""
    if test_session:
        try:
            # Get all apps involved in this test (parameterized or not)
            apps = []
            # Try to extract app parameter from test function arguments
            if hasattr(request, "param"):
                apps = [request.param]
            elif hasattr(request, "node") and hasattr(request.node, "callspec"):
                # For parameterized tests
                callspec = getattr(request.node, "callspec", None)
                if callspec and "app" in callspec.params:
                    apps = [callspec.params["app"]]
            # Fallback: get all tracked apps if none found
            if not apps:
                try:
                    apps = test_session.get_apps_with_tracked_files()
                except Exception:
                    apps = []
            # Clean /tmp for each app
            for app in apps:
                try:
                    # Use cf ssh to clean /tmp, ignore errors
                    os.system(f"cf ssh {app} -c 'rm -rf /tmp/*' > /dev/null 2>&1")
                except Exception:
                    pass
        except Exception:
            pass


def pytest_runtest_teardown(item, nextitem):
    """Teardown after each test - handle restart on failure."""
    # Check if this test failed and needs app restart
    if hasattr(item, "_test_failed") and item._test_failed:
        # Extract app name from test parameters
        app_name = _extract_app_name_from_test(item)
        if app_name and test_session and test_session._cf_logged_in:
            try:
                print(f"🔄 FAILURE RESTART: Test failed, restarting app {app_name}...")
                success = test_session.cf_manager.restart_single_app(app_name)
                if success:
                    print(f"✅ FAILURE RESTART: App {app_name} restarted successfully after test failure")
                else:
                    print(f"⚠️ FAILURE RESTART: Failed to restart app {app_name} after test failure")
            except Exception as e:
                print(f"⚠️ FAILURE RESTART: Error restarting app {app_name} after test failure: {e}")
                # Continue with test execution even if restart fails
                pass


def _extract_app_name_from_test(item):
    """Extract app name from test item parameters."""
    try:
        # Check for parameterized test with app parameter
        if hasattr(item, "callspec") and item.callspec:
            params = item.callspec.params
            if "app" in params:
                return params["app"]

        # Try to extract from node ID for parameterized tests
        # Format: test_file.py::TestClass::test_method[app_name]
        if "[" in item.nodeid and "]" in item.nodeid:
            param_part = item.nodeid.split("[")[-1].rstrip("]")
            # Simple heuristic: if it doesn't contain spaces or special chars, likely an app name
            if param_part and " " not in param_part and "," not in param_part:
                return param_part

        return None
    except Exception:
        return None


@pytest.hookimpl(tryfirst=True, hookwrapper=True)
def pytest_runtest_makereport(item, call):
    """Create test report and track failures for restart logic."""
    # Execute all other hooks to get the report
    outcome = yield
    rep = outcome.get_result()

    # Mark the item if test failed during the call phase
    if call.when == "call" and rep.failed:
        item._test_failed = True
