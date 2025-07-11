"""
Basic CF Java Plugin command tests.

Run with:
    pytest test_basic_commands.py -v                    # All basic commands
    pytest test_basic_commands.py::TestHeapDump -v     # Only heap dump tests
    pytest test_basic_commands.py::TestVMCommands -v   # Only VM commands

    ./test.py basic # Run all basic tests
    ./test.py run heap-dump # Run only heap dump tests
    # ...
"""

import os
import shutil
import tempfile

from framework.decorators import test
from framework.runner import TestBase


class TestHeapDump(TestBase):
    """Test suite for heap dump functionality."""

    @test(no_restart=True)
    def test_basic_download(self, t, app):
        """Test basic heap dump with local download."""
        t.run(f"heap-dump {app}").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        ).should_create_no_remote_files()

    @test(no_restart=True)
    def test_keep_remote_file(self, t, app):
        """Test heap dump with --keep flag to preserve remote file."""
        t.run(f"heap-dump {app} --keep").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        ).should_create_remote_file(
            "*.hprof"
        )  # Just look for any .hprof file

    @test(no_restart=True)
    def test_no_download(self, t, app):
        """Test heap dump without downloading - file stays remote."""
        t.run(f"heap-dump {app} --no-download").should_succeed().should_create_no_files().should_create_remote_file(
            "*.hprof"
        ).should_contain("Successfully created heap dump").should_contain("No download requested")

    @test(no_restart=True)
    def test_custom_container_dir(self, t, app):
        """Test heap dump with custom container directory."""
        t.run(f"heap-dump {app} --container-dir /home/vcap/app").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        ).should_create_no_remote_files()

    @test(no_restart=True)
    def test_custom_local_dir(self, t, app):
        """Test heap dump with custom local directory."""
        # Create a temporary directory for the download
        temp_dir = tempfile.mkdtemp()
        try:
            t.run(f"heap-dump {app} --local-dir {temp_dir}").should_succeed()
            # Verify file exists in the custom directory
            import glob

            files = glob.glob(f"{temp_dir}/{app}-heapdump-*.hprof")
            assert len(files) > 0, f"No heap dump files found in {temp_dir}"
        finally:
            # Clean up
            shutil.rmtree(temp_dir, ignore_errors=True)

    @test(no_restart=True)
    def test_verbose_output(self, t, app):
        """Test heap dump with verbose output."""
        # Verbose output should contain more detailed information
        t.run(f"heap-dump {app} --verbose").should_succeed().should_contain("[VERBOSE]")

    @test(no_restart=True)
    def test_combined_flags(self, t, app):
        """Test heap dump with multiple flags combined."""
        t.run(f"heap-dump {app} --keep --local-dir . --verbose").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        ).should_create_remote_file("*.hprof")

    @test(no_restart=True)
    def test_dry_run(self, t, app):
        """Test heap dump dry run shows SSH command."""
        t.run(f"heap-dump {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test(no_restart=True)
    def test_dry_run_variable_replacement(self, t, app):
        """Test that @ variables are properly replaced in dry-run mode."""
        result = t.run(f"heap-dump {app} --dry-run").should_succeed()
        # Ensure no @ variables remain in the output
        result.should_not_contain("@FSPATH")
        result.should_not_contain("@APP_NAME")
        result.should_not_contain("@FILE_NAME")
        result.should_not_contain("@ARGS")
        # Should contain the actual app name
        result.should_contain(app)

    @test(no_restart=True)
    def test_no_download_twice(self, t, app):
        """Test error handling when heap dump file already exists on remote."""
        t.run(f"heap-dump {app} --no-download").should_succeed()
        t.run(f"heap-dump {app} --no-download").should_succeed()

    @test(no_restart=True)
    def test_app_instance_selection(self, t, app):
        """Test heap dump with specific app instance index."""
        # Note: This test is valid even with a single instance app
        # as specifying index 0 should work the same as not specifying

        # Try with explicit instance 0 (should succeed even if only one instance)
        t.run(f"heap-dump {app} --app-instance-index 0 --local-dir .").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        )

    @test(no_restart=True)
    def test_heap_dump_shorthand_flags(self, t, app):
        """Test heap dump with shorthand flags."""
        # Test with shorthand flags -k (keep) and -ld (local-dir)
        t.run(f"heap-dump {app} -k -ld .").should_succeed().should_create_file(
            f"{app}-heapdump-*.hprof"
        ).should_create_remote_file("*.hprof")

    @test(no_restart=True)
    def test_invalid_flag(self, t, app):
        """Test heap dump with an invalid/unknown flag."""
        t.run(f"heap-dump {app} --not-a-real-flag").should_fail().should_contain(
            "Error while parsing command arguments: Invalid flag: --not-a-real-flag"
        )

    @test(no_restart=True)
    def test_help_output(self, t, app):
        """Test heap dump help/usage output."""
        # the help only works for the main command
        t.run(f"heap-dump {app} --help").should_succeed().should_contain_help()

    @test(no_restart=True)
    def test_nonexistent_local_dir(self, t, app):
        """Test heap dump with a non-existent local directory."""
        import uuid

        bad_dir = f"/tmp/does-not-exist-{uuid.uuid4()}"
        t.run(f"heap-dump {app} --local-dir {bad_dir}").should_fail().should_contain("Error creating local file at")

    @test(no_restart=True)
    def test_unwritable_local_dir(self, t, app):
        """Test heap dump with an unwritable local directory."""
        with tempfile.TemporaryDirectory() as temp_dir:
            os.chmod(temp_dir, 0o400)  # Read-only
            try:
                t.run(f"heap-dump {app} --local-dir {temp_dir}").should_fail().should_contain(
                    "Error creating local file at"
                )
            finally:
                os.chmod(temp_dir, 0o700)

    @test(no_restart=True)
    def test_negative_app_instance_index(self, t, app):
        # Test negative index
        t.run(f"heap-dump {app} --app-instance-index -1").should_fail().should_contain(
            "Invalid application instance index -1, must be >= 0"
        )

    @test(no_restart=True)
    def test_invalid_app_instance_index(self, t, app):
        t.run(f"heap-dump {app} --app-instance-index abc").should_fail().should_contain(
            "Error while parsing command arguments: Value for flag 'app-instance-index' must be an integer"
        )

    @test(no_restart=True)
    def test_wrong_app_instance_index(self, t, app):
        """Test heap dump with wrong app instance index."""
        t.run(f"heap-dump {app} --app-instance-index 1").should_fail().should_contain(
            "Command execution failed: The specified application instance does not exist"
        )


class TestGeneralCommands(TestBase):
    """Test suite for general command functionality."""

    @test(no_restart=True)
    def test_invalid_command_error(self, t, app):
        """Test that invalid commands fail with appropriate error message."""
        t.run("invalid-command-xyz").should_fail().should_contain(
            'Unrecognized command "invalid-command-xyz", did you mean:'
        ).no_files()


class TestThreadDump(TestBase):
    """Test suite for thread dump functionality."""

    @test(no_restart=True)
    def test_thread_dump_format(self, t, app):
        """Test thread dump output format with proper validation."""
        t.run(f"thread-dump {app}").should_succeed().should_contain_valid_thread_dump().should_contain(
            "http-nio-8080-Acceptor"
        ).no_files()

    @test(no_restart=True)
    def test_dry_run(self, t, app):
        """Test thread dump dry run shows SSH command."""
        t.run(f"thread-dump {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test(no_restart=True)
    def test_thread_dump_basic_success(self, t, app):
        """Test thread dump basic functionality."""
        t.run(f"thread-dump {app}").should_succeed().no_files().should_contain_valid_thread_dump()

    @test(no_restart=True)
    def test_thread_dump_keep_flag_error(self, t, app):
        """Test that thread-dump rejects --keep flag."""
        t.run(f"thread-dump {app} --keep").should_fail().should_contain("not supported for thread-dump")


class TestVMCommands(TestBase):
    """Test suite for VM information commands."""

    @test(no_restart=True)
    def test_vm_info_comprehensive(self, t, app):
        """Test VM info provides comprehensive system information."""
        t.run(f"vm-info {app}").should_succeed().should_contain_vm_info().should_have_at_least(1000, "lines").no_files()

    @test(no_restart=True)
    def test_vm_info_invalid_flag(self, t, app):
        """Test VM info with invalid flag."""
        t.run(f"vm-info {app} --not-a-real-flag").should_fail().should_contain(
            "Error while parsing command arguments: Invalid flag: --not-a-real-flag"
        )

    @test(no_restart=True)
    def test_vm_info_help_output(self, t, app):
        """Test VM info help/usage output."""
        t.run(f"vm-info {app} --help").should_succeed().should_contain_help()

    @test(no_restart=True)
    def test_vm_info_dry_run(self, t, app):
        """Test VM info dry run shows SSH command."""
        t.run(f"vm-info {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test(no_restart=True)
    def test_vm_vitals_dry_run(self, t, app):
        """Test VM vitals dry run shows SSH command."""
        t.run(f"vm-vitals {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test(no_restart=True)
    def test_vm_vitals_basic(self, t, app):
        """Test VM vitals provides vital statistics."""
        t.run(f"vm-vitals {app}").should_succeed().no_files()

    @test(no_restart=True)
    def test_vm_vitals_content(self, t, app):
        """Test VM vitals output contains expected vital statistics."""
        t.run(f"vm-vitals {app}").should_succeed().should_contain_vitals()

    @test(no_restart=True)
    def test_vm_version(self, t, app):
        """Test VM version output format validation."""
        t.run(f"vm-version {app}").should_succeed().should_contain(
            "OpenJDK 64-Bit Server VM version 21"
        ).should_contain("JDK").should_have_at_least(2, "lines")

    @test(no_restart=True)
    def test_vm_commands_with_file_flags(self, t, app):
        """Test that VM commands handle file-related flags appropriately."""
        # VM info should work with --dry-run
        t.run(f"vm-info {app} --dry-run").should_succeed().should_contain("cf ssh")

        # VM commands don't generate files, so --keep should either be ignored or error
        t.run(f"vm-info {app} --keep").should_fail()


class TestVariableReplacements(TestBase):
    """Test suite for variable replacements in commands."""

    @test(no_restart=True)
    def test_fspath_validation(self, t, app):
        """Test that FSPATH environment variable is properly set and usable."""
        # Run a command that uses FSPATH and verify it works
        t.run(
            f'jcmd {app} --args \'"FSPATH is: @FSPATH" && test -d "@FSPATH" &&'
            'echo "FSPATH directory exists"\' --dry-run'
        ).should_succeed().should_contain("FSPATH is: /tmp/jcmd").should_contain("FSPATH directory exists")

    @test(no_restart=True)
    def test_variable_replacement_functionality(self, t, app):
        """Test that variable replacements work correctly in dry-run mode."""
        # Use dry-run to see that variables are properly replaced
        (
            t.run(f"jcmd {app} --args 'echo test @FSPATH @APP_NAME' --dry-run")
            .should_succeed()
            .should_not_contain("@FSPATH")
            .should_not_contain("@ARGS")
            .should_not_contain("@APP_NAME")
        )

    @test(no_restart=True)
    def test_variable_replacement_with_disallowed_recursion(self, t, app):
        """Test that @-variables do not allow recursive replacements."""
        t.run(f"jcmd {app} --args 'echo @ARGS'").should_fail()


class TestJCmdCommands(TestBase):
    """Test suite for JCMD functionality."""

    @test(no_restart=True)
    def test_heap_dump_without_download(self, t, app):
        """Test JCMD heap dump without local download."""
        t.run(f"jcmd {app} --args 'GC.heap_dump my_dump.hprof'").should_succeed().should_create_remote_file(
            "my_dump.hprof", folder="$HOME/app"
        ).should_not_create_file()

    @test(no_restart=True)
    def test_heap_dump_with_fspath(self, t, app):
        """Test JCMD heap dump with local download using FSPATH."""
        t.run(f"jcmd {app} --args 'GC.heap_dump @FSPATH/my_dump.hprof'").should_succeed().should_create_file(
            "my_dump.hprof"
        )

    @test(no_restart=True)
    def test_heap_dump_absolute_path(self, t, app):
        """Test JCMD heap dump with absolute path (without using FSPATH)."""
        t.run(
            f"jcmd {app} --args 'GC.heap_dump /tmp/my_absolute_dump.hprof'"
        ).should_succeed().should_not_create_file().should_create_remote_file(
            absolute_path="/tmp/my_absolute_dump.hprof"
        )

    @test(no_restart=True)
    def test_heap_dump_no_download(self, t, app):
        """Test JCMD heap dump without download."""
        t.run(
            f"jcmd {app} --args 'GC.heap_dump @FSPATH/my_dump.hprof' --no-download"
        ).should_succeed().should_not_create_file("my_dump.hprof")

    @test(no_restart=True)  # VM uptime is read-only
    def test_vm_uptime(self, t, app):
        """Test JCMD VM uptime command."""
        t.run(f"jcmd {app} --args 'VM.uptime'").should_succeed().should_match(
            r"\d+\.\d+\s+s"
        )  # Should show uptime in seconds

    @test(no_restart=True)
    def test_relative_path_with_fspath(self, t, app):
        """Test JCMD with relative path combined with FSPATH."""
        t.run(
            f"jcmd {app} --args 'GC.heap_dump @FSPATH/../relative_dump.hprof'"
        ).should_succeed().should_not_create_file().should_create_remote_file("relative_dump.hprof")

    @test(no_restart=True)
    def test_jcmd_recursive_args_error(self, t, app):
        """Test that JCMD prevents recursive @ARGS usage."""
        t.run(f"jcmd {app} --args 'echo @ARGS'").should_fail()

    @test(no_restart=True)
    def test_jcmd_invalid_command_error(self, t, app):
        """Test that JCMD fails gracefully with an invalid command."""
        t.run(f"jcmd {app} --args 'invalid-command'").should_fail().should_contain(
            "java.lang.IllegalArgumentException: Unknown diagnostic command"
        )

    @test(no_restart=True)
    def test_sapmachine_uses_asprof_jcmd(self, t, app):
        """Test that SapMachine uses asprof-jcmd instead of regular jcmd."""
        (
            t.run(f"jcmd {app} --args 'help \\\"$JCMD_COMMAND\\\"'")
            .should_contain("asprof jcmd")
            .no_files()
            .should_succeed()
        )


if __name__ == "__main__":
    import pytest

    pytest.main([__file__, "-v", "--tb=short"])
