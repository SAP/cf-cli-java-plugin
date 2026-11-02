"""Offline regression checks for release packaging and workflow wiring."""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github/workflows"


class ReleaseWorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = yaml.safe_load((WORKFLOWS / "release.yml").read_text(encoding="utf-8"))

    def steps(self, job):
        return self.workflow["jobs"][job]["steps"]

    def test_build_fetches_embedded_dependencies_before_linting_and_testing(self):
        steps = self.steps("build")
        prepare = next(i for i, step in enumerate(steps) if "build.py --deps-only" in step.get("run", ""))
        lint = next(i for i, step in enumerate(steps) if "golangci-lint" in step.get("name", ""))
        self.assertLess(prepare, lint)
        self.assertIn(
            "dist/cf-cli-java-plugin-*",
            next(step["with"]["path"] for step in steps if step.get("uses", "").startswith("actions/upload-artifact")),
        )

    def test_go_shell_lint_uses_bash(self):
        step = next(step for step in self.steps("build") if "./scripts/lint-go.sh" in step.get("run", ""))
        self.assertEqual(step.get("shell"), "bash")

    def test_go_format_check_skips_windows_crlf_checkout(self):
        steps = self.steps("build")
        quality = next(step for step in steps if "./scripts/lint-go.sh" in step.get("run", ""))
        self.assertEqual(quality.get("if"), "matrix.os != 'windows-latest'")
        self.assertTrue(any(step.get("run") == "go test ./..." for step in steps))
        self.assertTrue(any(step.get("run") == "python3 .github/workflows/build.py" for step in steps))

    def test_ci_go_lint_never_rewrites_tagged_source(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            scripts = folder / "scripts"
            scripts.mkdir()
            shutil.copyfile(ROOT / "scripts/lint-go.sh", scripts / "lint-go.sh")
            (folder / "go.mod").write_text("module example.com/release-lint\n\ngo 1.26\n", encoding="utf-8")
            go_file = folder / "main.go"
            original = 'package main\nfunc main(){println("hello")}\n'
            go_file.write_text(original, encoding="utf-8")
            subprocess.run(["git", "init"], cwd=folder, check=True, capture_output=True)
            subprocess.run(["git", "add", "go.mod", "main.go"], cwd=folder, check=True, capture_output=True)
            result = subprocess.run(["bash", "scripts/lint-go.sh", "ci"], cwd=folder, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(go_file.read_text(encoding="utf-8"), original)

    def test_linux_arm64_binary_is_built_and_required_for_publication(self):
        build = self.steps("build")
        cross_build = next(step for step in build if step.get("name") == "Build Linux ARM64 binary")
        self.assertIn("GOOS=linux GOARCH=arm64 go build", cross_build["run"])
        self.assertEqual(cross_build.get("if"), "matrix.os == 'ubuntu-latest'")
        combine = next(
            step["run"] for step in self.steps("create-release") if step.get("name") == "Verify build artifacts"
        )
        self.assertIn("dist/cf-cli-java-plugin-linux-arm64", combine)

    def test_macos_arm64_is_cross_built_without_hosted_macos_runner(self):
        build = self.workflow["jobs"]["build"]
        self.assertNotIn("macos-latest", build["strategy"]["matrix"]["os"])
        cross_build = next(step for step in build["steps"] if step.get("name") == "Build macOS ARM64 binary")
        self.assertEqual(cross_build.get("if"), "matrix.os == 'ubuntu-latest'")
        self.assertIn("GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build", cross_build["run"])
        self.assertIn("dist/cf-cli-java-plugin-macos-arm64", cross_build["run"])

    def test_generated_release_notes_are_transferred_to_publisher(self):
        self.assertTrue(
            any("release_notes.md" in step.get("with", {}).get("path", "") for step in self.steps("prepare-release"))
        )
        self.assertTrue(
            any("actions/download-artifact" in step.get("uses", "") for step in self.steps("create-release"))
        )
        self.assertEqual(
            next(
                step["with"]["body_path"]
                for step in self.steps("create-release")
                if "body_path" in step.get("with", {})
            ),
            "release_notes.md",
        )

    def test_jstall_tag_is_selected_once_and_shared_with_notes_and_builds(self):
        prepare = self.steps("prepare-release")
        self.assertIn("jstall", [step.get("id") for step in prepare])
        selection = next(step for step in prepare if step.get("id") == "jstall")
        self.assertIn("gh release view --repo parttimenerd/jstall", selection["run"])
        self.assertEqual(
            self.workflow["jobs"]["prepare-release"]["outputs"]["jstall_tag"], "${{ steps.jstall.outputs.tag }}"
        )
        notes = next(step for step in prepare if step.get("name") == "Prepare version and release notes")
        self.assertEqual(notes["env"]["JSTALL_VERSION"], "${{ steps.jstall.outputs.tag }}")
        download = next(
            step for step in self.steps("build") if step.get("name") == "Download JStall minimal JAR for go:embed"
        )
        self.assertEqual(download["env"]["JSTALL_TAG"], "${{ needs.prepare-release.outputs.jstall_tag }}")
        self.assertIn('gh release download "$JSTALL_TAG"', download["run"])
        self.assertNotIn("gh release view", download["run"])

    def test_only_binary_artifacts_are_merged_into_dist(self):
        steps = self.steps("create-release")
        download = next(step for step in steps if step.get("name") == "Download all build artifacts")
        self.assertEqual(download["with"]["pattern"], "binaries-*")
        self.assertEqual(download["with"]["path"], "dist/")
        self.assertTrue(download["with"]["merge-multiple"])

    def test_existing_tag_uses_current_release_generator(self):
        for job in ("prepare-release", "create-release"):
            steps = self.steps(job)
            tooling = next(step for step in steps if step.get("with", {}).get("path") == "release-tools")
            self.assertIn("github.event.repository.default_branch", tooling["with"]["ref"])
        steps = self.steps("create-release")
        self.assertTrue(
            any("release-tools/.github/workflows/generate_plugin_repo.py" in step.get("run", "") for step in steps)
        )

    def test_published_release_does_not_duplicate_plugin_repository_assets(self):
        published = yaml.safe_load((WORKFLOWS / "plugin-repo.yml").read_text(encoding="utf-8"))
        condition = published["jobs"]["generate-plugin-repo"]["if"]
        self.assertIn("plugin-repo-entry.yml", condition)
        self.assertIn("plugin-repo-summary.txt", condition)
        steps = published["jobs"]["generate-plugin-repo"]["steps"]
        self.assertTrue(any(step.get("with", {}).get("path") == "release-tools" for step in steps))
        self.assertTrue(
            any("release-tools/.github/workflows/generate_plugin_repo.py" in step.get("run", "") for step in steps)
        )

    def test_release_is_published_only_after_assets_are_uploaded(self):
        steps = self.steps("create-release")
        upload_index = next(i for i, step in enumerate(steps) if step.get("name") == "Create GitHub Release")
        self.assertTrue(steps[upload_index]["with"].get("draft"))
        publish_index = next(i for i, step in enumerate(steps) if step.get("name") == "Publish complete release")
        self.assertGreater(publish_index, upload_index)
        self.assertIn('gh release edit "$VERSION" --draft=false', steps[publish_index]["run"])

    def test_release_runs_are_serialized_until_publication(self):
        self.assertIn("concurrency", self.workflow)
        self.assertNotIn("concurrency", self.workflow["jobs"]["prepare-release"])
        self.assertFalse(self.workflow["concurrency"]["cancel-in-progress"])

    def test_existing_tag_can_be_published_without_moving_it(self):
        prepare = next(
            step["run"]
            for step in self.steps("prepare-release")
            if step.get("name") == "Prepare version and release notes"
        )
        self.assertIn('gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100"', prepare)
        self.assertIn("git add cf_cli_java_plugin.go CHANGELOG.md\n", prepare)
        self.assertNotIn("git add cf_cli_java_plugin.go CHANGELOG.md release_changelog.txt", prepare)
        self.assertIn('git checkout --detach "$VERSION"', prepare)
        self.assertIn('--release-notes-only "$VERSION"', prepare)
        self.assertIn('git tag "$VERSION"', prepare)
        self.assertIn('remote_tag=$(git ls-remote --tags origin "refs/tags/$VERSION")', prepare)

    def test_generator_includes_windows_executable_in_repository_entry(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            dist = folder / "dist"
            dist.mkdir()
            for name in (
                "cf-cli-java-plugin-linux-amd64",
                "cf-cli-java-plugin-macos-arm64",
                "cf-cli-java-plugin-windows-amd64.exe",
            ):
                (dist / name).write_bytes(b"plugin")
            result = subprocess.run(
                [sys.executable, str(WORKFLOWS / "generate_plugin_repo.py"), "4.0.3"],
                cwd=folder,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            entry = yaml.safe_load((folder / "plugin-repo-entry.yml").read_text(encoding="utf-8"))[0]
            windows = next(binary for binary in entry["binaries"] if binary["platform"] == "win64")
            self.assertTrue(windows["url"].endswith("/cf-cli-java-plugin-windows-amd64.exe"))
            self.assertIn("/releases/tag/4.0.3", (folder / "plugin-repo-summary.txt").read_text(encoding="utf-8"))

    def test_generator_links_to_repository_running_workflow(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            dist = folder / "dist"
            dist.mkdir()
            for name in (
                "cf-cli-java-plugin-linux-amd64",
                "cf-cli-java-plugin-macos-arm64",
                "cf-cli-java-plugin-windows-amd64.exe",
            ):
                (dist / name).write_bytes(b"plugin")
            result = subprocess.run(
                [sys.executable, str(WORKFLOWS / "generate_plugin_repo.py"), "4.0.3"],
                cwd=folder,
                env={**os.environ, "GITHUB_REPOSITORY": "parttimenerd/cf-cli-java-plugin"},
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            entry = yaml.safe_load((folder / "plugin-repo-entry.yml").read_text(encoding="utf-8"))[0]
            self.assertTrue(all("parttimenerd/cf-cli-java-plugin" in binary["url"] for binary in entry["binaries"]))

    def test_generator_rejects_missing_or_empty_required_binaries(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            dist = folder / "dist"
            dist.mkdir()
            (dist / "cf-cli-java-plugin-linux-amd64").write_bytes(b"plugin")
            (dist / "cf-cli-java-plugin-macos-arm64").write_bytes(b"plugin")
            for content in (None, b""):
                windows = dist / "cf-cli-java-plugin-windows-amd64.exe"
                if content is not None:
                    windows.write_bytes(content)
                result = subprocess.run(
                    [sys.executable, str(WORKFLOWS / "generate_plugin_repo.py"), "4.0.3"],
                    cwd=folder,
                    capture_output=True,
                    text=True,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((folder / "plugin-repo-entry.yml").exists())

    def test_manual_preparation_reuses_old_tag_and_creates_new_tag(self):
        prepare = next(
            step["run"]
            for step in self.steps("prepare-release")
            if step.get("name") == "Prepare version and release notes"
        )
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            source = folder / "source"
            source.mkdir()
            remote = folder / "remote.git"

            def git(*args, cwd=source):
                return subprocess.run(
                    ["git", *args], cwd=cwd, check=True, capture_output=True, text=True
                ).stdout.strip()

            git("init", "--bare", str(remote), cwd=folder)
            git("init", "-b", "master")
            git("config", "user.name", "Release Test")
            git("config", "user.email", "release@example.com")
            git("remote", "add", "origin", str(remote))
            (source / "cf_cli_java_plugin.go").write_text(
                "Version: plugin.VersionType{Major: 4, Minor: 0, Build: 3,}\n", encoding="utf-8"
            )
            (source / "CHANGELOG.md").write_text(
                "# Changelog\n\n## [Unreleased]\n\n- Future change\n\n## [4.0.3]\n\n- Existing release\n",
                encoding="utf-8",
            )
            git("add", ".")
            git("commit", "-m", "Tagged source without the new release script")
            tagged_commit = git("rev-parse", "HEAD")
            git("tag", "4.0.3")
            git("push", "origin", "master", "4.0.3")

            workflows = source / ".github/workflows"
            workflows.mkdir(parents=True)
            shutil.copyfile(WORKFLOWS / "update_version.py", workflows / "update_version.py")
            git("add", ".")
            git("commit", "-m", "Install new workflow script")
            git("push", "origin", "master")

            tooling = source / "release-tools/.github/workflows"
            tooling.mkdir(parents=True)
            shutil.copyfile(WORKFLOWS / "update_version.py", tooling / "update_version.py")

            fake_bin = folder / "bin"
            fake_bin.mkdir()
            (fake_bin / "gh").write_text('#!/bin/sh\nif [ "$1" = api ]; then exit 0; fi\nexit 1\n', encoding="utf-8")
            (fake_bin / "gh").chmod(0o755)
            (fake_bin / "python").symlink_to(sys.executable)
            env = {
                **os.environ,
                "PATH": str(fake_bin) + os.pathsep + os.environ["PATH"],
                "GITHUB_EVENT_NAME": "workflow_dispatch",
                "GITHUB_REF_NAME": "master",
                "GITHUB_REPOSITORY": "SAP/cf-cli-java-plugin",
                "RUNNER_TEMP": str(folder),
                "JSTALL_VERSION": "v-test",
            }
            (fake_bin / "gh").write_text(
                '#!/bin/sh\nif [ "$1" = api ]; then echo false; exit 0; fi\nexit 1\n',
                encoding="utf-8",
            )
            env["VERSION"] = "4.0.3"
            published = subprocess.run(
                ["bash", "-e", "-c", prepare], cwd=source, env=env, capture_output=True, text=True
            )
            self.assertNotEqual(published.returncode, 0)
            self.assertEqual(git("rev-parse", "HEAD"), git("rev-parse", "master"))

            (fake_bin / "gh").write_text(
                '#!/bin/sh\nif [ "$1" = api ]; then echo true; exit 0; fi\nexit 1\n',
                encoding="utf-8",
            )
            env["VERSION"] = "4.0.3"
            retry = subprocess.run(["bash", "-e", "-c", prepare], cwd=source, env=env, capture_output=True, text=True)
            self.assertEqual(retry.returncode, 0, retry.stderr)
            (fake_bin / "gh").write_text('#!/bin/sh\nif [ "$1" = api ]; then exit 0; fi\nexit 1\n', encoding="utf-8")

            for version in ("4.0.3", "4.0.4"):
                env["VERSION"] = version
                result = subprocess.run(
                    ["bash", "-e", "-c", prepare], cwd=source, env=env, capture_output=True, text=True
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("Plugin " + version, (source / "release_notes.md").read_text(encoding="utf-8"))
                if version == "4.0.3":
                    self.assertEqual(git("rev-parse", "HEAD"), tagged_commit)
                    self.assertEqual(git("rev-parse", "refs/tags/4.0.3"), tagged_commit)
                    git("checkout", "master")
                else:
                    self.assertIn("## [4.0.4]", (source / "CHANGELOG.md").read_text(encoding="utf-8"))
                    self.assertEqual(git("rev-parse", "refs/tags/4.0.4"), git("rev-parse", "HEAD"))

            git("checkout", "--detach", "4.0.4")
            tag_commit = git("rev-parse", "HEAD")
            env["GITHUB_EVENT_NAME"] = "push"
            result = subprocess.run(["bash", "-e", "-c", prepare], cwd=source, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(git("rev-parse", "HEAD"), tag_commit)
            self.assertIn("Plugin 4.0.4", (source / "release_notes.md").read_text(encoding="utf-8"))

            git("checkout", "--detach", "4.0.3")
            env["VERSION"] = "4.0.3"
            result = subprocess.run(["bash", "-e", "-c", prepare], cwd=source, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(git("rev-parse", "HEAD"), tagged_commit)
            self.assertIn("Plugin 4.0.3", (source / "release_notes.md").read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
