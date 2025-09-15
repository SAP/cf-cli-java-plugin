"""
Fluent DSL for CF Java Plugin testing.
Provides a clean, readable interface for test assertions.
"""

import glob
import re
import time
from typing import TYPE_CHECKING, Dict, List, Optional

from .core import CFJavaTestRunner, CommandResult, TestContext

if TYPE_CHECKING:
    from .file_validators import FileType


def is_ssh_auth_error(output: str) -> tuple[bool, Optional[str]]:
    """
    Check if the given output contains SSH authentication errors.

    Returns:
        tuple: (is_auth_error: bool, detected_error: Optional[str])
    """
    ssh_auth_errors = [
        "Error getting one time auth code: Error getting SSH code: Error requesting one time code from server:",
        "Error getting one time auth code",
        "Error getting SSH code",
        "Authentication failed",
        "SSH authentication failed",
        "Error opening SSH connection: ssh: handshake failed",
    ]

    for error_pattern in ssh_auth_errors:
        if error_pattern in output:
            return True, error_pattern

    return False, None


class FluentAssertion:
    """Fluent assertion interface for test results."""

    def __init__(self, result: CommandResult, context: TestContext, runner: CFJavaTestRunner):
        self.result = result
        self.context = context
        self.runner = runner
        self._remote_files_before: Optional[Dict] = None
        self._test_name: Optional[str] = None
        self._command_name: Optional[str] = None

    # Command execution assertions
    def should_succeed(self) -> "FluentAssertion":
        """Assert that the command succeeded."""
        if self.result.failed:
            # Check for SSH auth errors that should trigger re-login and restart
            output_to_check = self.result.stderr + " " + self.result.stdout
            ssh_error_detected, detected_error = is_ssh_auth_error(output_to_check)
            print("🔍 Checking for SSH auth errors in command output...")
            print("output_to_check:", output_to_check)
            print("ssh_error_detected:", ssh_error_detected)
            if ssh_error_detected:
                import pytest

                print(f"🔄 SSH AUTH ERROR DETECTED: {detected_error}")
                print("🔄 SSH AUTH ERROR DETECTED: Attempting re-login and app restart")

                # Try to recover by re-logging in and restarting the app
                try:
                    # Force re-login by clearing login state
                    from .core import CFManager

                    CFManager.reset_global_login_state()

                    time.sleep(5)

                    # Re-login
                    cf_manager = CFManager(self.runner.config)
                    login_success = cf_manager.login()

                    if login_success:
                        print("✅ SSH AUTH RECOVERY: Successfully re-logged in")

                        # Restart the app
                        if hasattr(self.context, "app_name") and self.context.app_name:
                            restart_success = cf_manager.restart_single_app(self.context.app_name)
                            if restart_success:
                                print(f"✅ SSH AUTH RECOVERY: Successfully restarted {self.context.app_name}")

                                # Re-run the original command
                                print(f"🔄 SSH AUTH RECOVERY: Retrying original command: {self.result.command}")
                                retry_result = self.runner.run_command(
                                    self.result.command, app_name=self.context.app_name
                                )

                                # Replace the result with the retry result
                                self.result = retry_result

                                # Check if retry succeeded
                                if not retry_result.failed:
                                    print("✅ SSH AUTH RECOVERY: Command succeeded after recovery")
                                    return self
                                else:
                                    print(
                                        "❌ SSH AUTH RECOVERY: Command still failed after recovery: "
                                        f"{retry_result.stderr}"
                                    )
                            else:
                                print(f"❌ SSH AUTH RECOVERY: Failed to restart {self.context.app_name}")
                        else:
                            print("❌ SSH AUTH RECOVERY: No app name available for restart")
                    else:
                        print("❌ SSH AUTH RECOVERY: Failed to re-login")

                except Exception as e:
                    print(f"❌ SSH AUTH RECOVERY: Exception during recovery: {e}")

                # If we get here, recovery failed or retry failed, so skip the test
                pytest.skip(f"Test skipped due to SSH auth error (CF platform issue): {detected_error}")

            raise AssertionError(
                f"Expected command to succeed, but it failed with code {self.result.returncode}:\n"
                f"Command: {self.result.command}\n"
                f"Error: {self.result.stderr}\n"
                f"Output: {self.result.stdout[:1000]}"  # Show first 1000 chars of output
            )
        return self

    def should_fail(self) -> "FluentAssertion":
        """Assert that the command failed."""
        if self.result.success:
            raise AssertionError(
                f"Expected command to fail, but it succeeded:\n"
                f"Command: {self.result.command}\n"
                f"Output: {self.result.stdout}"
            )
        return self

    # Output content assertions
    def should_contain(self, text: str, ignore_case: bool = False) -> "FluentAssertion":
        """Assert that output contains specific text."""
        output = self.result.output if not ignore_case else self.result.output.lower()
        text = text if not ignore_case else text.lower()
        if text not in output:
            raise AssertionError(
                f"Expected output to contain '{text}', but it didn't:\n"
                f"Actual output: {self.result.output[:1000]}..."
            )
        return self

    def should_not_contain(self, text: str, ignore_case: bool = False) -> "FluentAssertion":
        """Assert that output doesn't contain specific text."""
        output = self.result.output if not ignore_case else self.result.output.lower()
        text = text if not ignore_case else text.lower()
        if text in output:
            raise AssertionError(
                f"Expected output NOT to contain '{text}', but it did:\n"
                f"Actual output: {self.result.output[:1000]}..."
            )
        return self

    def should_start_with(self, text: str, ignore_case: bool = False) -> "FluentAssertion":
        """Assert that output starts with specific text."""
        output = self.result.output if not ignore_case else self.result.output.lower()
        text = text if not ignore_case else text.lower()
        if not output.startswith(text):
            raise AssertionError(
                f"Expected output to start with '{text}', but it didn't:\n"
                f"Actual output: {self.result.output[:1000]}..."
            )
        return self

    def should_match(self, pattern: str) -> "FluentAssertion":
        """Assert that output matches regex pattern."""
        if not re.search(pattern, self.result.output, re.MULTILINE | re.DOTALL):
            raise AssertionError(
                f"Expected output to match pattern '{pattern}', but it didn't:\n"
                f"Actual output: {self.result.output[:1000]}..."
            )
        return self

    def should_have_at_least(self, min_lines: int, description: str = "lines") -> "FluentAssertion":
        """Assert minimum line count."""
        lines = self.result.output.split("\n")
        actual_lines = len([line for line in lines if line.strip()])
        if actual_lines < min_lines:
            raise AssertionError(f"Expected at least {min_lines} {description}, but got {actual_lines}")
        return self

    # File assertions
    def should_create_file(self, pattern: str, validate_as: Optional["FileType"] = None) -> "FluentAssertion":
        """Assert that a file matching pattern was created locally.

        Args:
            pattern: Glob pattern to match created files
            validate_as: Optional file type validation (FileType.HEAP_DUMP, FileType.JFR, etc.)
        """
        files = glob.glob(pattern)
        if not files:
            all_files = list(self.context.new_files)
            raise AssertionError(
                f"Expected file matching '{pattern}' to be created, but none found.\n" f"Files created: {all_files}"
            )

        # If validation is requested, validate the file
        if validate_as is not None:
            from .file_validators import validate_generated_file

            try:
                validate_generated_file(pattern, validate_as)
            except Exception as e:
                raise AssertionError(f"File validation failed: {e}")

        return self

    def should_create_no_files(self) -> "FluentAssertion":
        """Assert that no local files were created."""
        if self.context.new_files:
            raise AssertionError(f"Expected no files to be created, but found: {list(self.context.new_files)}")
        return self

    def should_not_create_file(self, pattern: str = ".*") -> "FluentAssertion":
        """Assert that no file matching pattern was created."""
        files = glob.glob(pattern)
        if files:
            raise AssertionError(f"Expected no file matching '{pattern}', but found: {files}")
        return self

    # Remote file assertions
    def should_create_no_remote_files(self) -> "FluentAssertion":
        """Assert that no new files were left on the remote container after the command."""
        if self._remote_files_before is None:
            # If no before state was captured, we can't reliably detect new files
            # This is a limitation - we should warn about this
            print(
                "Warning: should_create_no_remote_files() called without before state;"
                "cannot reliably detect new files"
            )
            return self
        else:
            # Capture current state and compare
            after_state = self.runner.capture_remote_file_state(self.context.app_name)
            new_files = self.runner.compare_remote_file_states(self._remote_files_before, after_state)

            if new_files:
                # Format the error message nicely
                error_parts = []
                for directory, files in new_files.items():
                    error_parts.append(f"  {directory}: {files}")
                error_msg = "New files left on remote after command execution:\n" + "\n".join(error_parts)
                raise AssertionError(error_msg)

        return self

    def _get_recursive_files(self, folder: str) -> List[str]:
        """Get all files recursively from a remote folder."""
        # Use find command for recursive file listing
        cmd = f"cf ssh {self.context.app_name} -c 'find {folder} -type f 2>/dev/null || echo NO_DIRECTORY'"
        result = self.runner.run_command(cmd, app_name=self.context.app_name, timeout=15)

        if result.success:
            output = result.stdout.strip()
            if output == "NO_DIRECTORY" or not output:
                return []
            else:
                # Return full file paths relative to the base folder
                files = [f.strip() for f in output.split("\n") if f.strip()]
                return files
        else:
            return []

    def should_create_remote_file(
        self, file_pattern: str = None, file_extension: str = None, folder: str = "/tmp", absolute_path: str = None
    ) -> "FluentAssertion":
        """Assert that a remote file exists.

        Can work in two modes:
        1. Search mode: Searches the specified folder recursively for files matching pattern/extension
        2. Absolute path mode: Check if a specific absolute file path exists

        Args:
            file_pattern: Glob pattern to match file names (e.g., "*.jfr", "heap-dump-*")
            file_extension: File extension to match (e.g., ".jfr", ".hprof")
            folder: Remote folder to check (default: "/tmp") - ignored if absolute_path is provided
            absolute_path: Absolute path to a specific file to check for existence
        """
        # If absolute_path is provided, check that specific file
        if absolute_path:
            cmd = (
                f'cf ssh {self.context.app_name} -c \'test -f "{absolute_path}" && echo "EXISTS" || echo "NOT_FOUND"\''
            )
            result = self.runner.run_command(cmd, app_name=self.context.app_name, timeout=15)

            if result.success and "EXISTS" in result.stdout:
                return self
            else:
                # Try to provide helpful debugging info
                parent_dir = "/".join(absolute_path.split("/")[:-1]) if "/" in absolute_path else "/"

                # List files in parent directory
                debug_cmd = (
                    f'cf ssh {self.context.app_name} -c \'ls -la "{parent_dir}" 2>/dev/null || '
                    'echo "DIRECTORY_NOT_FOUND"\''
                )
                debug_result = self.runner.run_command(debug_cmd, app_name=self.context.app_name, timeout=15)

                error_msg = f"Expected remote file '{absolute_path}' to exist, but it doesn't."

                if debug_result.success and "DIRECTORY_NOT_FOUND" not in debug_result.stdout:
                    files_in_dir = [line.strip() for line in debug_result.stdout.split("\n") if line.strip()]
                    error_msg += f"\nFiles in directory '{parent_dir}':\n"
                    for file_line in files_in_dir[:20]:  # Show first 20 files
                        error_msg += f"  {file_line}\n"
                    if len(files_in_dir) > 20:
                        error_msg += f"  ... and {len(files_in_dir) - 20} more files"
                else:
                    error_msg += f"\nParent directory '{parent_dir}' does not exist or is not accessible."

                raise AssertionError(error_msg)

        # Original search mode logic
        # Check if folder is supported
        all_folders = {"tmp": "/tmp", "home": "$HOME", "app": "$HOME/app"}
        if folder not in all_folders.values():
            raise ValueError(f"Unsupported folder '{folder}'. Supported folders: /tmp, $HOME, $HOME/app")

        # Get all files recursively from the specified folder
        all_files = self._get_recursive_files(folder)

        # Find matching files based on criteria
        matching_files = []

        for file_path in all_files:
            file_name = file_path.split("/")[-1]
            match = True

            # Check file pattern
            if file_pattern:
                import fnmatch

                if not fnmatch.fnmatch(file_name, file_pattern):
                    match = False

            # Check file extension
            if file_extension and not file_name.endswith(file_extension):
                match = False

            if match:
                matching_files.append(file_path)

        if not matching_files:
            # Search across all other folders recursively
            found_elsewhere = {}

            for search_folder in all_folders.values():
                if search_folder == folder:
                    continue  # Skip the folder we already searched

                search_files = self._get_recursive_files(search_folder)

                for file_path in search_files:
                    file_name = file_path.split("/")[-1]
                    match = True

                    # Apply same criteria checks
                    if file_pattern:
                        import fnmatch

                        if not fnmatch.fnmatch(file_name, file_pattern):
                            match = False

                    if file_extension and not file_name.endswith(file_extension):
                        match = False

                    if match:
                        if search_folder not in found_elsewhere:
                            found_elsewhere[search_folder] = []
                        found_elsewhere[search_folder].append(file_path)  # Store full path for subfolders

            # Build helpful error message
            criteria = []
            if file_pattern:
                criteria.append(f"pattern='{file_pattern}'")
            if file_extension:
                criteria.append(f"extension='{file_extension}'")

            error_msg = (
                f"Expected remote file matching criteria [{', '.join(criteria)}] in folder '{folder}'"
                " (searched recursively)"
            )

            if found_elsewhere:
                error_msg += ", but found matching files in other folders:\n"
                for other_folder, files in found_elsewhere.items():
                    error_msg += f"  {other_folder}: {files}\n"
                error_msg += f"Tip: Use folder='{list(found_elsewhere.keys())[0]}' to check the correct folder."
            else:
                # Show summary of what files exist
                total_files = len(all_files)
                if total_files > 0:
                    file_names = [f.split("/")[-1] for f in all_files]
                    if total_files <= 30:
                        error_msg += f", but found no matching files anywhere.\nFiles in {folder}: {file_names}"
                    else:
                        error_msg += (
                            f", but found no matching files anywhere.\nFiles in {folder}: "
                            f"{file_names[:30]}... (showing 30 of {total_files} files)"
                        )
                else:
                    error_msg += f", but found no files in {folder}."

                # Also show summary from other folders for debugging
                other_files_summary = []
                for search_folder in all_folders.values():
                    if search_folder != folder:
                        search_files = self._get_recursive_files(search_folder)
                        if search_files:
                            count = len(search_files)
                            other_files_summary.append(f"{search_folder}: {count} files")
                if other_files_summary:
                    error_msg += f"\nOther folders: {'; '.join(other_files_summary)}"

            raise AssertionError(error_msg)

        return self

    # JFR-specific assertions
    def jfr_should_have_events(self, event_type: str, min_count: int, file_pattern: str = None) -> "FluentAssertion":
        """Assert that JFR file contains minimum number of events."""
        if file_pattern is None:
            file_pattern = f"{self.context.app_name}-*.jfr"

        # Delegate to the core method to avoid code duplication
        from .core import FluentAssertions

        FluentAssertions.jfr_has_events(file_pattern, event_type, min_count)

        return self

    def should_contain_valid_thread_dump(self) -> "FluentAssertion":
        """Assert that output contains valid thread dump information."""
        from .file_validators import validate_thread_dump_output

        try:
            validate_thread_dump_output(self.result.output)
        except Exception as e:
            raise AssertionError(f"Thread dump validation failed: {e}")
        return self

    def should_contain_help(self) -> "FluentAssertion":
        """Assert that output contains help/usage information."""
        output = self.result.output

        # Check for common help patterns
        help_indicators = [
            "NAME:",
            "USAGE:",
            "DESCRIPTION:",
            "OPTIONS:",
            "EXAMPLES:",
            "--help",
            "Usage:",
            "Commands:",
            "Flags:",
            "Arguments:",
        ]

        found_indicators = [indicator for indicator in help_indicators if indicator in output]

        if len(found_indicators) < 2:
            raise AssertionError(
                f"Output does not appear to contain help information. "
                f"Expected at least 2 help indicators, found {len(found_indicators)}: {found_indicators}. "
                f"Output: {output[:200]}..."
            )

        return self

    def should_contain_vitals(self) -> "FluentAssertion":
        """Assert that output contains VM vitals information in the expected format."""
        output = self.result.output.strip()

        # Check that output starts with "Vitals:"
        if not output.startswith("Vitals:"):
            raise AssertionError(f"VM vitals output should start with 'Vitals:', but starts with: {output[:50]}...")

        # Check for system section header
        if "------------system------------" not in output:
            raise AssertionError("VM vitals output should contain '------------system------------' section header")

        # Check for key vitals metrics
        required_metrics = [
            "avail: Memory available without swapping",
            "comm: Committed memory",
            "crt: Committed-to-Commit-Limit ratio",
            "swap: Swap space used",
            "si: Number of pages swapped in",
            "so: Number of pages pages swapped out",
            "p: Number of processes",
        ]

        missing_metrics = [metric for metric in required_metrics if metric not in output]
        if missing_metrics:
            raise AssertionError(f"VM vitals output missing required metrics: {missing_metrics}")

        # Check for "Last 60 minutes:" section
        if "Last 60 minutes:" not in output:
            raise AssertionError("VM vitals output should contain 'Last 60 minutes:' section")

        return self

    def should_contain_vm_info(self) -> "FluentAssertion":
        """Assert that output contains VM info information in the expected format."""
        output = self.result.output

        # Check for JRE version line with OpenJDK Runtime Environment SapMachine
        jre_pattern = r"#\s*JRE version:.*OpenJDK Runtime Environment.*SapMachine"
        if not re.search(jre_pattern, output, re.IGNORECASE):
            raise AssertionError(
                "VM info output should contain JRE version line with 'OpenJDK Runtime Environment SapMachine'. "
                f"Expected pattern: '{jre_pattern}'"
            )

        # Check for SUMMARY section header
        if "---------------  S U M M A R Y ------------" not in output:
            raise AssertionError(
                "VM info output should contain '---------------  S U M M A R Y ------------' section header"
            )

        # Check for PROCESS section header
        if "---------------  P R O C E S S  ---------------" not in output:
            raise AssertionError(
                "VM info output should contain '---------------  P R O C E S S  ---------------' section header"
            )

        return self

    def no_files(self) -> "FluentAssertion":
        """Assert that no local files were created.

        This is a convenience method for commands that should not create any local files.
        It does NOT check remote files since many commands don't affect remote file state.
        """
        self.should_create_no_files()
        return self


class CFJavaTest:
    """Main DSL entry point for CF Java Plugin testing."""

    def __init__(self, runner: CFJavaTestRunner, context: TestContext, test_name: str = None):
        self.runner = runner
        self.context = context
        self.test_name = test_name

    def run(self, command: str) -> FluentAssertion:
        """Execute a CF Java command and return assertion object with remote state capture."""
        # Capture remote file state before command execution
        before_state = self.runner.capture_remote_file_state(self.context.app_name)

        # Execute the command
        result = self.runner.run_command(f"cf java {command}", app_name=self.context.app_name)

        # Create assertion with all context
        assertion = FluentAssertion(result, self.context, self.runner)
        assertion._test_name = self.test_name
        assertion._command_name = command.split()[0] if command else "unknown"
        assertion._remote_files_before = before_state

        return assertion


# Factory function for creating test DSL with test name
def test_cf_java(runner: CFJavaTestRunner, context: TestContext, test_name: str = None) -> CFJavaTest:
    """Create a test DSL instance with optional test name for snapshot tracking."""
    return CFJavaTest(runner, context, test_name)
