"""
Async-profiler tests (most are SapMachine only).
"""

import time

from framework.decorators import test
from framework.runner import TestBase


class TestAsprofBasic(TestBase):
    """Basic async-profiler functionality."""

    # @test(ine11", no_restart=True)
    # def test_asprof_not_present(self, t, app):
    #    """Test that async-profiler is not present in JDK 21."""
    #    t.run(f"asprof {app} --args 'status'").should_fail().should_contain("not found")

    @test(no_restart=True)
    def test_status_no_profiling(self, t, app):
        """Test asprof status when no profiling is active."""
        t.run(f"asprof-status {app}").should_succeed()

    @test(no_restart=True)
    def test_start_provides_stop_instruction(self, t, app):
        """Test that asprof-start provides clear stop instructions."""
        t.run(f"asprof-start-cpu {app}").should_succeed().should_contain(f"Use 'cf java asprof-stop {app}'")

        # Clean up
        t.run(f"asprof-stop {app} --no-download").should_succeed().should_create_remote_file(
            "*.jfr"
        ).should_not_create_file()

    @test(no_restart=True)
    def test_basic_profile(self, t, app):
        """Test basic async-profiler profile start and stop."""
        # Start profiling
        t.run(f"asprof-start-cpu {app}").should_succeed().should_contain(f"Use 'cf java asprof-stop {app}'").no_files()

        # Clean up
        t.run(f"asprof-stop {app}").should_succeed().should_create_file("*.jfr").should_create_no_remote_files()

    @test(no_restart=True)
    def test_dry_run_commands(self, t, app):
        """Test async-profiler commands dry run functionality."""
        commands = [
            "asprof-start-wall",
            "asprof-start-cpu",
            "asprof-start-alloc",
            "asprof-start-lock",
            "asprof-status",
            "asprof-stop",
        ]

        for cmd in commands:
            t.run(f"{cmd} {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test(no_restart=True)
    def test_asprof_error_handling(self, t, app):
        """Test error messages for invalid flags."""
        t.run(f"asprof-start-cpu {app} --invalid-flag").should_fail().no_files().should_contain("invalid")


class TestAsprofProfiles(TestBase):
    """Different async-profiler profiling modes."""

    @test(no_restart=True)
    def test_cpu_profiling(self, t, app):
        """Test CPU profiling with async-profiler."""
        # Start CPU profiling
        t.run(f"asprof-start-cpu {app}").should_succeed().should_contain(f"Use 'cf java asprof-stop {app}'").no_files()

        # Check status shows profiling is active
        t.run(f"asprof-status {app}").should_succeed().no_files().should_match("Profiling is running for")

        # Wait for profiling data
        time.sleep(1)

        # Stop and verify JFR file contains execution samples
        t.run(f"asprof-stop {app}").should_succeed().should_create_file(f"{app}-asprof-*.jfr").jfr_should_have_events(
            "jdk.NativeLibrary", 10
        )

    @test(no_restart=True)
    def test_wall_clock_profiling(self, t, app):
        """Test wall-clock profiling mode."""
        t.run(f"asprof-start-wall {app}").should_succeed()

        time.sleep(1)

        t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(f"{app}-asprof-*.jfr")

    @test(no_restart=True)
    def test_allocation_profiling(self, t, app):
        """Test allocation profiling mode."""
        t.run(f"asprof-start-alloc {app}").should_succeed()

        time.sleep(1)

        t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(f"{app}-asprof-*.jfr")

    @test(no_restart=True)
    def test_allocation_profiling_dry_run(self, t, app):
        """Test allocation profiling dry run."""
        # This should not create any files, just show the command
        t.run(f"asprof-start-alloc {app} --dry-run").should_succeed().should_contain("-e alloc").no_files()
        t.run(f"asprof-status {app}").should_succeed().no_files().should_contain("Profiler is not active")
        t.run(f"asprof-stop {app}").should_succeed()

    @test(no_restart=True)
    def test_lock_profiling(self, t, app):
        """Test lock profiling mode."""
        t.run(f"asprof-start-lock {app}").should_succeed()

        time.sleep(1)

        t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(f"{app}-asprof-*.jfr")


class TestAsprofAdvanced(TestBase):
    """Advanced async-profiler scenarios."""

    @test(no_restart=True)
    def test_stop_without_download(self, t, app):
        """Test stopping profiling without downloading results."""
        # Start profiling
        t.run(f"asprof-start-cpu {app}").should_succeed()

        time.sleep(1)

        # Stop without download
        t.run(f"asprof-stop {app} --no-download").should_succeed().should_not_create_file("*.jfr")

    @test(no_restart=True)
    def test_keep_remote_file(self, t, app):
        """Test keeping profiling file on remote after download."""
        # Start profiling
        t.run(f"asprof-start-cpu {app}").should_succeed()

        time.sleep(1)

        # Stop with keep flag
        t.run(f"asprof-stop {app} --local-dir . --keep").should_succeed().should_create_file(
            f"{app}-asprof-*.jfr"
        ).should_create_remote_file(file_extension=".jfr")

    @test(no_restart=True)
    def test_workflow_with_multiple_checks(self, t, app):
        """Test complete workflow with comprehensive checks."""
        # Test each step of the profiling workflow

        # Start profiling - check success and basic output
        t.run(f"asprof-start-cpu {app}").should_succeed().should_contain("Profiling started").no_files()

        time.sleep(1)

        # Check status - verify profiling is active
        t.run(f"asprof-status {app}").should_succeed().should_contain("Profiling is running for").no_files()

        # Stop profiling - check completion message
        t.run(f"asprof-stop {app} --no-download").should_succeed().should_contain(
            "--- Execution profile ---"
        ).should_create_no_files().should_create_remote_file("*.jfr")


class TestAsprofLifecycle(TestBase):
    """Complete async-profiler workflow tests."""

    @test(no_restart=True)
    def test_full_cpu_profiling_workflow(self, t, app):
        """Test complete CPU profiling workflow with validation."""
        # 1. Verify no profiling initially
        t.run(f"asprof-status {app}").should_succeed()

        # 2. Start CPU profiling
        t.run(f"asprof-start-cpu {app}").should_succeed().should_contain("asprof-stop").no_files()

        # 3. Verify profiling is active
        t.run(f"asprof-status {app}").should_succeed().no_files().should_contain("Profiling is running for")

        # 4. Let it run for enough time to collect data
        time.sleep(2)

        # 5. Stop and download with validation
        t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(
            f"{app}-asprof-*.jfr"
        ).jfr_should_have_events("jdk.NativeLibrary", 5)

        # 6. Verify profiling has stopped
        t.run(f"asprof-status {app}").should_succeed().no_files().should_contain("Profiler is not active")

    @test(no_restart=True)
    def test_multiple_profiling_sessions(self, t, app):
        """Test running multiple profiling sessions in sequence."""
        profiling_modes = ["cpu", "wall", "alloc"]

        for mode in profiling_modes:
            # Start profiling
            t.run(f"asprof-start-{mode} {app}").should_succeed()

            time.sleep(1)

            # Stop and verify file creation
            t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(f"{app}-asprof-*.jfr")


class TestAsprofCommand(TestBase):
    """Tests for the general asprof command with --args (distinct from asprof-* commands)."""

    @test(no_restart=True)
    def test_asprof_help_command(self, t, app):
        """Test asprof help command via --args."""
        t.run(f"asprof {app} --args '--help'").should_succeed().should_contain("profiler").no_files()

    @test(no_restart=True)
    def test_asprof_version_command(self, t, app):
        """Test asprof version command."""
        t.run(f"asprof {app} --args '--version'").should_succeed().should_start_with("Async-profiler ").no_files()

    @test(no_restart=True)
    def test_asprof_status_via_args(self, t, app):
        """Test asprof status via --args (different from asprof-status command)."""
        t.run(f"asprof {app} --args 'status'").should_succeed().no_files().should_contain("Profiler is not active")

    @test(no_restart=True)
    def test_asprof_start_auto_no_download(self, t, app):
        """Test that asprof start commands automatically set no-download."""
        t.run(f"asprof {app} --args 'start -e cpu -f /tmp/asprof/bla.jfr'").should_succeed().no_files()

        time.sleep(1)

        t.run(f"asprof {app} --args 'stop'").should_succeed().should_create_file(
            "*.jfr"
        ).should_create_no_remote_files()

    @test(no_restart=True)
    def test_asprof_with_custom_output_file(self, t, app):
        """Test asprof with custom output file using @FSPATH."""
        # Start profiling with custom file in the asprof folder
        t.run(f"asprof {app} --args 'start -e cpu -f @FSPATH/custom-profile.jfr'").should_succeed().no_files()

        time.sleep(1)

        # Stop and download
        t.run(f"asprof {app} --args 'stop' --local-dir .").should_succeed().should_create_file("custom-profile.jfr")

    @test(no_restart=True)
    def test_asprof_collect_multiple_files(self, t, app):
        """Test that asprof collects multiple files from the asprof folder."""
        # Create multiple files via asprof
        t.run(f"asprof {app} --args 'start -e cpu -f @FSPATH/cpu.jfr'").should_succeed()
        time.sleep(1)
        t.run(f"asprof {app} --args 'stop'").should_succeed()

        # Start another profiling session with different file
        t.run(f"asprof {app} --args 'start -e alloc -f @FSPATH/alloc.jfr'").should_succeed()
        time.sleep(1)
        t.run(f"asprof {app} --args 'stop' --local-dir .").should_succeed().should_create_file(
            "cpu.jfr"
        ).should_create_file("alloc.jfr")

    @test()
    def test_asprof_keep_remote_files(self, t, app):
        """Test keeping remote files with asprof."""
        # Generate a file and keep it
        t.run(f"asprof {app} --args 'start -e cpu -f @FSPATH/keep-test.jfr'").should_succeed()
        time.sleep(1)
        t.run(f"asprof {app} --args 'stop' --keep --local-dir .").should_succeed().should_create_file(
            "keep-test.jfr"
        ).should_create_remote_file("keep-test.jfr")

    @test(no_restart=True)
    def test_asprof_invalid_args_flag_for_non_args_commands(self, t, app):
        """Test that --args flag is rejected for commands that don't support it."""
        # asprof-start commands don't use @ARGS, so --args should be rejected
        t.run(f"asprof-start-cpu {app} --args 'test'").should_fail().should_contain(
            "not supported for asprof-start-cpu"
        )


class TestAsprofEdgeCases(TestBase):
    """Edge cases and error conditions for async-profiler."""

    @test()
    def test_asprof_start_commands_file_flags_validation(self, t, app):
        """Test that asprof-start commands reject inappropriate file flags."""
        # asprof-start commands have GenerateFiles=false, so some file flags should be rejected
        for flag in ["--keep", "--no-download"]:
            t.run(f"asprof-start-cpu {app} {flag}").should_fail().should_contain("not supported for asprof-start-cpu")

    # @test()
    # def test_asprof_stop_requires_prior_start(self, t, app):
    #    """Test asprof-stop behavior when no profiling is active."""
    #    t.run(f"asprof-stop {app}").should_fail().should_contain("[ERROR] Profiler has not started").no_files()

    @test()
    def test_asprof_different_event_types(self, t, app):
        """Test CPU event type via asprof command."""
        # Test CPU event type
        t.run(f"asprof {app} --args 'start -e cpu'").should_succeed()
        time.sleep(0.5)
        t.run(f"asprof {app} --args 'stop'").should_succeed()

    @test(no_restart=True)
    def test_asprof_output_formats(self, t, app):
        """Test JFR output format with asprof."""
        t.run(f"asprof {app} --args 'start -e cpu -o jfr -f @FSPATH/profile.jfr'").should_succeed()
        time.sleep(0.5)
        t.run(f"asprof {app} --args 'stop' --local-dir .").should_succeed().should_create_file("profile.jfr")

    @test(no_restart=True)
    def test_asprof_recursive_args_validation(self, t, app):
        """Test that @ARGS cannot contain itself in asprof."""
        # This should fail due to the validation in replaceVariables
        t.run(f"asprof {app} --args 'echo @ARGS'").should_fail()

    @test(no_restart=True)
    def test_asprof_profiling_duration_and_interval(self, t, app):
        """Test asprof with duration parameter."""
        # Test duration parameter
        t.run(f"asprof {app} --args 'start -e cpu -d 2 -f @FSPATH/duration.jfr'").should_succeed()
        time.sleep(3)  # Wait for profiling to complete
        t.run(f"asprof {app} --args 'status'").should_succeed()  # Should show no active profiling
        t.run(f"asprof {app} --args 'stop' --local-dir .").should_succeed().should_create_file("duration.jfr")

    @test(no_restart=True)
    def test_asprof_list_command(self, t, app):
        # List should show available files
        t.run(f"asprof {app} --args 'list'").should_succeed().no_files().should_contain("Basic events:")


class TestAsprofAdvancedFeatures(TestBase):
    """Advanced async-profiler features and workflows."""

    @test(no_restart=True)
    def test_asprof_flamegraph_generation(self, t, app):
        """Test flamegraph generation with asprof."""
        t.run(f"asprof {app} --args 'start -e cpu'").should_succeed()
        time.sleep(1)

        # Generate flamegraph directly
        t.run(
            f"asprof {app} --args 'stop -o collapsed -f @FSPATH/flamegraph.html' --local-dir ."
        ).should_succeed().should_create_file("flamegraph.html")
