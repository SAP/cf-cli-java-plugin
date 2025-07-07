"""
Integration and cross-cutting tests for CF Java Plugin.

This file contains integration tests and scenarios that span multiple commands.
For focused command testing, see:
- test_basic_commands.py: Basic CF Java commands
- test_jfr.py: JFR functionality
- test_asprof.py: Async-profiler (SapMachine)

Run with:
    pytest test_cf_java_plugin.py -v               # Integration tests
    pytest test_cf_java_plugin.py::TestWorkflows -v # Complete workflows
"""

import time

from framework.decorators import test
from framework.runner import TestBase


class TestDryRunConsistency(TestBase):
    """Test that all commands support --dry-run consistently."""

    @test()
    def test_all_commands_support_dry_run(self, t, app):
        """Test that all major commands support --dry-run flag."""
        commands = [
            "heap-dump",
            "thread-dump",
            "vm-info",
            "vm-version",
            "vm-vitals",
            "jfr-start",
            "jfr-status",
            "jfr-stop",
            "jfr-dump",
            "asprof-start-wall",
            "asprof-start-cpu",
            "asprof-start-alloc",
            "asprof-start-lock",
            "asprof-status",
            "asprof-stop",
        ]

        for cmd in commands:
            t.run(f"{cmd} {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()


class TestWorkflows(TestBase):
    """Integration tests for complete workflows."""

    @test()
    def test_diagnostic_data_collection_workflow(self, t, app):
        """Test collecting comprehensive diagnostic data."""
        # 1. Collect VM information
        t.run(f"vm-info {app}").should_succeed().should_contain_vm_info()
        # 2. Get thread state
        t.run(f"thread-dump {app}").should_succeed().should_contain_valid_thread_dump()

        # 3. Capture memory state
        t.run(f"heap-dump {app} --local-dir .").should_succeed().should_create_file(f"{app}-heapdump-*.hprof")

        # 4. Start performance recording
        t.run(f"jfr-start {app}").should_succeed()

        time.sleep(2)

        # 5. Capture performance data
        t.run(f"jfr-stop {app} --local-dir .").should_succeed().should_create_file(f"{app}-jfr-*.jfr")

    @test()
    def test_performance_analysis_workflow(self, t, app):
        """Test performance analysis workflow with async-profiler."""
        # 1. Baseline: Get VM vitals
        t.run(f"vm-vitals {app}").should_succeed().should_contain_vitals()

        # 2. Start CPU profiling
        t.run(f"asprof-start-cpu {app}").should_succeed().no_files()

        # 3. Let application run under profiling
        time.sleep(2)

        # 4. Capture profiling data
        t.run(f"asprof-stop {app} --local-dir .").should_succeed().should_create_file(
            f"{app}-asprof-*.jfr"
        ).jfr_should_have_events("jdk.NativeLibrary", 5)

        # 5. Follow up with memory analysis
        t.run(f"heap-dump {app} --local-dir .").should_succeed().should_create_file(f"{app}-heapdump-*.hprof")

    @test()
    def test_concurrent_operations_safety(self, t, app):
        """Test that concurrent operations don't interfere."""
        # Start JFR recordingx
        t.run(f"jfr-start {app}").should_succeed()

        # Other operations should work while JFR is recording
        t.run(f"vm-info {app}").should_succeed().should_contain_vm_info()
        t.run(f"thread-dump {app}").should_succeed().should_contain_valid_thread_dump()
        t.run(f"vm-vitals {app}").should_succeed().should_contain_vitals()

        # JFR should still be recording
        t.run(f"jfr-status {app}").should_succeed().should_contain("name=JFR maxsize=250.0MB (running)")

        # Clean up
        t.run(f"jfr-stop {app} --no-download").should_succeed()


if __name__ == "__main__":
    import pytest

    pytest.main([__file__, "-v", "--tb=short"])
