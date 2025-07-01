"""
Core test framework for CF Java Plugin black box testing.
Provides a clean DSL for writing readable tests.
"""

import getpass
import glob
import os
import re
import shutil
import subprocess
import tempfile
import threading
import time
from datetime import datetime
from typing import Any, Dict, List, Union

import yaml


class GlobalCFCommandStats:
    """Global singleton for tracking CF command statistics across all test instances and processes."""

    _instance = None
    _lock = threading.Lock()
    _stats_file = None

    def __new__(cls):
        if cls._instance is None:
            with cls._lock:
                if cls._instance is None:
                    cls._instance = super().__new__(cls)
                    cls._instance._initialized = False
        return cls._instance

    def __init__(self):
        if not self._initialized:
            self.cf_command_stats = []
            self.stats_mode = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"
            # Use a fixed temp file name for this pytest run to ensure all instances use the same file
            if GlobalCFCommandStats._stats_file is None:
                import tempfile

                # Use a fixed name based on the pytest run to ensure all sessions share the same file
                temp_dir = tempfile.gettempdir()
                import getpass

                username = getpass.getuser()
                GlobalCFCommandStats._stats_file = os.path.join(temp_dir, f"cf_stats_pytest_{username}.json")
            self._stats_file = GlobalCFCommandStats._stats_file
            self._load_stats_from_file()
            self._initialized = True

    def _load_stats_from_file(self):
        """Load statistics from persistent file."""
        try:
            if self._stats_file and os.path.exists(self._stats_file):
                import json

                with open(self._stats_file, "r") as f:
                    data = json.load(f)
                    self.cf_command_stats = data.get("stats", [])
        except Exception:
            # If loading fails, start with empty stats
            self.cf_command_stats = []

    def _save_stats_to_file(self):
        """Save statistics to persistent file."""
        try:
            if self._stats_file:
                import json

                data = {"stats": self.cf_command_stats}
                with open(self._stats_file, "w") as f:
                    json.dump(data, f)
        except Exception:
            # If saving fails, continue silently
            pass

    def add_command_stat(self, command: str, duration: float, success: bool):
        """Add a CF command statistic to the global tracker."""
        # Always check current environment variable value (don't rely on cached self.stats_mode)
        stats_enabled = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"
        if not stats_enabled:
            return

        with self._lock:
            # Load latest stats from file (in case other processes added stats)
            self._load_stats_from_file()

            # Add new stat
            self.cf_command_stats.append(
                {"command": command, "duration": duration, "success": success, "timestamp": time.time()}
            )

            # Save updated stats to file
            self._save_stats_to_file()

    def get_stats(self) -> List[Dict]:
        """Get all CF command statistics."""
        # Always load from file to get latest stats
        self._load_stats_from_file()
        return self.cf_command_stats.copy()

    def clear_stats(self):
        """Clear all statistics (useful for testing)."""
        with self._lock:
            self.cf_command_stats.clear()
            self._save_stats_to_file()

    @classmethod
    def cleanup_temp_files(cls):
        """Clean up temporary stats files (call at end of test run)."""
        if cls._stats_file and os.path.exists(cls._stats_file):
            try:
                os.unlink(cls._stats_file)
            except Exception:
                pass
            cls._stats_file = None

    def has_stats(self) -> bool:
        """Check if any statistics have been recorded."""
        # Load from file to get latest count
        self._load_stats_from_file()
        return len(self.cf_command_stats) > 0

    def print_summary(self):
        """Print a summary of all CF command statistics."""
        # Always check current environment variable value
        stats_enabled = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"

        # Load latest stats from file
        self._load_stats_from_file()

        # Only print if stats mode is enabled AND we have commands to show
        if not stats_enabled or not self.cf_command_stats:
            return

        print("\n" + "=" * 80)
        print("CF COMMAND STATISTICS SUMMARY (GLOBAL)")
        print("=" * 80)

        total_commands = len(self.cf_command_stats)
        total_time = sum(stat["duration"] for stat in self.cf_command_stats)
        successful_commands = sum(1 for stat in self.cf_command_stats if stat["success"])
        failed_commands = total_commands - successful_commands

        print(f"Total CF commands executed: {total_commands}")
        print(f"Total execution time: {total_time:.2f}s")
        print(f"Successful commands: {successful_commands}")
        print(f"Failed commands: {failed_commands}")
        print(
            f"Average command time: {total_time / total_commands:.2f}s"
            if total_commands > 0
            else "Average command time: 0.00s"
        )

        # Show slowest commands
        if self.cf_command_stats:
            slowest = sorted(self.cf_command_stats, key=lambda x: x["duration"], reverse=True)[:5]
            print("\nSlowest commands:")
            for i, stat in enumerate(slowest, 1):
                status = "✓" if stat["success"] else "✗"
                print(f"  {i}. {status} {stat['command']} | {stat['duration']:.2f}s")

        # Print detailed table of all commands
        if self.cf_command_stats:
            print(f"\n{'DETAILED COMMAND TABLE':^80}")
            print("-" * 80)

            # Table headers
            header = f"{'#':<3} {'Status':<6} {'Duration':<10} {'Timestamp':<19} {'Command':<36}"
            print(header)
            print("-" * 80)

            # Sort by execution order (timestamp)
            sorted_stats = sorted(self.cf_command_stats, key=lambda x: x["timestamp"])

            for i, stat in enumerate(sorted_stats, 1):
                status = "✓" if stat["success"] else "✗"
                duration_str = f"{stat['duration']:.2f}s"

                # Format timestamp (convert from float to datetime)
                timestamp_dt = datetime.fromtimestamp(stat["timestamp"])
                timestamp_str = timestamp_dt.strftime("%H:%M:%S")

                # Truncate command if too long
                command = stat["command"]
                if len(command) > 36:
                    command = command[:33] + "..."

                row = f"{i:<3} {status:<6} {duration_str:<10} {timestamp_str:<19} {command:<36}"
                print(row)

        print("=" * 80)


class CFConfig:
    """Configuration for the test suite."""

    def __init__(self, config_file: str = "test_config.yml"):
        self.config_file = config_file
        self.config = self._load_config()

    def _load_config(self) -> Dict[str, Any]:
        """Load configuration from YAML file, with environment variable overrides."""
        try:
            with open(self.config_file, "r") as f:
                config = yaml.safe_load(f)
        except FileNotFoundError:
            config = self._default_config()

        # Ensure required sections exist
        if config is None:
            config = {}

        if "cf" not in config:
            config["cf"] = {}

        # Override with environment variables if they exist
        config["cf"]["api_endpoint"] = os.environ.get("CF_API", config["cf"].get("api_endpoint", ""))
        config["cf"]["username"] = os.environ.get("CF_USERNAME", config["cf"].get("username", ""))
        config["cf"]["password"] = os.environ.get("CF_PASSWORD", config["cf"].get("password", ""))
        config["cf"]["org"] = os.environ.get("CF_ORG", config["cf"].get("org", ""))
        config["cf"]["space"] = os.environ.get("CF_SPACE", config["cf"].get("space", ""))

        # Ensure apps section exists
        if "apps" not in config:
            config["apps"] = self._auto_detect_apps()

        # Ensure timeouts section exists
        if "timeouts" not in config:
            config["timeouts"] = {"app_start": 300, "command": 60}

        return config

    def _default_config(self) -> Dict[str, Any]:
        """Default configuration if file doesn't exist."""
        return {
            "cf": {
                "api_endpoint": os.environ.get("CF_API", "https://api.cf.eu12.hana.ondemand.com"),
                "username": os.environ.get("CF_USERNAME", ""),
                "password": os.environ.get("CF_PASSWORD", ""),
                "org": os.environ.get("CF_ORG", "sapmachine-testing"),
                "space": os.environ.get("CF_SPACE", "dev"),
            },
            "apps": self._auto_detect_apps(),
            "timeouts": {"app_start": 300, "command": 60},
        }

    def _auto_detect_apps(self) -> Dict[str, str]:
        """Auto-detect apps by scanning the testing apps folder."""
        apps = {}

        # Look for app directories in common locations
        possible_paths = [
            os.path.join(os.getcwd(), "apps"),  # From testing dir
            os.path.join(os.getcwd(), "..", "testing", "apps"),  # From framework dir
            os.path.join(os.path.dirname(__file__), "..", "apps"),  # Relative to this file
            os.path.join(os.path.dirname(__file__), "..", "..", "testing", "apps"),  # Up two levels
        ]

        for base_path in possible_paths:
            if os.path.exists(base_path) and os.path.isdir(base_path):
                for item in os.listdir(base_path):
                    app_dir = os.path.join(base_path, item)
                    if os.path.isdir(app_dir):
                        # Check if it looks like a CF app (has manifest.yml or similar)
                        app_files = [
                            "manifest.yml",
                            "manifest.yaml",
                            "Dockerfile",
                            "pom.xml",
                            "build.gradle",
                            "package.json",
                        ]
                        if any(os.path.exists(os.path.join(app_dir, f)) for f in app_files):
                            apps[item] = item
                if apps:  # Found apps, use this path
                    break
        return apps

    @property
    def username(self) -> str:
        return self.config["cf"]["username"]

    @property
    def password(self) -> str:
        return self.config["cf"]["password"]

    @property
    def api_endpoint(self) -> str:
        return self.config["cf"]["api_endpoint"]

    @property
    def org(self) -> str:
        return self.config["cf"]["org"]

    @property
    def space(self) -> str:
        return self.config["cf"]["space"]

    @property
    def apps(self) -> Dict[str, str]:
        return self.config["apps"]

    def get_detected_apps_info(self) -> str:
        """Get information about detected apps for debugging."""
        apps = self.apps
        if not apps:
            return "No apps detected"

        info = f"Detected {len(apps)} apps:\n"
        for app_key, app_name in apps.items():
            info += f"  - {app_key}: {app_name}\n"
        return info.rstrip()


class CommandResult:
    """Represents the result of a command execution."""

    def __init__(self, returncode: int, stdout: str, stderr: str, command: str):
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr
        self.command = command
        self.output = stdout + stderr  # Combined output

    @property
    def success(self) -> bool:
        return self.returncode == 0

    @property
    def failed(self) -> bool:
        return self.returncode != 0

    def __str__(self) -> str:
        return (
            f"CommandResult(cmd='{self.command}', rc={self.returncode}, "
            f"stdout_len={len(self.stdout)}, stderr_len={len(self.stderr)})"
        )


class TestContext:
    """Context for a single test execution."""

    def __init__(self, app_name: str, temp_dir: str):
        self.app_name = app_name
        self.temp_dir = temp_dir
        self.original_cwd = os.getcwd()
        self.files_before = set()
        self.files_after = set()

    def __enter__(self):
        os.chdir(self.temp_dir)
        self.files_before = set(os.listdir("."))
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.files_after = set(os.listdir("."))
        os.chdir(self.original_cwd)

    @property
    def new_files(self) -> set:
        """Files created during test execution."""
        return self.files_after - self.files_before

    @property
    def deleted_files(self) -> set:
        """Files deleted during test execution."""
        return self.files_before - self.files_after


class CFJavaTestRunner:
    """Main test runner with a clean DSL for CF Java Plugin testing."""

    def __init__(self, config: CFConfig):
        self.config = config
        self.temp_dirs = []
        # Use global stats tracker instead of local instance stats
        self.global_stats = GlobalCFCommandStats()
        self.stats_mode = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"

    def _is_cf_command(self, cmd: str) -> bool:
        """Check if a command is a CF CLI command."""
        cmd_stripped = cmd.strip()
        return cmd_stripped.startswith("cf ") or cmd_stripped.startswith("CF ")

    def _redact_sensitive_info(self, cmd: str) -> str:
        """Redact sensitive information from commands for logging."""
        # Redact login commands
        if "cf login" in cmd:
            # Replace username and password with placeholders
            import re

            # Pattern to match cf login with -u and -p flags
            pattern = r"cf login -u [^\s]+ -p \'[^\']+\'"
            if re.search(pattern, cmd):
                redacted = re.sub(r"(-u) [^\s]+", r"\1 [REDACTED]", cmd)
                redacted = re.sub(r"(-p) \'[^\']+\'", r"\1 [REDACTED]", redacted)
                return redacted
        return cmd

    def _log_cf_command_stats(self, cmd: str, duration: float, success: bool):
        """Log CF command statistics."""
        # Check if stats mode is enabled
        stats_enabled = os.environ.get("CF_COMMAND_STATS", "false").lower() == "true"

        if not self.stats_mode and not stats_enabled:
            return

        # Extract just the CF command part (remove cd and other shell operations)
        cf_part = cmd
        if "&&" in cmd:
            parts = cmd.split("&&")
            for part in parts:
                part = part.strip()
                if self._is_cf_command(part):
                    cf_part = part
                    break

        # Redact sensitive information for logging
        cf_part_redacted = self._redact_sensitive_info(cf_part)

        status = "✓" if success else "✗"
        print(f"[CF_STATS] {status} {cf_part_redacted} | {duration:.2f}s")

        # Only store in global stats if environment variable is enabled
        if stats_enabled:
            # Store in global stats tracker
            self.global_stats.add_command_stat(cf_part_redacted, duration, success)

    def print_cf_command_summary(self):
        """Print a summary of all CF command statistics (delegates to global stats)."""
        self.global_stats.print_summary()

    def run_command(self, cmd: Union[str, List[str]], timeout: int = 60, app_name: str = None) -> CommandResult:
        """Execute a command and return the result."""
        if isinstance(cmd, list):
            # Handle sequence of commands
            results = []
            for single_cmd in cmd:
                if single_cmd.startswith("sleep "):
                    sleep_time = float(single_cmd.split()[1])
                    time.sleep(sleep_time)
                    continue
                result = self._execute_single_command(single_cmd, timeout, app_name)
                results.append(result)
                if result.failed:
                    return result  # Return first failure
            return results[-1]  # Return last result if all succeeded
        else:
            return self._execute_single_command(cmd, timeout, app_name)

    def _execute_single_command(self, cmd: str, timeout: int, app_name: str = None) -> CommandResult:
        """Execute a single command."""
        if app_name:
            cmd = cmd.replace("$APP_NAME", app_name)

        # Track timing for CF commands
        is_cf_cmd = self._is_cf_command(cmd)
        start_time = time.time() if is_cf_cmd else 0

        try:
            process = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
            result = CommandResult(
                returncode=process.returncode, stdout=process.stdout, stderr=process.stderr, command=cmd
            )

            # Log CF command stats
            if is_cf_cmd and start_time > 0:
                duration = time.time() - start_time
                self._log_cf_command_stats(cmd, duration, result.success)

            return result

        except subprocess.TimeoutExpired:
            result = CommandResult(
                returncode=-1, stdout="", stderr=f"Command timed out after {timeout} seconds", command=cmd
            )

            # Log timeout for CF command
            if is_cf_cmd and start_time > 0:
                duration = time.time() - start_time
                self._log_cf_command_stats(cmd, duration, False)

            return result

        except KeyboardInterrupt:
            # Handle CTRL-C gracefully
            if is_cf_cmd:
                print(f"🛑 CF COMMAND CANCELLED: {cmd} (CTRL-C)")

            result = CommandResult(returncode=-1, stdout="", stderr="Command cancelled by user (CTRL-C)", command=cmd)

            # Log cancellation for CF command
            if is_cf_cmd and start_time > 0:
                duration = time.time() - start_time
                self._log_cf_command_stats(cmd, duration, False)

            # Re-raise to allow calling code to handle
            raise

    def create_test_context(self, app_name: str) -> TestContext:
        """Create a temporary directory context for test execution."""
        temp_dir = tempfile.mkdtemp(prefix=f"cf_java_test_{app_name}_")
        self.temp_dirs.append(temp_dir)
        return TestContext(app_name, temp_dir)

    def cleanup(self):
        """Clean up temporary directories."""
        # Clean up temporary directories
        for temp_dir in self.temp_dirs:
            if os.path.exists(temp_dir):
                shutil.rmtree(temp_dir)
        self.temp_dirs.clear()

    def check_file_exists(self, pattern: str) -> bool:
        """Check if a file matching the pattern exists."""
        matches = glob.glob(pattern)
        return len(matches) > 0

    def get_matching_files(self, pattern: str) -> List[str]:
        """Get all files matching the pattern."""
        return glob.glob(pattern)

    def check_remote_files(self, app_name: str, expected_files: List[str] = None) -> List[str]:
        """Check files in the remote app directory."""
        result = self.run_command(f"cf ssh {app_name} -c 'ls'", app_name=app_name)
        if result.failed:
            return []

        remote_files = [f.strip() for f in result.stdout.split("\n") if f.strip()]

        if expected_files is not None:
            unexpected = set(remote_files) - set(expected_files)
            missing = set(expected_files) - set(remote_files)
            if unexpected or missing:
                raise AssertionError(f"Remote files mismatch. Unexpected: {unexpected}, Missing: {missing}")

        return remote_files

    def check_jfr_events(self, file_pattern: str, event_type: str, min_count: int) -> bool:
        """Check JFR file contains minimum number of events."""
        files = self.get_matching_files(file_pattern)
        if not files:
            return False

        for file in files:
            result = self.run_command(f"jfr summary {file}")
            if result.success:
                # Parse output to find event count
                lines = result.stdout.split("\n")
                for line in lines:
                    if event_type in line:
                        # Extract count from line (assuming format like "ExecutionSample: 123")
                        match = re.search(r":\s*(\d+)", line)
                        if match and int(match.group(1)) >= min_count:
                            return True
        return False

    def capture_remote_file_state(self, app_name: str) -> Dict[str, List[str]]:
        """
        Capture the complete file state in key remote directories,
        ignoring JVM artifacts like /tmp/hsperfdata_vcap.
        """
        state = {}
        # Directories to monitor for file changes
        directories = {"tmp": "/tmp", "home": "$HOME", "app": "$HOME/app"}
        for name, directory in directories.items():
            # Use simple ls command to get directory contents
            cmd = f"cf ssh {app_name} -c 'ls -1 {directory} 2>/dev/null || echo NO_DIRECTORY'"
            result = self.run_command(cmd, app_name=app_name, timeout=15)
            if result.success:
                output = result.stdout.strip()
                if output == "NO_DIRECTORY" or not output:
                    state[name] = []
                else:
                    files = [f.strip() for f in output.split("\n") if f.strip()]
                    # Filter out JVM perfdata directory from /tmp
                    if name == "tmp":
                        files = [f for f in files if f != "hsperfdata_vcap"]
                    state[name] = files
            else:
                # If command fails, record empty state
                state[name] = []
        return state

    def compare_remote_file_states(
        self, before: Dict[str, List[str]], after: Dict[str, List[str]]
    ) -> Dict[str, List[str]]:
        """Compare two remote file states and return new files."""
        new_files = {}

        for directory in before.keys():
            before_set = set(before.get(directory, []))
            after_set = set(after.get(directory, []))

            # Find files that were added
            added_files = after_set - before_set
            if added_files:
                new_files[directory] = list(added_files)

        return new_files


class FluentAssertions:
    """Assertion helpers for test validation."""

    @staticmethod
    def output_contains(result: CommandResult, text: str):
        """Assert that command output contains specific text."""
        if text not in result.output:
            raise AssertionError(f"Expected output to contain '{text}', but got:\n{result.output}")

    @staticmethod
    def output_matches(result: CommandResult, pattern: str):
        """Assert that command output matches regex pattern."""
        if not re.search(pattern, result.output, re.MULTILINE | re.DOTALL):
            raise AssertionError(f"Expected output to match pattern '{pattern}', but got:\n{result.output}")

    @staticmethod
    def command_succeeds(result: CommandResult):
        """Assert that command succeeded."""
        if result.failed:
            raise AssertionError(
                f"Expected command to succeed, but it failed with code {result.returncode}:\n{result.stderr}"
            )

    @staticmethod
    def command_fails(result: CommandResult):
        """Assert that command failed."""
        if result.success:
            raise AssertionError(f"Expected command to fail, but it succeeded:\n{result.stdout}")

    @staticmethod
    def has_file(pattern: str):
        """Assert that a file matching pattern exists."""
        files = glob.glob(pattern)
        if not files:
            raise AssertionError(f"Expected file matching '{pattern}' to exist, but none found")

    @staticmethod
    def has_no_files(pattern: str = "*"):
        """Assert that no files matching pattern exist."""
        files = glob.glob(pattern)
        if files:
            raise AssertionError(f"Expected no files matching '{pattern}', but found: {files}")

    @staticmethod
    def line_count_at_least(result: CommandResult, min_lines: int):
        """Assert that output has at least specified number of lines."""
        lines = result.output.split("\n")
        actual_lines = len([line for line in lines if line.strip()])
        if actual_lines < min_lines:
            raise AssertionError(f"Expected at least {min_lines} lines, but got {actual_lines}")

    @staticmethod
    def jfr_has_events(file_pattern: str, event_type: str, min_count: int):
        """Assert that JFR file contains minimum number of events using JFRSummaryParser."""
        files = glob.glob(file_pattern)
        if not files:
            raise AssertionError(f"No JFR files found matching '{file_pattern}'")

        for file in files:
            try:
                parser = JFRSummaryParser(file)
                summary = parser.parse_summary()
                matching_events = [e for e in summary["events"] if event_type in e["name"] or e["name"] in event_type]
                for event in matching_events:
                    if event["count"] >= min_count:
                        return  # Success
            except Exception as ex:
                raise AssertionError(f"Failed to parse JFR summary for {file}: {ex}")

        # On error, show JFR summary with only events that have counts > 0
        error_msg = f"JFR file does not contain at least {min_count} {event_type} events"
        if files:
            file = files[0]
            try:
                parser = JFRSummaryParser(file)
                summary = parser.parse_summary()
                events_with_counts = [e for e in summary["events"] if e["count"] > 0]
                if events_with_counts:
                    try:
                        table = parser.format_events_table(min_count=0, highlight_pattern=event_type)
                        error_msg += f"\n\nJFR Summary for {file} (events with count > 0):\n{table}"
                    except Exception:
                        # Fallback to simple format
                        error_msg += f"\n\nJFR Summary for {file} (events with count > 0):\n"
                        for event in events_with_counts:
                            marker = "→" if (event_type in event["name"] or event["name"] in event_type) else " "
                            error_msg += f"  {marker} {event['name']}: {event['count']:,}\n"
                matching_events = [e for e in events_with_counts if event_type in e["name"] or e["name"] in event_type]
                if matching_events:
                    event_name, actual_count = matching_events[0]["name"], matching_events[0]["count"]
                    error_msg += (
                        f"\n\n💡 Note: '{event_name}' was found with {actual_count:,} events (needed {min_count:,})"
                    )
                else:
                    error_msg += f"\n\n💡 Note: No events matching '{event_type}' were found in the JFR file"
            except Exception as ex:
                error_msg += f"\n\nFailed to parse JFR summary for {file}: {ex}"
        raise AssertionError(error_msg)


class JFRSummaryParser:
    """Utility class for parsing JFR summary output."""

    def __init__(self, jfr_file_path: str):
        self.jfr_file_path = jfr_file_path
        self._summary_data = None

    def parse_summary(self) -> Dict[str, Any]:
        """Parse JFR summary and return structured data."""
        if self._summary_data is not None:
            return self._summary_data

        result = subprocess.run(["jfr", "summary", self.jfr_file_path], capture_output=True, text=True)
        if result.returncode != 0:
            raise ValueError(f"Failed to get JFR summary for {self.jfr_file_path}: {result.stderr}")

        lines = result.stdout.split("\n")

        # Parse metadata
        metadata = {}
        events = []

        in_event_table = False
        for line in lines:
            line_stripped = line.strip()

            # Parse metadata before the event table
            if not in_event_table:
                if line_stripped.startswith("Start:"):
                    metadata["start"] = line_stripped.split(":", 1)[1].strip()
                elif line_stripped.startswith("Version:"):
                    metadata["version"] = line_stripped.split(":", 1)[1].strip()
                elif line_stripped.startswith("VM Arguments:"):
                    metadata["vm_arguments"] = line_stripped.split(":", 1)[1].strip()
                elif line_stripped.startswith("Chunks:"):
                    try:
                        metadata["chunks"] = int(line_stripped.split(":", 1)[1].strip())
                    except ValueError:
                        pass
                elif line_stripped.startswith("Duration:"):
                    duration_str = line_stripped.split(":", 1)[1].strip()
                    metadata["duration"] = duration_str
                    # Parse duration to seconds if possible
                    try:
                        if "s" in duration_str:
                            duration_num = float(duration_str.replace("s", "").strip())
                            metadata["duration_seconds"] = duration_num
                    except ValueError:
                        pass

            # Look for the actual separator line (all equals signs)
            if line_stripped.startswith("=") and "=" * 10 in line_stripped and len(line_stripped) > 30:
                in_event_table = True
                continue

            # Skip empty lines and metadata before the table
            if not in_event_table or not line_stripped:
                continue

            # Skip metadata lines that can appear after the separator
            if line_stripped.startswith(("Start:", "Version:", "VM Arguments:", "Chunks:", "Duration:")):
                continue

            # Skip the header line ("Event Type    Count    Size (bytes)")
            if "Event Type" in line_stripped and "Count" in line_stripped:
                continue

            # Parse event lines (format: "jdk.EventName    count    size")
            parts = line_stripped.split()
            if len(parts) >= 2:
                try:
                    event_name = parts[0]
                    count = int(parts[1])
                    size_bytes = int(parts[2]) if len(parts) >= 3 else 0

                    # Only include events that look like JFR event names
                    if "." in event_name or event_name.startswith(("jdk", "jfr")):
                        events.append({"name": event_name, "count": count, "size_bytes": size_bytes})
                except (ValueError, IndexError):
                    # Skip lines that don't match the expected format
                    continue

        self._summary_data = {
            "metadata": metadata,
            "events": events,
            "total_events": sum(event["count"] for event in events),
            "total_size_bytes": sum(event["size_bytes"] for event in events),
        }

        return self._summary_data

    def get_events_with_count_gt(self, min_count: int) -> List[Dict[str, Any]]:
        """Get events with count greater than specified minimum."""
        summary = self.parse_summary()
        return [event for event in summary["events"] if event["count"] > min_count]

    def find_events_matching(self, pattern: str) -> List[Dict[str, Any]]:
        """Find events whose names contain the given pattern."""
        summary = self.parse_summary()
        return [event for event in summary["events"] if pattern in event["name"]]

    def get_total_event_count(self) -> int:
        """Get total number of events across all types."""
        summary = self.parse_summary()
        return summary["total_events"]

    def get_duration_seconds(self) -> float:
        """Get recording duration in seconds, or 0 if not available."""
        summary = self.parse_summary()
        return summary["metadata"].get("duration_seconds", 0.0)

    def has_minimum_events(self, min_total_events: int) -> bool:
        """Check if JFR has at least the specified number of total events."""
        return self.get_total_event_count() >= min_total_events

    def has_minimum_duration(self, min_duration_seconds: float) -> bool:
        """Check if JFR recording has at least the specified duration."""
        return self.get_duration_seconds() >= min_duration_seconds

    def format_events_table(self, min_count: int = 0, highlight_pattern: str = None) -> str:
        """Format events as a beautiful table, optionally filtering and highlighting."""
        events_to_show = self.get_events_with_count_gt(min_count)

        if not events_to_show:
            return "No events found with the specified criteria."

        # Sort by count descending
        events_to_show.sort(key=lambda x: x["count"], reverse=True)

        try:
            from tabulate import tabulate

            # Prepare table data with highlighting for pattern
            table_data = []
            for event in events_to_show:
                event_name = event["name"]
                count = event["count"]

                # Highlight if pattern matches
                if highlight_pattern and (highlight_pattern in event_name or event_name in highlight_pattern):
                    event_display = f"→ {event_name}"  # Mark with arrow
                else:
                    event_display = f"  {event_name}"

                # Format count with thousand separators
                count_display = f"{count:,}"
                table_data.append([event_display, count_display])

            # Create the table
            return tabulate(
                table_data, headers=["Event Type", "Count"], tablefmt="rounded_grid", stralign="left", numalign="right"
            )

        except ImportError:
            # Fallback to simple format if tabulate is not available
            result = "Event Type                           Count\n"
            result += "-" * 50 + "\n"
            for event in events_to_show:
                event_name = event["name"]
                count = event["count"]
                marker = (
                    "→"
                    if (highlight_pattern and (highlight_pattern in event_name or event_name in highlight_pattern))
                    else " "
                )
                result += f"{marker} {event_name:<30} {count:>10,}\n"

            return result


class CFManager:
    """Manages CF operations like login, app deployment, etc."""

    # File-based state tracking (replaces class-level state)
    _login_state_file = None
    _restart_state_file = None
    _lock = threading.Lock()

    @classmethod
    def _get_state_files(cls):
        """Get the paths for state files, creating them if needed."""
        if cls._login_state_file is None or cls._restart_state_file is None:
            temp_dir = tempfile.gettempdir()
            username = getpass.getuser()
            cls._login_state_file = os.path.join(temp_dir, f"cf_login_state_{username}.json")
            cls._restart_state_file = os.path.join(temp_dir, f"cf_restart_state_{username}.json")
        return cls._login_state_file, cls._restart_state_file

    @classmethod
    def _load_login_state(cls) -> Dict:
        """Load login state from persistent file."""
        login_file, _ = cls._get_state_files()
        try:
            if os.path.exists(login_file):
                import json

                with open(login_file, "r") as f:
                    return json.load(f)
        except Exception:
            pass
        # Return default state if loading fails
        return {"logged_in": False, "login_config": {}, "login_timestamp": 0.0}

    @classmethod
    def _save_login_state(cls, state: Dict):
        """Save login state to persistent file."""
        login_file, _ = cls._get_state_files()
        try:
            import json

            with open(login_file, "w") as f:
                json.dump(state, f)
        except Exception:
            pass

    @classmethod
    def _load_restart_state(cls) -> Dict:
        """Load deferred restart state from persistent file."""
        _, restart_file = cls._get_state_files()
        try:
            if os.path.exists(restart_file):
                import json

                with open(restart_file, "r") as f:
                    data = json.load(f)
                    # Handle legacy format (list of apps) and convert to new format
                    if isinstance(data, dict) and "restart_entries" in data:
                        return data
                    elif isinstance(data, dict) and "deferred_restart_apps" in data:
                        # Legacy format - convert to new format with class names
                        apps = data.get("deferred_restart_apps", [])
                        return {
                            "restart_entries": [
                                {"app": app, "test_class": "Unknown", "reason": "Legacy"} for app in apps
                            ]
                        }
                    else:
                        # Very old format - just a list
                        return {"restart_entries": []}
        except Exception:
            pass
        return {"restart_entries": []}

    @classmethod
    def _save_restart_state(cls, restart_entries: List[Dict]):
        """Save deferred restart state to persistent file."""
        _, restart_file = cls._get_state_files()
        try:
            import json

            data = {"restart_entries": restart_entries}
            with open(restart_file, "w") as f:
                json.dump(data, f)
        except Exception:
            pass

    def __init__(self, config: CFConfig):
        self.config = config
        self.runner = CFJavaTestRunner(config)
        self._app_status_cache = {}
        self._cache_timestamp = 0
        # Track which apps have been initially restarted in this session
        self._initially_restarted_apps = set()

    def check_current_cf_target(self) -> Dict[str, str]:
        """Check current CF target information."""
        result = self.runner.run_command("cf target", timeout=10)
        if result.failed:
            return {}

        target_info = {}
        lines = result.stdout.split("\n")

        for line in lines:
            line = line.strip()
            if line.startswith("api endpoint:"):
                target_info["api"] = line.split(":", 1)[1].strip()
            elif line.startswith("user:"):
                target_info["user"] = line.split(":", 1)[1].strip()
            elif line.startswith("org:"):
                target_info["org"] = line.split(":", 1)[1].strip()
            elif line.startswith("space:"):
                target_info["space"] = line.split(":", 1)[1].strip()

        return target_info

    def is_logged_in_correctly(self) -> bool:
        """Check if we're logged in with the correct credentials and target."""
        target_info = self.check_current_cf_target()

        if not target_info:
            return False

        # Check all required fields match
        expected = {
            "api": self.config.api_endpoint,
            "user": self.config.username,
            "org": self.config.org,
            "space": self.config.space,
        }

        for key, expected_value in expected.items():
            current_value = target_info.get(key, "").strip()
            if current_value != expected_value:
                # Only print mismatches for debugging, no detailed output
                return False

        return True

    def login(self) -> bool:
        """Login to CF only if needed, with file-based state tracking to prevent redundant logins."""
        import time

        # Create a config signature for comparison
        current_config = {
            "username": self.config.username,
            "password": self.config.password,
            "api_endpoint": self.config.api_endpoint,
            "org": self.config.org,
            "space": self.config.space,
        }

        with self._lock:
            # Load current login state from file
            login_state = self._load_login_state()

            # Check if already logged in with same config
            if login_state["logged_in"] and login_state["login_config"] == current_config:
                # Check if login is still valid (not older than 30 minutes)
                login_timestamp = login_state["login_timestamp"]
                if isinstance(login_timestamp, (int, float)):
                    login_age = time.time() - login_timestamp
                    if login_age < 1800:  # 30 minutes
                        print("🔗 LOGIN: Using existing session (already logged in during this test run)")
                        return True
                    else:
                        print("🔗 LOGIN: Previous session expired, re-authenticating...")
                        login_state["logged_in"] = False

        # Fast check: are we already logged in with correct credentials?
        if self.is_logged_in_correctly():
            print("🔗 LOGIN: Already logged in with correct credentials")
            # Update file-based state
            with self._lock:
                login_state = {"logged_in": True, "login_config": current_config, "login_timestamp": time.time()}
                self._save_login_state(login_state)
            return True

        print("🔗 LOGIN: Logging in to CF...")
        try:
            cmd = (
                f"cf login -u {self.config.username} -p '{self.config.password}' "
                f"-a {self.config.api_endpoint} -o {self.config.org} -s {self.config.space}"
            )
            result = self.runner.run_command(cmd, timeout=60)

            if result.success:
                print("✅ LOGIN: Successfully logged in to CF")
                # Update file-based state on successful login
                with self._lock:
                    login_state = {"logged_in": True, "login_config": current_config, "login_timestamp": time.time()}
                    self._save_login_state(login_state)
                return True
            else:
                print(f"❌ LOGIN: CF login failed: {result.stderr}")
                return False

        except KeyboardInterrupt:
            print("🛑 LOGIN: Login cancelled by CTRL-C")
            return False

    def deploy_apps(self) -> bool:
        """Deploy test applications."""
        success = True

        # Find apps directory using the same logic as auto-detection
        apps_base_path = None
        possible_paths = [
            os.path.join(os.getcwd(), "apps"),  # From testing dir
            os.path.join(os.getcwd(), "..", "testing", "apps"),  # From framework dir
            os.path.join(os.path.dirname(__file__), "..", "apps"),  # Relative to this file
            os.path.join(os.path.dirname(__file__), "..", "..", "testing", "apps"),  # Up two levels
        ]

        for path in possible_paths:
            if os.path.exists(path) and os.path.isdir(path):
                apps_base_path = path
                break

        if not apps_base_path:
            print("No apps directory found, cannot deploy apps")
            return False

        print(f"Using apps directory: {apps_base_path}")

        # Deploy each detected app
        for app_key, app_name in self.config.apps.items():
            app_path = os.path.join(apps_base_path, app_key)
            if os.path.exists(app_path):
                print(f"Deploying {app_name} from {app_path}")
                result = self.runner.run_command(f"cd '{app_path}' && cf push --no-start", timeout=120)
                if result.failed:
                    print(f"Failed to deploy {app_name}: {result.stderr}")
                    success = False
                else:
                    print(f"Successfully deployed {app_name}")
            else:
                print(f"App directory not found: {app_path}")
                success = False

        return success

    def start_apps(self) -> bool:
        """Start all test applications."""
        success = True
        for app_name in self.config.apps.values():
            print(f"Starting application: {app_name}")
            result = self.runner.run_command(
                f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
            )
            if result.failed:
                print(f"Failed to start {app_name}: {result.stderr}")
                success = False
        return success

    def start_apps_parallel(self) -> bool:
        """Start all test applications in parallel."""
        if not self.config.apps:
            return True

        app_names = list(self.config.apps.values())

        results = {}
        threads = []

        def start_single_app(app_name: str):
            """Start a single app and store the result."""
            result = self.runner.run_command(
                f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
            )
            results[app_name] = result

        # Start all start operations in parallel
        for app_name in app_names:
            thread = threading.Thread(target=start_single_app, args=(app_name,))
            thread.start()
            threads.append(thread)

        # Wait for all operations to complete
        for thread in threads:
            thread.join()

        # Check results and report any failures
        success = True
        for app_name, result in results.items():
            if result.failed:
                print(f"Failed to start {app_name}: {result.stderr}")
                success = False

        return success

    def restart_apps(self) -> bool:
        """Restart all test applications."""
        success = True
        for app_name in self.config.apps.values():
            result = self.runner.run_command(
                f"cf restart {app_name}", timeout=self.config.config["timeouts"]["app_start"]
            )
            if result.failed:
                print(f"Failed to restart {app_name}: {result.stderr}")
                success = False
        return success

    def restart_apps_parallel(self) -> bool:
        """Restart all test applications in parallel."""
        if not self.config.apps:
            return True

        app_names = list(self.config.apps.values())

        results = {}
        threads = []

        def restart_single_app(app_name: str):
            """Restart a single app and store the result."""
            result = self.runner.run_command(
                f"cf restart {app_name}", timeout=self.config.config["timeouts"]["app_start"]
            )
            results[app_name] = result

        # Start all restart operations in parallel
        for app_name in app_names:
            thread = threading.Thread(target=restart_single_app, args=(app_name,))
            thread.start()
            threads.append(thread)

        # Wait for all operations to complete
        for thread in threads:
            thread.join()

        # Check results and report any failures
        success = True
        for app_name, result in results.items():
            if result.failed:
                print(f"Failed to restart {app_name}: {result.stderr}")
                success = False

        return success

    def delete_apps(self) -> bool:
        """Delete test applications."""
        # Legacy SKIP_DELETE environment variable (for backwards compatibility)
        if os.environ.get("SKIP_DELETE", "").lower() == "true":
            print("Skipping app deletion due to SKIP_DELETE=true")
            return True

        success = True
        for app_name in self.config.apps.values():
            print(f"Deleting app: {app_name}")
            result = self.runner.run_command(f"cf delete {app_name} -f", timeout=60)
            if result.failed:
                print(f"Failed to delete {app_name}: {result.stderr}")
                success = False
            else:
                print(f"Successfully deleted {app_name}")
        return success

    def _clear_app_cache_if_stale(self, max_age_seconds: int = 30):
        """Clear app status cache if it's too old."""
        import time

        current_time = time.time()
        if current_time - self._cache_timestamp > max_age_seconds:
            self._app_status_cache.clear()
            self._cache_timestamp = current_time

    def check_app_status(self, app_name: str, use_cache: bool = True) -> str:
        """Check the status of an application with optional caching."""
        if use_cache:
            self._clear_app_cache_if_stale()
            if app_name in self._app_status_cache:
                return self._app_status_cache[app_name]

        result = self.runner.run_command(f"cf app {app_name}", timeout=15)
        if result.failed:
            status = "unknown"
        else:
            # Parse the output to determine status
            output = result.stdout.lower()
            if "running" in output:
                status = "running"
            elif "stopped" in output:
                status = "stopped"
            elif "crashed" in output:
                status = "crashed"
            else:
                status = "unknown"

        if use_cache:
            self._app_status_cache[app_name] = status

        return status

    def check_all_apps_status(self) -> Dict[str, str]:
        """Check status of all configured apps efficiently."""
        # Use cf apps command to get all app statuses at once
        result = self.runner.run_command("cf apps", timeout=20)
        statuses = {}

        if result.failed:
            # Fallback to individual checks
            for app_name in self.config.apps.values():
                statuses[app_name] = self.check_app_status(app_name, use_cache=False)
            return statuses

        # Parse cf apps output
        lines = result.stdout.split("\n")
        for line in lines:
            line = line.strip()
            if not line or line.startswith("name") or line.startswith("Getting"):
                continue

            parts = line.split()
            if len(parts) >= 3:  # name, requested state, processes
                app_name = parts[0]
                if app_name in self.config.apps.values():
                    requested_state = parts[1].lower()  # "started" or "stopped"
                    processes = parts[2] if len(parts) > 2 else ""  # e.g., "web:1/1" or "web:0/1"

                    # Determine status based on requested state and process info
                    if requested_state == "stopped":
                        statuses[app_name] = "stopped"
                    elif requested_state == "started":
                        # Check if processes are actually running
                        # Format is typically "web:1/1" where first number is running instances
                        if ":" in processes:
                            process_parts = processes.split(":")
                            if len(process_parts) > 1:
                                instance_info = process_parts[1].split("/")
                                if len(instance_info) >= 2:
                                    running_instances = instance_info[0]
                                    try:
                                        if int(running_instances) > 0:
                                            statuses[app_name] = "running"
                                        else:
                                            statuses[app_name] = "stopped"  # Started but no running instances
                                    except ValueError:
                                        statuses[app_name] = "unknown"
                                else:
                                    statuses[app_name] = "unknown"
                            else:
                                statuses[app_name] = "unknown"
                        else:
                            # No process info, assume running if started
                            statuses[app_name] = "running"
                    else:
                        statuses[app_name] = "unknown"

        # Cache the results
        import time

        self._cache_timestamp = time.time()
        self._app_status_cache.update(statuses)

        # Fill in any missing apps with individual checks
        for app_name in self.config.apps.values():
            if app_name not in statuses:
                statuses[app_name] = self.check_app_status(app_name, use_cache=False)

        return statuses

    def deploy_apps_if_needed(self) -> bool:
        """Deploy apps only if they don't exist."""
        success = True

        # Check which apps already exist efficiently
        app_exists = self.check_apps_exist()

        # Find apps directory using the same logic as auto-detection
        apps_base_path = None
        possible_paths = [
            os.path.join(os.getcwd(), "apps"),  # From testing dir
            os.path.join(os.getcwd(), "..", "testing", "apps"),  # From framework dir
            os.path.join(os.path.dirname(__file__), "..", "apps"),  # Relative to this file
            os.path.join(os.path.dirname(__file__), "..", "..", "testing", "apps"),  # Up two levels
        ]

        for path in possible_paths:
            if os.path.exists(path) and os.path.isdir(path):
                apps_base_path = path
                break

        if not apps_base_path:
            print("No apps directory found, cannot deploy apps")
            return False

        # Check and deploy each app if needed
        apps_to_deploy = []
        for app_key, app_name in self.config.apps.items():
            # Check if app already exists
            if not app_exists.get(app_name, False):
                app_path = os.path.join(apps_base_path, app_key)
                if os.path.exists(app_path):
                    apps_to_deploy.append((app_key, app_name))
                    print(f"🚀 DEPLOY IF NEEDED: {app_name} needs deployment")
                else:
                    print(f"❌ DEPLOY IF NEEDED: App directory not found: {app_path}")
                    success = False
            else:
                print(f"✅ DEPLOY IF NEEDED: {app_name} already exists, skipping deployment")

        if apps_to_deploy:
            print(f"Deploying {len(apps_to_deploy)} apps...")
            for app_key, app_name in apps_to_deploy:
                app_path = os.path.join(apps_base_path, app_key)
                print(f"Deploying {app_name} from {app_path}")
                result = self.runner.run_command(f"cd '{app_path}' && cf push --no-start", timeout=120)
                if result.failed:
                    print(f"Failed to deploy {app_name}: {result.stderr}")
                    success = False
                else:
                    print(f"Successfully deployed {app_name}")

        return success

    def start_apps_if_needed(self) -> bool:
        """Start apps only if they're not already running."""
        success = True

        # Get all app statuses at once for efficiency
        app_statuses = self.check_all_apps_status()

        apps_to_start = []
        for app_name in self.config.apps.values():
            status = app_statuses.get(app_name, "unknown")
            if status != "running":
                apps_to_start.append(app_name)
                print(f"🚀 START IF NEEDED: {app_name} is {status} → will start")
            else:
                print(f"✅ START IF NEEDED: {app_name} is already running")

        if apps_to_start:
            print(f"🚀 START IF NEEDED: Starting {len(apps_to_start)} apps...")
            for app_name in apps_to_start:
                print(f"🚀 START IF NEEDED: Starting {app_name}")
                result = self.runner.run_command(
                    f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                )
                if result.failed:
                    print(f"❌ START IF NEEDED FAILED: {app_name}: {result.stderr}")
                    success = False
                else:
                    print(f"✅ START IF NEEDED SUCCESS: {app_name}")
        else:
            print("✅ START IF NEEDED: No apps need starting - all are already running")

        return success

    def start_apps_if_needed_parallel(self) -> bool:
        """Start apps in parallel only if they're not already running."""
        # Get all app statuses at once for efficiency
        app_statuses = self.check_all_apps_status()

        apps_to_start = []
        for app_name in self.config.apps.values():
            status = app_statuses.get(app_name, "unknown")
            if status != "running":
                apps_to_start.append(app_name)
                print(f"🚀 PARALLEL START IF NEEDED: {app_name} is {status} → will start")
            else:
                print(f"✅ PARALLEL START IF NEEDED: {app_name} is already running")

        if not apps_to_start:
            print("✅ PARALLEL START IF NEEDED: No apps need starting - all are already running")
            return True

        results = {}
        threads = []

        def start_single_app(app_name: str):
            """Start a single app and store the result."""
            try:
                print(f"🚀 PARALLEL START IF NEEDED: Starting start for {app_name}")
                result = self.runner.run_command(
                    f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                )
                results[app_name] = result
                if result.failed:
                    print(f"❌ PARALLEL START IF NEEDED FAILED: {app_name}: {result.stderr}")
                else:
                    print(f"✅ PARALLEL START IF NEEDED SUCCESS: {app_name}")
            except KeyboardInterrupt:
                print(f"🛑 PARALLEL START IF NEEDED CANCELLED: {app_name} (CTRL-C)")
                results[app_name] = CommandResult(-1, "", "Cancelled by user", f"cf start {app_name}")

        # Start all start operations in parallel
        for app_name in apps_to_start:
            thread = threading.Thread(target=start_single_app, args=(app_name,))
            thread.start()
            threads.append(thread)

        # Wait for all operations to complete
        for thread in threads:
            thread.join()

        # Check results and report any failures
        success = True
        for app_name, result in results.items():
            if result.failed:
                print(f"❌ PARALLEL START IF NEEDED FAILED: {app_name}: {result.stderr}")
                success = False

        if success:
            print(f"✅ PARALLEL START IF NEEDED: All {len(apps_to_start)} operations completed successfully")

        return success

    def check_apps_exist(self) -> Dict[str, bool]:
        """Check which apps exist in CF."""
        result = self.runner.run_command("cf apps", timeout=20)
        app_exists = {}

        if result.failed:
            # If cf apps fails, assume all apps don't exist
            for app_name in self.config.apps.values():
                app_exists[app_name] = False
            return app_exists

        # Parse cf apps output to see which apps exist
        lines = result.stdout.split("\n")
        existing_apps = set()

        for line in lines:
            line = line.strip()
            if not line or line.startswith("name") or line.startswith("Getting"):
                continue

            parts = line.split()
            if len(parts) >= 1:
                app_name = parts[0]
                existing_apps.add(app_name)

        # Check each configured app
        for app_name in self.config.apps.values():
            app_exists[app_name] = app_name in existing_apps

        return app_exists

    def restart_apps_if_needed(self) -> bool:
        """Restart apps only if they're not running or have crashed."""
        print("🧠 SMART RESTART: Checking which apps need restart...")
        success = True

        # Get all app statuses at once for efficiency
        app_statuses = self.check_all_apps_status()

        apps_to_restart = []
        apps_to_start = []

        for app_name in self.config.apps.values():
            status = app_statuses.get(app_name, "unknown")

            if status == "running":
                apps_to_restart.append(app_name)
                print(f"🔄 SMART RESTART: {app_name} is running → will restart")
            elif status in ["stopped", "crashed"]:
                apps_to_start.append(app_name)
                print(f"🚀 SMART RESTART: {app_name} is {status} → will start")
            else:
                apps_to_restart.append(app_name)  # Unknown status, try restart
                print(f"❓ SMART RESTART: {app_name} status unknown → will restart")

        if apps_to_restart:
            print(f"🔄 SMART RESTART: Restarting {len(apps_to_restart)} running apps...")
            for app_name in apps_to_restart:
                print(f"🔄 SMART RESTART: Restarting {app_name}")
                result = self.runner.run_command(
                    f"cf restart {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                )
                if result.failed:
                    print(f"❌ SMART RESTART FAILED: {app_name}: {result.stderr}")
                    success = False
                else:
                    print(f"✅ SMART RESTART SUCCESS: {app_name}")

        if apps_to_start:
            print(f"🚀 SMART RESTART: Starting {len(apps_to_start)} stopped apps...")
            for app_name in apps_to_start:
                print(f"🚀 SMART RESTART: Starting {app_name}")
                result = self.runner.run_command(
                    f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                )
                if result.failed:
                    print(f"❌ SMART START FAILED: {app_name}: {result.stderr}")
                    success = False
                else:
                    print(f"✅ SMART START SUCCESS: {app_name}")

        if not apps_to_restart and not apps_to_start:
            print("✅ SMART RESTART: No apps need restart - all are already running")

        return success

    def restart_apps_if_needed_parallel(self) -> bool:
        """Restart apps in parallel only if they're not running or have crashed."""
        print("🚀 SMART PARALLEL RESTART: Checking which apps need restart...")

        try:
            # Get all app statuses at once for efficiency
            app_statuses = self.check_all_apps_status()

            apps_to_restart = []
            apps_to_start = []

            for app_name in self.config.apps.values():
                status = app_statuses.get(app_name, "unknown")

                if status == "running":
                    apps_to_restart.append(app_name)
                    print(f"🔄 SMART PARALLEL: {app_name} is running → will restart")
                elif status in ["stopped", "crashed"]:
                    apps_to_start.append(app_name)
                    print(f"🚀 SMART PARALLEL: {app_name} is {status} → will start")
                else:
                    apps_to_restart.append(app_name)  # Unknown status, try restart
                    print(f"❓ SMART PARALLEL: {app_name} status unknown → will restart")

            if not apps_to_restart and not apps_to_start:
                print("✅ SMART PARALLEL: No apps need restart - all are already running")
                return True

            total_ops = len(apps_to_restart) + len(apps_to_start)
            print(f"🚀 SMART PARALLEL: Starting {total_ops} operations in parallel...")

            results = {}
            threads = []

            def restart_single_app(app_name: str):
                """Restart a single app and store the result."""
                try:
                    print(f"🔄 SMART PARALLEL: Starting restart for {app_name}")
                    result = self.runner.run_command(
                        f"cf restart {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                    )
                    results[app_name] = ("restart", result)
                    if result.failed:
                        print(f"❌ SMART PARALLEL RESTART FAILED: {app_name}: {result.stderr}")
                    else:
                        print(f"✅ SMART PARALLEL RESTART SUCCESS: {app_name}")
                except KeyboardInterrupt:
                    print(f"🛑 SMART PARALLEL RESTART CANCELLED: {app_name} (CTRL-C)")
                    results[app_name] = (
                        "restart",
                        CommandResult(-1, "", "Cancelled by user", f"cf restart {app_name}"),
                    )

            def start_single_app(app_name: str):
                """Start a single app and store the result."""
                try:
                    print(f"🚀 SMART PARALLEL: Starting start for {app_name}")
                    result = self.runner.run_command(
                        f"cf start {app_name}", timeout=self.config.config["timeouts"]["app_start"]
                    )
                    results[app_name] = ("start", result)
                    if result.failed:
                        print(f"❌ SMART PARALLEL START FAILED: {app_name}: {result.stderr}")
                    else:
                        print(f"✅ SMART PARALLEL START SUCCESS: {app_name}")
                except KeyboardInterrupt:
                    print(f"🛑 SMART PARALLEL START CANCELLED: {app_name} (CTRL-C)")
                    results[app_name] = ("start", CommandResult(-1, "", "Cancelled by user", f"cf start {app_name}"))

            # Start all restart operations in parallel
            if apps_to_restart:
                for app_name in apps_to_restart:
                    thread = threading.Thread(target=restart_single_app, args=(app_name,))
                    thread.start()
                    threads.append(thread)

            # Start all start operations in parallel
            if apps_to_start:
                for app_name in apps_to_start:
                    thread = threading.Thread(target=start_single_app, args=(app_name,))
                    thread.start()
                    threads.append(thread)

            # Wait for all operations to complete
            for thread in threads:
                thread.join()

            # Check results and report any failures
            success = True
            cancelled_count = 0
            for app_name, (operation, result) in results.items():
                if result.failed:
                    success = False
                    if "Cancelled by user" in result.stderr:
                        cancelled_count += 1

            if cancelled_count > 0:
                print(f"🛑 SMART PARALLEL RESTART: {cancelled_count} operations cancelled by user")

            return success
        except Exception as e:
            print(f"❌ SMART PARALLEL RESTART: Failed to restart apps: {e}")
            return False

    @classmethod
    def reset_global_login_state(cls):
        """Reset the global login state (for testing)."""
        with cls._lock:
            default_state = {"logged_in": False, "login_config": {}, "login_timestamp": 0.0}
            cls._save_login_state(default_state)
        print("🔗 LOGIN: Global login state reset")

    @classmethod
    def get_global_login_info(cls) -> str:
        """Get information about the current global login state."""
        state = cls._load_login_state()
        if state["logged_in"]:
            import time

            timestamp = state["login_timestamp"]
            config = state["login_config"]

            if isinstance(timestamp, (int, float)) and isinstance(config, dict):
                login_age = time.time() - timestamp
                return (
                    f"Logged in as {config.get('username', 'unknown')} @ "
                    f"{config.get('api_endpoint', 'unknown')} for {login_age:.0f}s"
                )
            else:
                return "Logged in (invalid state data)"
        else:
            return "Not logged in"

    @classmethod
    def add_deferred_restart_app(cls, app_name: str, test_class: str = "Unknown", reason: str = "no_restart=True"):
        """Add an app to the deferred restart list (due to no_restart=True test)."""
        with cls._lock:
            restart_data = cls._load_restart_state()
            restart_entries = restart_data.get("restart_entries", [])

            # Check if app is already in the list for this test class
            existing_entry = next(
                (entry for entry in restart_entries if entry["app"] == app_name and entry["test_class"] == test_class),
                None,
            )

            if not existing_entry:
                restart_entries.append(
                    {"app": app_name, "test_class": test_class, "reason": reason, "timestamp": time.time()}
                )
                cls._save_restart_state(restart_entries)

        print(f"🚫➡️ DEFERRED RESTART: Tracking {app_name} for later restart (from {test_class})")

    @classmethod
    def get_deferred_restart_apps(cls) -> set:
        """Get the set of apps that need deferred restarts."""
        restart_data = cls._load_restart_state()
        restart_entries = restart_data.get("restart_entries", [])
        return set(entry["app"] for entry in restart_entries)

    @classmethod
    def get_deferred_restart_details(cls) -> List[Dict]:
        """Get detailed information about deferred restarts including test class names."""
        restart_data = cls._load_restart_state()
        return restart_data.get("restart_entries", [])

    @classmethod
    def clear_deferred_restart_apps(cls):
        """Clear the deferred restart apps list."""
        with cls._lock:
            restart_data = cls._load_restart_state()
            restart_entries = restart_data.get("restart_entries", [])
            if restart_entries:
                apps_list = [entry["app"] for entry in restart_entries]
                cls._save_restart_state([])
                print(f"🧹 DEFERRED RESTART: Cleared deferred restart list: {apps_list}")
            else:
                print("🧹 DEFERRED RESTART: No apps in deferred restart list")

    @classmethod
    def has_deferred_restarts(cls) -> bool:
        """Check if there are any apps pending deferred restart."""
        restart_data = cls._load_restart_state()
        restart_entries = restart_data.get("restart_entries", [])
        return bool(restart_entries)

    def process_deferred_restarts(self, restart_mode: str = "smart_parallel") -> bool:
        """Process any deferred restarts before proceeding with the current test."""
        with self._lock:
            restart_data = self._load_restart_state()
            restart_entries = restart_data.get("restart_entries", [])
            if not restart_entries:
                return True

            apps_to_restart = [entry["app"] for entry in restart_entries]
            test_classes = [entry["test_class"] for entry in restart_entries]
            print(
                f"🔄➡️ DEFERRED RESTART: Processing deferred restarts for apps: {apps_to_restart}"
                f"(from test classes: {set(test_classes)})"
            )

            # Clear the deferred list before attempting restarts
            self._save_restart_state([])

        # Perform the actual restart based on mode
        try:
            if restart_mode == "smart_parallel":
                print("🚀 DEFERRED RESTART: Using smart parallel restart")
                return self.restart_apps_if_needed_parallel()
            elif restart_mode == "smart":
                print("🧠 DEFERRED RESTART: Using smart restart")
                return self.restart_apps_if_needed()
            elif restart_mode == "parallel":
                print("🔄 DEFERRED RESTART: Using parallel restart")
                return self.restart_apps_parallel()
            elif restart_mode == "always":
                print("🔄 DEFERRED RESTART: Using always restart")
                return self.restart_apps()
            else:
                print("🚀 DEFERRED RESTART: Using default smart parallel restart")
                return self.restart_apps_if_needed_parallel()
        except Exception as e:
            print(f"❌ DEFERRED RESTART: Failed to process deferred restarts: {e}")
            return False

    @classmethod
    def cleanup_state_files(cls):
        """Clean up temporary state files (call at end of test run)."""
        login_file, restart_file = cls._get_state_files()

        # Clean up login state file
        if login_file and os.path.exists(login_file):
            try:
                os.unlink(login_file)
            except Exception:
                pass

        # Clean up restart state file
        if restart_file and os.path.exists(restart_file):
            try:
                os.unlink(restart_file)
            except Exception:
                pass

        # Reset file paths so they'll be recreated if needed
        cls._login_state_file = None
        cls._restart_state_file = None

    def restart_single_app(self, app_name: str) -> bool:
        """Restart a single specific application."""
        print(f"🔄 SINGLE APP RESTART: Restarting {app_name}")
        result = self.runner.run_command(f"cf restart {app_name}", timeout=self.config.config["timeouts"]["app_start"])
        if result.failed:
            print(f"⚠️ SINGLE APP RESTART: Failed to restart {app_name}: {result.stderr}")
            return False
        print(f"✅ SINGLE APP RESTART: Successfully restarted {app_name}")
        return True

    def restart_single_app_if_needed(self, app_name: str) -> bool:
        """Restart a single app only if it's not running or unhealthy."""
        print(f"🔄 SMART SINGLE RESTART: Checking if {app_name} needs restart")

        # Check if app is running and healthy
        result = self.runner.run_command("cf apps")
        if result.failed:
            print("⚠️ SMART SINGLE RESTART: Failed to check app status")
            return False

        # Parse the apps output to check status
        lines = result.stdout.strip().split("\n")
        app_found = False
        needs_restart = True

        for line in lines:
            if app_name in line:
                app_found = True
                # Check if app is running (look for "started" state)
                if "started" in line.lower():
                    print(f"✅ SMART SINGLE RESTART: {app_name} is already running, no restart needed")
                    needs_restart = False
                else:
                    print(f"🔄 SMART SINGLE RESTART: {app_name} is not running, restart needed")
                break

        if not app_found:
            print(f"⚠️ SMART SINGLE RESTART: {app_name} not found in app list")
            return False

        if needs_restart:
            return self.restart_single_app(app_name)

        return True

    def needs_initial_restart(self, app_name: str) -> bool:
        """Check if an app needs initial restart for this session."""
        restart_behavior = os.environ.get("RESTART_APPS", "smart_parallel").lower()

        # If restart is disabled, never restart
        if restart_behavior == "never":
            return False

        # Check if app has already been initially restarted in this session
        return app_name not in self._initially_restarted_apps

    def mark_app_initially_restarted(self, app_name: str):
        """Mark an app as having been initially restarted in this session."""
        self._initially_restarted_apps.add(app_name)
        print(f"📝 SESSION TRACKING: Marked {app_name} as initially restarted")

    def restart_single_app_with_initial_check(self, app_name: str) -> bool:
        """Restart a single app, handling initial restart logic."""
        if self.needs_initial_restart(app_name):
            print(f"🔄 INITIAL RESTART: First use of {app_name} in this session - performing initial restart")
            success = self.restart_single_app(app_name)
            if success:
                self.mark_app_initially_restarted(app_name)
            return success
        else:
            print(f"✅ INITIAL RESTART: {app_name} already restarted in this session, performing smart restart")
            return self.restart_single_app_if_needed(app_name)

    def restart_single_app_if_needed_with_initial_check(self, app_name: str) -> bool:
        """Smart restart a single app, handling initial restart logic."""
        if self.needs_initial_restart(app_name):
            print(f"🔄 INITIAL SMART RESTART: First use of {app_name} in this session - ensuring it's running")
            # For initial restart, we want to ensure the app is definitely restarted
            # even if it appears to be running, to guarantee fresh state
            success = self.restart_single_app(app_name)
            if success:
                self.mark_app_initially_restarted(app_name)
            return success
        else:
            print(f"✅ INITIAL SMART RESTART: {app_name} already restarted in this session, performing smart check")
            return self.restart_single_app_if_needed(app_name)
