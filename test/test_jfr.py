"""
JFR (Java Flight Recorder) tests.

Run with:
    pytest test_jfr.py -v                           # All JFR tests
    pytest test_jfr.py::TestJFRBasic -v            # Basic JFR functionality
    pytest test_jfr.py::TestJFRProfiles -v         # Profile-specific tests
    pytest test_jfr.py::TestJFRLifecycle -v        # Complete workflows
"""

import time

from framework.decorators import test
from framework.runner import TestBase


class TestJFRBasic(TestBase):
    """Basic JFR functionality tests."""

    @test()
    def test_status_no_recording(self, t, app):
        """Test JFR status when no recording is active."""
        t.run(f"jfr-status {app}").should_succeed().should_match(
            r"No available recordings\.\s*Use jcmd \d+ JFR\.start to start a recording\."
        ).no_files()

    @test()
    def test_status_with_active_recording(self, t, app):
        """Test JFR status shows active recording information."""
        # Start recording
        t.run(f"jfr-start {app}").should_succeed().no_files()

        # Check status shows recording
        t.run(f"jfr-status {app}").should_succeed().should_contain("name=JFR maxsize=250.0MB (running)").no_files()

        # Clean up
        t.run(f"jfr-stop {app} --no-download").should_succeed().should_create_remote_file(
            "*.jfr"
        ).should_create_no_files()

    @test()
    def test_jfr_dump(self, t, app):
        """Test JFR dump functionality."""
        # Start recording
        t.run(f"jfr-start {app}").should_succeed().should_create_remote_file("*.jfr").should_create_no_files()

        # Wait a bit to ensure recording has data
        time.sleep(2)

        # Dump the recording
        t.run(f"jfr-dump {app}").should_succeed().should_create_file("*.jfr").should_create_no_remote_files()

        t.run(f"jfr-status {app}").should_succeed().should_contain("Recording ").no_files()

        # Clean up
        t.run(f"jfr-stop {app} --no-download").should_succeed().should_create_remote_file(
            "*.jfr"
        ).should_create_no_files()

    @test()
    def test_concurrent_recordings_prevention(self, t, app):
        """Test that concurrent JFR recordings are prevented."""
        # Start first recording
        t.run(f"jfr-start {app}").should_succeed().should_contain(f"Use 'cf java jfr-stop {app}'")

        # Attempt to start second recording should fail
        t.run(f"jfr-start {app}").should_fail().should_contain("JFR recording already running")

        # Clean up - stop the first recording
        t.run(f"jfr-stop {app} --no-download").should_succeed()

    @test()
    def test_gc_profile(self, t, app):
        """Test JFR GC profile (SapMachine only)."""
        t.run(f"jfr-start-gc {app}").should_succeed().no_files()
        t.run(f"jfr-stop {app} --no-download").should_succeed().should_create_remote_file("*.jfr")

    @test()
    def test_gc_details_profile(self, t, app):
        """Test JFR detailed GC profile (SapMachine only)."""
        t.run(f"jfr-start-gc-details {app}").should_succeed().no_files()
        t.run(f"jfr-stop {app}").should_succeed().should_create_no_remote_files().should_create_file("*.jfr")


if __name__ == "__main__":
    import pytest

    pytest.main([__file__, "-v", "--tb=short"])
