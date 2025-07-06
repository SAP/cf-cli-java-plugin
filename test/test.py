#!/usr/bin/env python3
"""
CF Java Plugin Test Suite - Python CLI.

A modern test runner for the CF Java Plugin test suite.
"""

import atexit
import os
import re
import signal
import subprocess
import sys
from pathlib import Path
from typing import Dict, List

# Ensure we're using the virtual environment Python
SCRIPT_DIR = Path(__file__).parent.absolute()
VENV_PYTHON = SCRIPT_DIR / "venv" / "bin" / "python"

# If we're not running in the venv, re-exec with venv python
if not sys.executable.startswith(str(SCRIPT_DIR / "venv")):
    if VENV_PYTHON.exists():
        os.execv(str(VENV_PYTHON), [str(VENV_PYTHON)] + sys.argv)
    else:
        print("❌ Virtual environment not found. Run: ./test.py setup")
        sys.exit(1)

import click
import colorama
from colorama import Fore, Style

# Initialize colorama for cross-platform colored output
colorama.init(autoreset=True)

# Script directory and paths
PYTEST = SCRIPT_DIR / "venv" / "bin" / "pytest"

# Color shortcuts
GREEN = Fore.GREEN + Style.BRIGHT
YELLOW = Fore.YELLOW + Style.BRIGHT
RED = Fore.RED + Style.BRIGHT
BLUE = Fore.BLUE + Style.BRIGHT
MAGENTA = Fore.MAGENTA + Style.BRIGHT
CYAN = Fore.CYAN + Style.BRIGHT

# Global state for tracking active tests and process cleanup
_active_command = None
_child_processes = set()  # Track child processes for proper cleanup
_test_failures = set()  # Track test failures for better interrupt reporting
_interrupt_count = 0  # Track multiple interrupts to handle force termination
_last_exit_code = 0  # Track last exit code for better reporting


def cleanup_on_exit():
    """Clean up any orphaned processes on exit."""
    for proc in _child_processes:
        try:
            if proc.poll() is None:  # Process is still running
                proc.terminate()
        except Exception:
            pass  # Ignore errors in cleanup


# Register cleanup function
atexit.register(cleanup_on_exit)


def handle_keyboard_interrupt(signum, frame):
    """Handle keyboard interrupt (Ctrl+C) gracefully."""
    global _interrupt_count
    _interrupt_count += 1
    if _interrupt_count == 1:
        click.echo(f"\n{YELLOW}⚠️  Interrupting test execution (Ctrl+C)...")
        click.echo(f"{YELLOW}Press Ctrl+C again to force immediate termination.")

        # Show all previous test failures without headers
        failed_tests_found = False
        all_failures = set()

        # Collect failures from pytest cache
        try:
            cache_file = SCRIPT_DIR / ".pytest_cache" / "v" / "cache" / "lastfailed"
            if cache_file.exists():
                import json

                with open(cache_file, "r") as f:
                    cached_failures = json.load(f)
                    all_failures.update(cached_failures.keys())
                    failed_tests_found = True
        except Exception as e:
            click.echo(f"\n{YELLOW}⚠️  Could not read test failure cache: {e}")

        # Add any failures tracked during this session
        all_failures.update(_test_failures)

        if all_failures:
            click.echo()  # Empty line for spacing
            # Show up to 20 most recent failures
            failure_list = sorted(list(all_failures))
            for test in failure_list[:20]:
                # Clean up test name for better readability
                clean_test = test.replace(".py::", " → ").replace("::", " → ")
                click.echo(f"{RED}  ✗ {clean_test}")

            if len(failure_list) > 20:
                remaining = len(failure_list) - 20
                click.echo(f"{YELLOW}  ... and {remaining} more failed tests")

            click.echo()  # Empty line for spacing
            click.echo(f"{BLUE}💡 Use './test.py --failed' to re-run only failed tests")
        elif failed_tests_found:
            click.echo(f"\n{GREEN}✅ No recent test failures found")
        else:
            click.echo(f"\n{BLUE}ℹ️  No test failure information available")

        # Try graceful termination of the active command
        if _active_command and _active_command.poll() is None:
            try:
                click.echo(f"{YELLOW}Attempting to terminate active command...")
                _active_command.terminate()
                import time

                time.sleep(0.5)
            except Exception:
                pass
    elif _interrupt_count == 2:
        click.echo(f"\n{RED}🛑 Forcing immediate termination...")
        cleanup_on_exit()
        click.echo(f"\n{YELLOW}📋 To debug failed tests, try:")
        click.echo(f"{BLUE}  1. ./test.py run <test_name> -v              # Run specific test with verbose output")
        click.echo(
            f"{BLUE}  2. ./test.py --failed -x                     # Run only failed tests, stop on first failure"
        )
        click.echo(f"{BLUE}  3. ./test.py --verbose --html                # Generate HTML report with details")
        os._exit(130)  # Force exit without running further cleanup handlers


# Register signal handler for SIGINT (Ctrl+C)
signal.signal(signal.SIGINT, handle_keyboard_interrupt)


def run_command(command: List[str], **kwargs) -> int:
    """Run a command and return the exit code."""

    try:
        # Add --showlocals to pytest to show variable values on failure
        if command[0].endswith("pytest") and "--showlocals" not in command:
            # Only add if not already present
            command.append("--showlocals")

        # Add -v for pytest if not already present to show more details
        if command[0].endswith("pytest") and "-v" not in command and "--verbose" not in command:
            command.append("-v")

        # Add --no-header and other options to improve pytest interrupt handling
        if command[0].endswith("pytest"):
            # Force showing summary on interruption
            if "--no-summary" not in command and "-v" in command:
                command.append("--force-short-summary")

            # Always capture keyboard interruption as a failure
            command.append("--capture=fd")

            # Make output unbuffered for real-time visibility
            os.environ["PYTHONUNBUFFERED"] = "1"

        # Ensure subprocess inherits terminal for proper output display
        if "stdout" not in kwargs:
            kwargs["stdout"] = None
        if "stderr" not in kwargs:
            kwargs["stderr"] = None

        # Prevent shell injection by using a list for the command
        cmd_str = " ".join(str(c) for c in command)
        click.echo(f"{BLUE}🔄 Running: {cmd_str}", err=True)

        # Run the command as a subprocess
        result = subprocess.Popen(command, **kwargs)
        _active_command = result
        _child_processes.add(result)

        # Wait for the command to complete
        return_code = result.wait()
        _child_processes.discard(result)
        _active_command = None

        # Extract test failures from pytest cache
        if command[0].endswith("pytest") and return_code != 0:
            try:
                cache_file = SCRIPT_DIR / ".pytest_cache" / "v" / "cache" / "lastfailed"
                if cache_file.exists():
                    import json

                    with open(cache_file, "r") as f:
                        failed_tests = json.load(f)
                        for test in failed_tests.keys():
                            _test_failures.add(test)
            except Exception:
                # Silently ignore errors in this diagnostic code
                pass

        # Show additional info based on exit code
        if return_code != 0:
            if return_code == 1:
                click.echo(f"\n{RED}❌ Tests failed")
                click.echo(f"{BLUE}💡 Use --failed to re-run only failed tests")
                click.echo(f"{BLUE}💡 Use --verbose for more detailed output")
                click.echo(f"{BLUE}💡 Use --start-with <test_selector> to resume from a specific test")
            elif return_code == 2:
                click.echo(f"\n{YELLOW}⚠️  Test execution interrupted or configuration error")
                click.echo(f"{BLUE}💡 Use --failed to re-run only failed tests")
                click.echo(f"{BLUE}💡 Use --start-with <test_selector> to resume from a specific test")
            elif return_code == 130:  # SIGINT (Ctrl+C)
                click.echo(f"\n{YELLOW}⚠️  Test execution interrupted by user (Ctrl+C)")
                click.echo(f"{BLUE}💡 Use --failed to re-run only failed tests")
                click.echo(f"{BLUE}💡 Use --start-with <test_selector> to resume from a specific test")

        return return_code
    except FileNotFoundError as e:
        click.echo(f"{RED}❌ Command not found: {e}", err=True)
        return 1
    except KeyboardInterrupt:
        # Let the global signal handler deal with this
        return 130
    except Exception as e:
        click.echo(f"{RED}❌ Unexpected error: {e}", err=True)
        import traceback

        traceback.print_exc()
        return 1
    finally:
        # Clean up the active command reference
        if _active_command in _child_processes:
            _child_processes.discard(_active_command)
        _active_command = None


def parse_test_file(file_path: Path) -> Dict:
    """Parse a test file to extract test classes, methods, and app dependencies."""
    try:
        with open(file_path, "r") as f:
            content = f.read()

        classes = {}
        current_class = None
        current_method = None

        for line_num, line in enumerate(content.split("\n"), 1):
            stripped_line = line.strip()

            # Match class definitions
            class_match = re.match(r"^class (Test\w+)", stripped_line)
            if class_match:
                class_name = class_match.group(1)
                current_class = class_name
                classes[class_name] = {"methods": [], "line": line_num, "docstring": None}
                continue

            # Extract class docstring
            if current_class and classes[current_class]["docstring"] is None:
                if stripped_line.startswith('"""') and len(stripped_line) > 3:
                    if stripped_line.endswith('"""') and len(stripped_line) > 6:
                        # Single line docstring
                        classes[current_class]["docstring"] = stripped_line[3:-3].strip()
                    else:
                        # Multi-line docstring start
                        classes[current_class]["docstring"] = stripped_line[3:].strip()

            # Match @test decorator and following method
            if current_class and stripped_line.startswith("@test("):
                # Extract app name from @test("app_name", ...)
                test_match = re.search(r'@test\(["\']([^"\']+)["\']', stripped_line)
                app_name = test_match.group(1) if test_match else "unknown"
                current_method = {"app": app_name, "options": []}

                # Extract additional options
                if "no_restart=True" in stripped_line:
                    current_method["options"].append("no_restart")

                continue

            # Match test method following @test decorator
            if current_class and current_method and re.match(r"^\s*def (test_\w+)", line):
                method_match = re.match(r"^\s*def (test_\w+)", line)
                if method_match:
                    method_name = method_match.group(1)
                    current_method["name"] = method_name
                    current_method["line"] = line_num

                    # Extract method docstring if available
                    current_method["docstring"] = None

                    classes[current_class]["methods"].append(current_method.copy())
                    current_method = None

        return {"file": file_path.name, "classes": classes}
    except Exception as e:
        return {"file": file_path.name, "classes": {}, "error": str(e)}


def get_test_hierarchy() -> Dict:
    """Get complete test hierarchy with app dependencies."""
    hierarchy = {}
    test_files = list(SCRIPT_DIR.glob("test_*.py"))

    for test_file in sorted(test_files):
        if test_file.name in ["test.py", "test_clean.py"]:  # Skip the runner itself
            continue

        parsed = parse_test_file(test_file)
        if parsed["classes"] or "error" in parsed:
            hierarchy[test_file.name] = parsed

    return hierarchy


@click.group(invoke_without_command=True)
@click.option("--no-initial-restart", is_flag=True, help="Skip initial app restart before running tests")
@click.option("--failed", is_flag=True, help="Run only previously failed tests")
@click.option("--html", is_flag=True, help="Generate HTML test report")
@click.option("--fail-fast", "-x", is_flag=True, help="Stop on first test failure")
@click.option("--verbose", "-v", is_flag=True, help="Verbose output with detailed information")
@click.option("--parallel", "-p", is_flag=True, help="Run tests in parallel using multiple CPU cores")
@click.option("--stats", is_flag=True, help="Enable CF command statistics tracking")
@click.option("--start-with", metavar="TEST_NAME", help="Start running tests with the specified test (inclusive)")
@click.pass_context
def cli(ctx, no_initial_restart, failed, html, fail_fast, verbose, parallel, stats, start_with):
    """CF Java Plugin Test Suite.

    Run different test suites with various options. Use --help on any command for details.
    """
    # Change to script directory
    os.chdir(SCRIPT_DIR)

    # Store options in context for subcommands
    ctx.ensure_object(dict)
    ctx.obj["pytest_args"] = []

    # Set environment variable if --no-initial-restart was specified
    if no_initial_restart:
        os.environ["RESTART_APPS"] = "never"
        click.echo(f"{YELLOW}🚫 Skipping initial app restart")

    # Enable CF command statistics if requested
    if stats:
        os.environ["CF_COMMAND_STATS"] = "true"
        click.echo(f"{CYAN}📊 CF command statistics enabled")

    # Build pytest arguments
    if failed:
        ctx.obj["pytest_args"].extend(["--lf"])
        click.echo(f"{YELLOW}🔄 Running only previously failed tests")

    if start_with:
        # Use pytest --collect-only -q to get all test nodeids in order
        import subprocess

        def get_all_pytest_nodeids():
            try:
                result = subprocess.run(
                    [str(PYTEST), "--collect-only", "-q", "--disable-warnings"],
                    capture_output=True,
                    text=True,
                    cwd=SCRIPT_DIR,
                )
                if result.returncode != 0:
                    click.echo(f"{RED}Failed to collect test nodeids via pytest.\n{result.stderr}")
                    return []
                # Only keep lines that look like pytest nodeids
                nodeids = [
                    line.strip()
                    for line in result.stdout.splitlines()
                    if (
                        "::" in line
                        and not line.strip().startswith("-")
                        and not line.strip().startswith("=")
                        and not line.strip().startswith("|")
                        and not line.strip().startswith("#")
                        and not line.strip().startswith("Status")
                        and not line.strip().startswith("Duration")
                        and not line.strip().startswith("Timestamp")
                        and not line.strip().startswith("Command")
                        and not line.strip().startswith("<")
                        and len(line.strip()) > 0
                    )
                ]
                return nodeids
            except Exception as e:
                click.echo(f"{RED}Error collecting test nodeids: {e}")
                return []

        all_nodeids = get_all_pytest_nodeids()
        idx = None
        for i, nodeid in enumerate(all_nodeids):
            if start_with in nodeid or nodeid.endswith(start_with):
                idx = i
                break
        if idx is not None:
            after_nodeids = all_nodeids[idx:]
            if after_nodeids:
                # If too many, warn user
                if len(after_nodeids) > 100:
                    click.echo(
                        f"{YELLOW}⚠️  More than 100 tests from selector, passing as positional args may hit OS limits."
                        "Consider a more specific selector."
                    )
                ctx.obj["pytest_args"].extend(after_nodeids)
                click.echo(f"{YELLOW}⏭️  Skipping {idx} tests, starting with: {all_nodeids[idx]}")
            else:
                click.echo(f"{RED}No tests found with selector '{start_with}'. Nothing to run.")
                sys.exit(0)
        else:
            click.echo(f"{RED}Could not find test matching selector '{start_with}'. Running all tests.")

    if html:
        ctx.obj["pytest_args"].extend(["--html=test_report.html", "--self-contained-html"])
        click.echo(f"{BLUE}📊 HTML report will be generated: test_report.html")

    if fail_fast:
        ctx.obj["pytest_args"].extend(["-x", "--tb=short"])
        click.echo(f"{RED}⚡ Fail-fast mode: stopping on first failure")

    if parallel:
        ctx.obj["pytest_args"].extend(["-n", "auto", "--dist", "worksteal"])
        click.echo(f"{MAGENTA}🚀 Parallel execution enabled")

    # Always add these flags for better developer experience
    if verbose:
        ctx.obj["pytest_args"].extend(["--tb=short", "-v", "--showlocals", "-ra"])
    else:
        ctx.obj["pytest_args"].extend(["--tb=short", "-v"])

    # If no subcommand provided, show help
    if ctx.invoked_subcommand is None:
        click.echo(ctx.get_help())


@cli.command("list")
@click.option("--apps-only", is_flag=True, help="Show only unique app names")
@click.option("--verbose", "-v", is_flag=True, help="Show method docstrings and line numbers")
@click.option("--short", is_flag=True, help="Show only method names without class prefix")
def list_tests(apps_only, verbose, short):
    """List all tests with their app dependencies in a hierarchical view.

    By default, test methods are prefixed with their class names (e.g., TestClass::test_method)
    making them ready to copy and paste for use with 'test.py run'. Use --short to disable
    this behavior and show only method names.
    """
    hierarchy = get_test_hierarchy()

    if apps_only:
        # Collect all unique app names
        apps = set()
        for file_data in hierarchy.values():
            for class_data in file_data["classes"].values():
                for method in class_data["methods"]:
                    apps.add(method["app"])

        click.echo(f"{GREEN}📱 Application Names Used in Tests:")
        for app in sorted(apps):
            click.echo(f"  • {app}")
        return

    click.echo(f"{GREEN}🧪 Test Suite Hierarchy:")
    click.echo(f"{BLUE}{'=' * 60}")

    for file_name, file_data in hierarchy.items():
        if "error" in file_data:
            click.echo(f"{RED}❌ {file_name}: {file_data['error']}")
            continue

        if not file_data["classes"]:
            continue

        click.echo(f"\n{CYAN}📁 {file_name}")

        for class_name, class_data in file_data["classes"].items():
            class_doc = class_data.get("docstring", "")
            if class_doc:
                click.echo(f"  {MAGENTA}📋 {class_name} - {class_doc}")
            else:
                click.echo(f"  {MAGENTA}📋 {class_name}")

            if verbose:
                click.echo(f"    {BLUE}(line {class_data['line']})")

            # Group methods by app
            methods_by_app = {}
            for method in class_data["methods"]:
                app = method["app"]
                if app not in methods_by_app:
                    methods_by_app[app] = []
                methods_by_app[app].append(method)

            for app, methods in sorted(methods_by_app.items()):
                app_color = YELLOW if app == "all" else GREEN if app == "sapmachine21" else CYAN
                click.echo(f"    {app_color}🎯 App: {app}")

                for method in methods:
                    options_str = ""
                    if method["options"]:
                        options_str = f" ({', '.join(method['options'])})"

                    # Format method name with or without class prefix
                    if short:
                        method_display = method["name"]
                    else:
                        method_display = f"{class_name}::{method['name']}"

                    if verbose:
                        click.echo(f"      • {method_display}{options_str} (line {method['line']})")
                    else:
                        click.echo(f"      • {method_display}{options_str}")


@cli.command()
@click.pass_context
def basic(ctx):
    """Run basic command tests."""
    click.echo(f"{GREEN}Running basic command tests...")
    return run_command([str(PYTEST), "test_basic_commands.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def jfr(ctx):
    """Run JFR tests."""
    click.echo(f"{GREEN}Running JFR tests...")
    return run_command([str(PYTEST), "test_jfr.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def asprof(ctx):
    """Run async-profiler tests (SapMachine only)."""
    click.echo(f"{GREEN}Running async-profiler tests...")
    return run_command([str(PYTEST), "test_asprof.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def integration(ctx):
    """Run integration tests."""
    click.echo(f"{GREEN}Running integration tests...")
    return run_command([str(PYTEST), "test_cf_java_plugin.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def disk_full(ctx):
    """Run disk full tests."""
    click.echo(f"{GREEN}Running disk full tests...")
    return run_command([str(PYTEST), "test_disk_full.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def jre21(ctx):
    """Run JRE21-specific tests."""
    click.echo(f"{GREEN}Running JRE21 tests...")
    return run_command([str(PYTEST), "test_jre21.py"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def all(ctx):
    """Run all tests."""
    click.echo(f"{GREEN}Running all tests...")
    return run_command([str(PYTEST)] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def heap(ctx):
    """Run all heap-related tests."""
    click.echo(f"{GREEN}Running heap-related tests...")
    return run_command([str(PYTEST), "-k", "heap"] + ctx.obj["pytest_args"])


@cli.command()
@click.pass_context
def profiling(ctx):
    """Run all profiling tests (JFR + async-profiler)."""
    click.echo(f"{GREEN}Running profiling tests...")
    return run_command([str(PYTEST), "-k", "jfr or asprof"] + ctx.obj["pytest_args"])


@cli.command()
@click.argument("selector")
@click.pass_context
def run(ctx, selector):
    """Run specific test by selector.

    SELECTOR can be:
    - TestClass::test_method (auto-finds file)
    - test_file.py::TestClass
    - test_file.py::TestClass::test_method
    - test_file.py
    - test_method_name (searches all files)

    Examples:
        test.py run test_cpu_profiling
        test.py run TestAsprofBasic::test_cpu_profiling
        test.py run test_asprof.py::TestAsprofBasic
    """
    pytest_args = ctx.obj["pytest_args"].copy()

    # Handle different selector formats
    if "::" in selector and not selector.endswith(".py"):
        # Class::method format - need to find the file
        parts = selector.split("::")
        class_name = parts[0]

        # Find the file containing this class
        hierarchy = get_test_hierarchy()
        found_file = None

        for file_name, file_data in hierarchy.items():
            if class_name in file_data.get("classes", {}):
                found_file = file_name
                break

        if found_file:
            click.echo(f"{BLUE}📁 Found test in file: {found_file}")
            full_selector = f"{found_file}::{selector}"
            pytest_args.append(full_selector)
        else:
            # Fall back to using -k for the selector
            click.echo(f"{YELLOW}⚠️ Could not find file for {selector}, using pattern matching")
            click.echo(f"{BLUE}💡 For better test selection, use the full path: test_file.py::{selector}")
            pytest_args.extend(["-k", selector.replace("::", " and ")])
    elif "::" in selector:
        # File::Class::method or File::Class format
        pytest_args.append(selector)
    elif selector.endswith(".py"):
        # File selection
        pytest_args.append(selector)
    else:
        # Search for method name across all files
        click.echo(f"{BLUE}📝 Searching for tests matching '{selector}' across all files")
        pytest_args.extend(["-k", selector])

    click.echo(f"{GREEN}Running specific test: {selector}")
    return run_command([str(PYTEST)] + pytest_args)


@cli.command()
def setup():
    """Set up the test environment (virtual environment, dependencies)."""
    import subprocess
    import sys

    click.echo(f"{GREEN}🔧 Setting up virtual environment...")

    venv_dir = SCRIPT_DIR / "venv"

    if not venv_dir.exists():
        click.echo("   Creating virtual environment...")
        subprocess.run([sys.executable, "-m", "venv", str(venv_dir)], check=True)

    click.echo("   Installing/updating dependencies...")
    pip_cmd = venv_dir / "bin" / "pip"
    subprocess.run([str(pip_cmd), "install", "--upgrade", "pip"], check=True)
    subprocess.run([str(pip_cmd), "install", "-r", str(SCRIPT_DIR / "requirements.txt")], check=True)

    click.echo(f"{GREEN}✅ Virtual environment setup complete!")
    click.echo("   To run tests: ./test.py all")
    return 0


@cli.command()
def clean():
    """Clean test artifacts and temporary files."""
    import shutil

    click.echo(f"{GREEN}🧹 Cleaning test artifacts...")

    # Remove pytest cache
    for cache_dir in [".pytest_cache", "__pycache__", "framework/__pycache__"]:
        cache_path = SCRIPT_DIR / cache_dir
        if cache_path.exists():
            shutil.rmtree(cache_path)

    # Remove test reports and cache files
    for pattern in ["test_report.html", ".test_success_cache.json"]:
        for file_path in SCRIPT_DIR.glob(pattern):
            file_path.unlink()

    # Remove downloaded files (heap dumps, JFR files, etc.)
    for pattern in ["*.hprof", "*.jfr"]:
        for file_path in SCRIPT_DIR.glob(pattern):
            file_path.unlink()

    click.echo(f"{GREEN}✅ Cleanup complete!")
    return 0


@cli.command()
@click.option("--force", "-f", is_flag=True, help="Force shutdown without confirmation")
def shutdown(force):
    """Shutdown all running test applications and scale them to zero instances."""
    import yaml

    click.echo(f"{YELLOW}🛑 Shutting down all test applications...")

    # Load test configuration to get app names
    config_file = SCRIPT_DIR / "test_config.yml"
    if not config_file.exists():
        click.echo(f"{RED}❌ Config file not found: {config_file}")
        return 1

    try:
        with open(config_file, "r") as f:
            config = yaml.safe_load(f)

        apps = config.get("apps", {})
        if not apps:
            click.echo(f"{YELLOW}⚠️  No apps found in configuration")
            return 0

        # Confirm shutdown unless --force is used
        if not force:
            app_names = list(apps.keys())
            click.echo(f"{CYAN}📋 Apps to shutdown: {', '.join(app_names)}")
            if not click.confirm(f"{YELLOW}Are you sure you want to shutdown all test apps?"):
                click.echo(f"{BLUE}ℹ️  Shutdown cancelled")
                return 0

        success_count = 0
        total_count = len(apps)

        for app_name in apps.keys():
            try:
                click.echo(f"{BLUE}🛑 Stopping {app_name}...")

                # First try to stop the app
                result = subprocess.run(["cf", "stop", app_name], capture_output=True, text=True, timeout=30)

                if result.returncode == 0:
                    click.echo(f"{GREEN}✅ {app_name} stopped")
                    success_count += 1
                else:
                    # App might not exist or already stopped
                    if "not found" in result.stderr.lower() or "does not exist" in result.stderr.lower():
                        click.echo(f"{YELLOW}⚠️  {app_name} does not exist or already stopped")
                        success_count += 1
                    else:
                        click.echo(f"{RED}❌ Failed to stop {app_name}: {result.stderr.strip()}")

            except subprocess.TimeoutExpired:
                click.echo(f"{RED}❌ Timeout stopping {app_name}")
            except Exception as e:
                click.echo(f"{RED}❌ Error stopping {app_name}: {e}")

        if success_count == total_count:
            click.echo(f"{GREEN}✅ All {total_count} apps shutdown successfully")
            return 0
        else:
            click.echo(f"{YELLOW}⚠️  {success_count}/{total_count} apps shutdown successfully")
            return 1

    except Exception as e:
        click.echo(f"{RED}❌ Error during shutdown: {e}")
        return 1


if __name__ == "__main__":
    cli()
