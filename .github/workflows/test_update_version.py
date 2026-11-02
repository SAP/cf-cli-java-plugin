"""Regression tests for the release-version script (no CF connection needed)."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("update_version.py")
GO_SOURCE = """package main
Version: plugin.VersionType{
    Major: 4,
    Minor: 0,
    Build: 3,
},
"""
CHANGELOG = """# Changelog

## [Unreleased]

### Added

- New feature

## [4.0.3]

### Fixed

- Previous fix

## [4.0.2]

- Older fix
"""


class UpdateVersionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.go_path = self.root / "cf_cli_java_plugin.go"
        self.changelog_path = self.root / "CHANGELOG.md"
        self.go_path.write_text(GO_SOURCE, encoding="utf-8")
        self.changelog_path.write_text(CHANGELOG, encoding="utf-8")
        (self.root / "README.md").write_text(
            "# Plugin\n\n## Changelog\n\nSee CHANGELOG.md.\n",
            encoding="utf-8",
        )

    def run_script(self, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), *args],
            cwd=self.root,
            env={**os.environ, "JSTALL_VERSION": "v-test"},
            capture_output=True,
            text=True,
        )

    def test_promotes_unreleased_and_creates_new_empty_section(self):
        result = self.run_script("4", "0", "4")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.changelog_path.read_text(encoding="utf-8"),
            CHANGELOG.replace("## [Unreleased]\n", "## [Unreleased]\n\n## [4.0.4]\n", 1),
        )
        self.assertIn("Build: 4,", self.go_path.read_text(encoding="utf-8"))
        self.assertEqual(
            (self.root / "release_changelog.txt").read_text(encoding="utf-8"),
            "### Added\n\n- New feature\n",
        )
        notes = (self.root / "release_notes.md").read_text(encoding="utf-8")
        self.assertIn("## CF CLI Java Plugin 4.0.4", notes)
        self.assertIn("- New feature", notes)
        self.assertIn("[v-test]", notes)
        self.assertIn("/cf-cli-java-plugin-windows-amd64.exe", notes)
        self.assertIn("/cf-cli-java-plugin-linux-arm64", notes)

    def test_uses_existing_version_without_changing_changelog(self):
        result = self.run_script("4", "0", "3")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), CHANGELOG)
        self.assertEqual(
            (self.root / "release_changelog.txt").read_text(encoding="utf-8"),
            "### Fixed\n\n- Previous fix\n",
        )

    def test_release_notes_use_repository_running_workflow(self):
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--release-notes-only", "4.0.3"],
            cwd=self.root,
            env={**os.environ, "JSTALL_VERSION": "v-test", "GITHUB_REPOSITORY": "parttimenerd/cf-cli-java-plugin"},
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        notes = (self.root / "release_notes.md").read_text(encoding="utf-8")
        self.assertIn("https://github.com/parttimenerd/cf-cli-java-plugin/releases/download/4.0.3/", notes)
        self.assertIn("https://github.com/parttimenerd/cf-cli-java-plugin/releases/latest/download/", notes)

    def test_promoted_version_is_reused_on_rerun(self):
        self.assertEqual(self.run_script("4", "0", "4").returncode, 0)
        promoted = self.changelog_path.read_text(encoding="utf-8")
        self.assertEqual(self.run_script("4", "0", "4").returncode, 0)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), promoted)
        self.assertEqual(promoted.count("## [4.0.4]"), 1)

    def test_rc_reads_existing_base_version_without_source_changes(self):
        result = self.run_script("4.0.3-rc2", "0", "0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.go_path.read_text(encoding="utf-8"), GO_SOURCE)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), CHANGELOG)
        self.assertEqual(
            (self.root / "release_changelog.txt").read_text(encoding="utf-8"),
            "### Fixed\n\n- Previous fix\n",
        )
        self.assertIn(
            "Plugin 4.0.3-rc2",
            (self.root / "release_notes.md").read_text(encoding="utf-8"),
        )

    def test_notes_only_reads_existing_section_without_source_changes(self):
        result = self.run_script("--release-notes-only", "4.0.3")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.go_path.read_text(encoding="utf-8"), GO_SOURCE)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), CHANGELOG)
        self.assertIn(
            "- Previous fix",
            (self.root / "release_notes.md").read_text(encoding="utf-8"),
        )

    def test_missing_unreleased_does_not_change_go_source(self):
        original = CHANGELOG.replace("## [Unreleased]\n\n### Added\n\n- New feature\n\n", "")
        self.changelog_path.write_text(original, encoding="utf-8")
        result = self.run_script("4", "0", "4")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unreleased", result.stderr)
        self.assertEqual(self.go_path.read_text(encoding="utf-8"), GO_SOURCE)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), original)

    def test_empty_unreleased_does_not_change_go_source(self):
        original = CHANGELOG.replace("### Added\n\n- New feature\n\n", "")
        self.changelog_path.write_text(original, encoding="utf-8")
        result = self.run_script("4", "0", "4")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.go_path.read_text(encoding="utf-8"), GO_SOURCE)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), original)

    def test_missing_version_in_notes_only_does_not_create_artifacts(self):
        result = self.run_script("--release-notes-only", "4.0.4")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), CHANGELOG)
        self.assertFalse((self.root / "release_notes.md").exists())

    def test_duplicate_version_headings_are_rejected(self):
        original = CHANGELOG + "\n## [4.0.3]\n\n- Duplicate\n"
        self.changelog_path.write_text(original, encoding="utf-8")
        result = self.run_script("--release-notes-only", "4.0.3")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("4.0.3", result.stderr)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), original)

    def test_missing_go_version_pattern_does_not_change_changelog(self):
        self.go_path.write_text("package main\n", encoding="utf-8")
        result = self.run_script("4", "0", "4")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.changelog_path.read_text(encoding="utf-8"), CHANGELOG)


if __name__ == "__main__":
    unittest.main()
