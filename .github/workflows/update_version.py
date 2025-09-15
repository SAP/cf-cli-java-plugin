#!/usr/bin/env python3
"""
Update version in cf_cli_java_plugin.go for releases.
This script updates the PluginMetadata version in the Go source code and processes the changelog.
"""

import sys
import re
from pathlib import Path

def update_version_in_go_file(file_path, major, minor, build):
    """Update the version in the Go plugin metadata."""
    with open(file_path, 'r') as f:
        content = f.read()

    # Pattern to match the Version struct in PluginMetadata
    pattern = r'(Version: plugin\.VersionType\s*{\s*Major:\s*)\d+(\s*,\s*Minor:\s*)\d+(\s*,\s*Build:\s*)\d+(\s*,\s*})'

    replacement = rf'\g<1>{major}\g<2>{minor}\g<3>{build}\g<4>'

    new_content = re.sub(pattern, replacement, content)

    if new_content == content:
        print(f"Warning: Version pattern not found or not updated in {file_path}")
        return False

    with open(file_path, 'w') as f:
        f.write(new_content)

    print(f"✅ Updated version to {major}.{minor}.{build} in {file_path}")
    return True

def process_readme_changelog(readme_path, version):
    """Process the README changelog section for the release."""
    with open(readme_path, 'r') as f:
        content = f.read()

    # Look for the snapshot section
    snapshot_pattern = rf'### Snapshot\s*\n'
    match = re.search(snapshot_pattern, content)

    if not match:
        print(f"Error: README.md does not contain a '### Snapshot' section")
        return False, None

    # Find the content of the snapshot section
    start_pos = match.end()

    # Find the next ## section or end of file
    next_section_pattern = r'\n##(#?) '
    next_match = re.search(next_section_pattern, content[start_pos:])

    if next_match:
        end_pos = start_pos + next_match.start()
        section_content = content[start_pos:end_pos].strip()
    else:
        section_content = content[start_pos:].strip()

    # Remove the "-snapshot" from the header
    new_header = f"## {version}"
    updated_content = re.sub(snapshot_pattern, "## Snapshot\n\n\n" + new_header + '\n\n', content)

    # Write the updated README
    with open(readme_path, 'w') as f:
        f.write(updated_content)

    print(f"✅ Updated README.md: converted '## Snapshot' to '## {version}'")

    return True, section_content

def get_base_version(version):
    """Return the base version (e.g., 4.0.0 from 4.0.0-rc2)"""
    return version.split('-')[0]

def is_rc_version(version_str):
    """Return True if the version string ends with -rc or -rcN."""
    return bool(re.match(r"^\d+\.\d+\.\d+-rc(\d+)?$", version_str))

def generate_release_notes(version, changelog_content):
    """Generate complete release notes file."""
    release_notes = f"""## CF CLI Java Plugin {version}

Plugin for profiling Java applications and getting heap and thread-dumps.

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
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/{version}/cf-cli-java-plugin-macos-arm64
# on Windows x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/{version}/cf-cli-java-plugin-windows-amd64
# on Linux x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/{version}/cf-cli-java-plugin-linux-amd64
```

Or download the latest release:

```bash
# on Mac arm64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-macos-arm64
# on Windows x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-windows-amd64
# on Linux x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-linux-amd64
```

**Note:** On Linux and macOS, if you get a permission error, run `chmod +x [cf-cli-java-plugin]` on the plugin binary.
On Windows, the plugin will refuse to install unless the binary has the `.exe` file extension.

You can verify that the plugin is successfully installed by looking for `java` in the output of `cf plugins`.
"""

    with open("release_notes.md", 'w') as f:
        f.write(release_notes)

def main():
    if len(sys.argv) != 4:
        print("Usage: update_version.py <major> <minor> <build>")
        print("Example: update_version.py 4 1 0")
        sys.exit(1)

    try:
        major = int(sys.argv[1])
        minor = int(sys.argv[2])
        build = int(sys.argv[3])
    except ValueError:
        print("Error: Version numbers must be integers")
        sys.exit(1)

    version = f"{major}.{minor}.{build}"
    version_arg = f"{major}.{minor}.{build}" if (major + minor + build) != 0 else sys.argv[1]
    # Accept any -rc suffix, e.g. 4.0.0-rc, 4.0.0-rc1, 4.0.0-rc2
    if is_rc_version(sys.argv[1]):
        base_version = get_base_version(sys.argv[1])
        go_file = Path("cf_cli_java_plugin.go")
        readme_file = Path("README.md")
        changelog_file = Path("release_changelog.txt")
        if not readme_file.exists():
            print(f"Error: {readme_file} not found")
            sys.exit(1)
        with open(readme_file, 'r') as f:
            content = f.read()
        # Find the section for the base version
        base_pattern = rf'## {re.escape(base_version)}\s*\n'
        match = re.search(base_pattern, content)
        if not match:
            print(f"Error: README.md does not contain a '## {base_version}' section for RC release")
            sys.exit(1)
        start_pos = match.end()
        next_match = re.search(r'\n## ', content[start_pos:])
        if next_match:
            end_pos = start_pos + next_match.start()
            section_content = content[start_pos:end_pos].strip()
        else:
            section_content = content[start_pos:].strip()
        with open(changelog_file, 'w') as f:
            f.write(section_content)

        # Generate full release notes for RC
        generate_release_notes(sys.argv[1], section_content)

        print(f"✅ RC release: Changelog for {base_version} saved to {changelog_file}")
        print(f"✅ RC release: Full release notes saved to release_notes.md")
        sys.exit(0)

    go_file = Path("cf_cli_java_plugin.go")
    readme_file = Path("README.md")
    changelog_file = Path("release_changelog.txt")

    # Update Go version
    success = update_version_in_go_file(go_file, major, minor, build)
    if not success:
        sys.exit(1)

    # Process README changelog
    success, changelog_content = process_readme_changelog(readme_file, version)
    if not success:
        sys.exit(1)

    # Write changelog content to a file for the workflow to use
    with open(changelog_file, 'w') as f:
        f.write(changelog_content)

    # Generate full release notes
    generate_release_notes(version, changelog_content)

    print(f"✅ Version updated successfully to {version}")
    print(f"✅ Changelog content saved to {changelog_file}")
    print(f"✅ Full release notes saved to release_notes.md")

if __name__ == "__main__":
    main()
