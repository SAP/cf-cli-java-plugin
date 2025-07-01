"""
JFR (Java Flight Recorder) tests.

Run with:
    pytest test_disk_full.py -v                           # All JFR tests
"""

import time

from framework.decorators import test
from framework.runner import TestBase, get_test_session


class DiskFullContext:
    """Fills the disk to the brim for testing purposes."""

    def __init__(self, app):
        self.app = app
        self.runner = get_test_session().runner

    def __enter__(self):
        # well dd doesn't work, so we use good
        self.runner.run_command(f"cf ssh {self.app} -c 'yes >> $HOME/fill_disk.txt'")
        return self

    def __exit__(self, exc_type, exc_value, traceback):
        # Clean up the dummy data
        self.runner.run_command(f"cf ssh {self.app} -c 'rm $HOME/fill_disk.txt'")


class TestDiskFull(TestBase):
    """Tests for disk full scenarios."""

    @test("all", no_restart=True)
    def test_heap_dump(self, t, app):
        """Test JFR functionality with disk full simulation."""
        with DiskFullContext(app):
            t.run(f"heap-dump {app}").should_fail().should_contain("No space left on device").no_files()

    @test("all", no_restart=True)
    def test_jfr(self, t, app):
        """Test JFR start with disk full simulation."""
        with DiskFullContext(app):
            t.run(f"jfr-start {app}")
            time.sleep(2)
            t.run(f"jfr-stop {app}").should_fail().no_files()

    @test("sapmachine21", no_restart=True)
    def test_asprofile(self, t, app):
        """Test ASProfile with disk full simulation."""
        with DiskFullContext(app):
            t.run(f"asprof-start-wall {app}")
            time.sleep(2)
            t.run(f"asprof-stop {app}").should_fail().no_files()
