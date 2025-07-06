"""
JRE21 tests - Testing that JRE (without JDK tools) properly fails for all commands.

A JRE doesn't include development tools like jcmd, jmap, jstack, etc.
All commands should fail with appropriate error messages.

Run with:
    pytest test_jre21.py -v                           # All JRE21 tests
    ./test.py run jre21                                # Run JRE21 tests
"""

from framework.decorators import test
from framework.runner import TestBase


class TestJRE21CommandFailures(TestBase):
    """Test that JRE21 app fails for all commands requiring JDK tools."""

    @test("jre21", no_restart=True)
    def test_heap_dump_fails(self, t, app):
        """Test that heap-dump fails on JRE21."""
        t.run(f"heap-dump {app}").should_fail().should_contain("jvmmon or jmap are required for generating heap dump")

    @test("jre21", no_restart=True)
    def test_thread_dump_fails(self, t, app):
        """Test that thread-dump fails on JRE21."""
        t.run(f"thread-dump {app}").should_fail().should_contain("jvmmon or jmap are required for")

    @test("jre21", no_restart=True)
    def test_vm_info_fails(self, t, app):
        """Test that vm-info fails on JRE21."""
        t.run(f"vm-info {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_vm_vitals_fails(self, t, app):
        """Test that vm-vitals fails on JRE21."""
        t.run(f"vm-vitals {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_vm_version_fails(self, t, app):
        """Test that vm-version fails on JRE21."""
        t.run(f"vm-version {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_jcmd_fails(self, t, app):
        """Test that jcmd fails on JRE21."""
        t.run(f"jcmd {app} --args 'help'").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_jfr_start_fails(self, t, app):
        """Test that jfr-start fails on JRE21."""
        t.run(f"jfr-start {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_jfr_stop_fails(self, t, app):
        """Test that jfr-stop fails on JRE21."""
        t.run(f"jfr-stop {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_jfr_status_fails(self, t, app):
        """Test that jfr-status fails on JRE21."""
        t.run(f"jfr-status {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_jfr_dump_fails(self, t, app):
        """Test that jfr-dump fails on JRE21."""
        t.run(f"jfr-dump {app}").should_fail().should_contain("jcmd not found")

    @test("jre21", no_restart=True)
    def test_asprof_start_fails(self, t, app):
        """Test that asprof-start-cpu fails on JRE21."""
        t.run(f"asprof-start-cpu {app}").should_fail().should_contain("asprof not found")

    @test("jre21", no_restart=True)
    def test_asprof_status_fails(self, t, app):
        """Test that asprof-status fails on JRE21."""
        t.run(f"asprof-status {app}").should_fail().should_contain("asprof not found")

    @test("jre21", no_restart=True)
    def test_asprof_stop_fails(self, t, app):
        """Test that asprof-stop fails on JRE21."""
        t.run(f"asprof-stop {app}").should_fail().should_contain("asprof not found")

    @test("jre21", no_restart=True)
    def test_asprof_command_fails(self, t, app):
        """Test that asprof command fails on JRE21."""
        t.run(f"asprof {app} --args 'help'").should_fail().should_contain("asprof not found")


class TestJRE21DryRun(TestBase):
    """Test that dry-run commands work on JRE21 (they don't actually execute)."""

    @test("jre21", no_restart=True)
    def test_heap_dump_dry_run_works(self, t, app):
        """Test that heap-dump dry-run works on JRE21."""
        t.run(f"heap-dump {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test("jre21", no_restart=True)
    def test_thread_dump_dry_run_works(self, t, app):
        """Test that thread-dump dry-run works on JRE21."""
        t.run(f"thread-dump {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test("jre21", no_restart=True)
    def test_vm_info_dry_run_works(self, t, app):
        """Test that vm-info dry-run works on JRE21."""
        t.run(f"vm-info {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test("jre21", no_restart=True)
    def test_jcmd_dry_run_works(self, t, app):
        """Test that jcmd dry-run works on JRE21."""
        t.run(f"jcmd {app} --args 'help' --dry-run").should_succeed().should_contain("cf ssh").no_files()

    @test("jre21", no_restart=True)
    def test_jfr_start_dry_run_works(self, t, app):
        """Test that jfr-start dry-run works on JRE21."""
        t.run(f"jfr-start {app} --dry-run").should_succeed().should_contain("cf ssh").no_files()


class TestJRE21Help(TestBase):
    """Test that help commands work on JRE21."""

    @test("jre21", no_restart=True)
    def test_heap_dump_help(self, t, app):
        """Test that heap-dump help works on JRE21."""
        t.run(f"heap-dump {app} --help").should_succeed().should_contain_help()

    @test("jre21", no_restart=True)
    def test_thread_dump_help(self, t, app):
        """Test that thread-dump help works on JRE21."""
        t.run(f"thread-dump {app} --help").should_succeed().should_contain_help()

    @test("jre21", no_restart=True)
    def test_vm_info_help(self, t, app):
        """Test that vm-info help works on JRE21."""
        t.run(f"vm-info {app} --help").should_succeed().should_contain_help()

    @test("jre21", no_restart=True)
    def test_jcmd_help(self, t, app):
        """Test that jcmd help works on JRE21."""
        t.run(f"jcmd {app} --help").should_succeed().should_contain_help()

    @test("jre21", no_restart=True)
    def test_jfr_start_help(self, t, app):
        """Test that jfr-start help works on JRE21."""
        t.run(f"jfr-start {app} --help").should_succeed().should_contain_help()


if __name__ == "__main__":
    import pytest

    pytest.main([__file__, "-v", "--tb=short"])
