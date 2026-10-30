#!/usr/bin/env python3
"""
Update the plugin version and CHANGELOG.md for releases.

For normal releases, this script updates the PluginMetadata version,
promotes the Unreleased changelog when needed, and generates release notes.

For RC releases, it extracts the existing base-version changelog and
generates release notes without modifying the source version or changelog.

The --release-notes-only option generates release notes from an existing
version section without modifying the source version or changelog.
"""

import sys
import re
import os
from pathlib import Path

try:
    import requests

    HAS_REQUESTS = True
except ImportError:
    HAS_REQUESTS = False


GO_VERSION_PATTERN = (
    r"(Version: plugin\.VersionType\s*{\s*Major:\s*)\d+" r"(\s*,\s*Minor:\s*)\d+" r"(\s*,\s*Build:\s*)\d+" r"(\s*,\s*})"
)


def update_version_in_go_file(file_path, major, minor, build, dry_run=False):
    """Update the version in the Go plugin metadata."""
    with open(file_path, "r", encoding="utf-8") as f:
        content = f.read()

    replacement = rf"\g<1>{major}\g<2>{minor}\g<3>{build}\g<4>"

    new_content, count = re.subn(GO_VERSION_PATTERN, replacement, content)

    if count == 0:
        print(
            f"Error: Version pattern not found in {file_path}",
            file=sys.stderr,
        )
        return False

    if not dry_run:
        with open(file_path, "w", encoding="utf-8") as f:
            f.write(new_content)

        print(f"Updated version to {major}.{minor}.{build} in {file_path}")
    return True


def find_changelog_section(content, heading):
    """Return the heading match and content of one exact changelog section."""
    pattern = rf"^##[ \t]+\[{re.escape(heading)}\][ \t]*$"
    matches = list(re.finditer(pattern, content, re.MULTILINE))
    if len(matches) != 1:
        reason = "missing" if not matches else "duplicate"
        raise ValueError(f"{reason} '## [{heading}]' section in CHANGELOG.md")

    match = matches[0]
    next_match = re.search(r"^##[ \t]+", content[match.end() :], re.MULTILINE)
    end = match.end() + next_match.start() if next_match else len(content)
    section_content = content[match.end() : end].strip()
    if not section_content:
        raise ValueError(f"empty '## [{heading}]' section in CHANGELOG.md")
    return match, section_content


def prepare_changelog(changelog_path, version):
    """Extract an existing version or prepare to promote Unreleased."""
    content = changelog_path.read_text(encoding="utf-8")
    version_pattern = rf"^##[ \t]+\[{re.escape(version)}\][ \t]*$"
    if re.search(version_pattern, content, re.MULTILINE):
        _, section_content = find_changelog_section(content, version)
        return content, section_content

    match, section_content = find_changelog_section(content, "Unreleased")
    updated = content[: match.start()] + f"## [Unreleased]\n\n## [{version}]" + content[match.end() :]
    return updated, section_content


def get_base_version(version):
    """Return the base version, e.g. 4.0.0 from 4.0.0-rc2."""
    return version.split("-")[0]


def is_rc_version(version_str):
    """Return True for RC versions such as 4.0.0-rc or 4.0.0-rc2."""
    return bool(re.fullmatch(r"\d+\.\d+\.\d+-rc\d*", version_str))


def get_jstall_version():
    """Get the bundled JStall version from an environment variable or API."""
    env_version = os.environ.get("JSTALL_VERSION")
    if env_version:
        return env_version

    if HAS_REQUESTS:
        try:
            response = requests.get(
                "https://api.github.com/repos/parttimenerd/jstall/releases/latest",
                timeout=10,
            )
            if response.status_code == 200:
                return response.json().get("tag_name", "")
        except Exception as exc:
            print(
                f"Warning: Could not retrieve JStall version: {exc}",
                file=sys.stderr,
            )

    return ""


def generate_release_notes(version, changelog_content):
    """Generate the complete release notes file."""
    jstall_version = get_jstall_version()
    jstall_note = ""

    if jstall_version:
        jstall_note = (
            f"\n**Bundled JStall version**: "
            f"[{jstall_version}]"
            f"(https://github.com/parttimenerd/jstall/releases/tag/"
            f"{jstall_version})\n"
        )
        jstall_note += (
            "\n> **Tip:** You can also use a newer JStall version "
            "directly via `jstall --cf APP_NAME COMMAND` "
            "(see [JStall README]"
            "(https://github.com/parttimenerd/jstall#usage)).\n"
        )

    repo_url = "https://github.com/" + os.environ.get("GITHUB_REPOSITORY", "SAP/cf-cli-java-plugin")
    release_url = f"{repo_url}/releases/download/{version}"
    latest_url = f"{repo_url}/releases/latest/download"
    release_notes = f"""## CF CLI Java Plugin {version}

Plugin for profiling Java applications and getting heap and thread-dumps.
{jstall_note}
## Changes

{changelog_content}

## Installation

### Installation via CF Community Repository

Make sure you have the CF Community plugin repository configured or add it via:

```bash
cf add-plugin-repo CF-Community http://plugins.cloudfoundry.org
```

Trigger installation of the plugin via:

```bash
cf install-plugin -r CF-Community "java"
```

### Manual Installation

Download this specific release ({version}) and install manually:

```bash
# on Mac arm64
cf install-plugin {release_url}/cf-cli-java-plugin-macos-arm64
# on Windows amd64
cf install-plugin {release_url}/cf-cli-java-plugin-windows-amd64.exe
# on Linux amd64
cf install-plugin {release_url}/cf-cli-java-plugin-linux-amd64
# on Linux arm64
cf install-plugin {release_url}/cf-cli-java-plugin-linux-arm64
```

Or download the latest release:

```bash
# on Mac arm64
cf install-plugin {latest_url}/cf-cli-java-plugin-macos-arm64
# on Windows amd64
cf install-plugin {latest_url}/cf-cli-java-plugin-windows-amd64.exe
# on Linux amd64
cf install-plugin {latest_url}/cf-cli-java-plugin-linux-amd64
# on Linux arm64
cf install-plugin {latest_url}/cf-cli-java-plugin-linux-arm64
```

**Note:** On Linux and macOS, if you get a permission error, run `chmod +x [cf-cli-java-plugin]` on the plugin binary.
On Windows, the plugin will refuse to install unless the binary has the `.exe` file extension.

You can verify that the plugin is successfully installed by looking for `java` in the output of `cf plugins`.
"""

    with open("release_notes.md", "w", encoding="utf-8") as f:
        f.write(release_notes)


def extract_version_changelog(changelog_path, version):
    """
    Extract a version's changelog section without modifying CHANGELOG.md.

    For RC versions, use the base version's section.
    """
    if not changelog_path.exists():
        print(f"Error: {changelog_path} not found", file=sys.stderr)
        sys.exit(1)

    base_version = get_base_version(version)

    with open(changelog_path, "r", encoding="utf-8") as f:
        content = f.read()

    try:
        _, section_content = find_changelog_section(content, base_version)
    except ValueError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)

    return section_content


def generate_release_notes_from_existing_changelog(version):
    """
    Generate release notes for an existing release tag.

    This mode does not modify cf_cli_java_plugin.go or CHANGELOG.md.
    It writes release_changelog.txt and release_notes.md.
    """
    changelog_file = Path("CHANGELOG.md")
    changelog_path = Path("release_changelog.txt")

    section_content = extract_version_changelog(changelog_file, version)

    with open(changelog_path, "w", encoding="utf-8") as f:
        f.write(section_content + "\n")

    generate_release_notes(version, section_content)

    print(f"Generated release notes for {version}")


def main():
    # Read-only source-version mode for an existing release tag.
    if len(sys.argv) == 3 and sys.argv[1] == "--release-notes-only":
        version = sys.argv[2]

        if not re.fullmatch(
            r"\d+\.\d+\.\d+(?:-rc\d*)?",
            version,
        ):
            print(f"Error: Invalid release version: {version}", file=sys.stderr)
            sys.exit(1)

        generate_release_notes_from_existing_changelog(version)
        return

    if len(sys.argv) != 4:
        print("Usage: update_version.py <major> <minor> <build>")
        print("Example: update_version.py 4 1 0")
        print("For RC releases: update_version.py 4.0.0-rc1 0 0")
        sys.exit(1)

    # RC handling must happen before integer parsing because sys.argv[1]
    # contains a complete version string for RC releases.
    if is_rc_version(sys.argv[1]):
        version = sys.argv[1]
        changelog_file = Path("CHANGELOG.md")
        changelog_path = Path("release_changelog.txt")

        section_content = extract_version_changelog(changelog_file, version)

        with open(changelog_path, "w", encoding="utf-8") as f:
            f.write(section_content + "\n")

        generate_release_notes(version, section_content)

        print(f"RC release: Changelog for {get_base_version(version)} " f"saved to {changelog_path}")
        print("RC release: Full release notes saved to release_notes.md")
        return

    try:
        major = int(sys.argv[1])
        minor = int(sys.argv[2])
        build = int(sys.argv[3])
    except ValueError:
        print("Error: Version numbers must be integers", file=sys.stderr)
        sys.exit(1)

    version = f"{major}.{minor}.{build}"

    go_file = Path("cf_cli_java_plugin.go")
    changelog_file = Path("CHANGELOG.md")
    release_changelog_file = Path("release_changelog.txt")

    # Validate both inputs before changing either source file.
    try:
        updated_changelog, changelog_content = prepare_changelog(changelog_file, version)
    except (OSError, ValueError) as exc:
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)

    if not update_version_in_go_file(go_file, major, minor, build, dry_run=True):
        sys.exit(1)

    update_version_in_go_file(go_file, major, minor, build)
    if updated_changelog != changelog_file.read_text(encoding="utf-8"):
        changelog_file.write_text(updated_changelog, encoding="utf-8")
        print(f"Updated CHANGELOG.md: created '## [{version}]' section")

    # Save the extracted changelog for use by the workflow.
    with open(release_changelog_file, "w", encoding="utf-8") as f:
        f.write(changelog_content + "\n")

    # Generate the complete release notes.
    generate_release_notes(version, changelog_content)

    print(f"Version updated successfully to {version}")
    print(f"Changelog content saved to {release_changelog_file}")
    print("Full release notes saved to release_notes.md")


if __name__ == "__main__":
    main()
