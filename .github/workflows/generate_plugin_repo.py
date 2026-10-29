#!/usr/bin/env python3
"""
Generate plugin repository YAML file for CF CLI plugin repository.
This script creates the YAML file in the required format for the CF CLI plugin repository.
"""

import sys
import yaml
import hashlib
from datetime import datetime
from pathlib import Path

def calculate_sha1(file_path):
    """Calculate SHA1 checksum of a file."""
    sha1_hash = hashlib.sha1()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(4096), b""):
            sha1_hash.update(chunk)
    return sha1_hash.hexdigest()

def generate_plugin_repo_yaml(version: str):
    """Generate the plugin repository YAML file."""
    repo_url = "https://github.com/SAP/cf-cli-java-plugin"
    
    # CF Community repository platform IDs are linux64, osx and win64;
    # Linux arm64 is distributed as a manual-download GitHub Release asset.
    platforms = {
        "linux64": "cf-cli-java-plugin-linux-amd64",
        "osx": "cf-cli-java-plugin-macos-arm64", 
        "win64": "cf-cli-java-plugin-windows-amd64.exe"
    }
    
    binaries = []
    dist_dir = Path("dist")

    missing = [
        str(dist_dir / filename)
        for filename in platforms.values()
        if not (dist_dir / filename).is_file() or (dist_dir / filename).stat().st_size == 0
    ]
    if missing:
        print(f"Error: Missing or empty required plugin binaries: {', '.join(missing)}", file=sys.stderr)
        sys.exit(1)
    
    for platform, filename in platforms.items():
        file_path = dist_dir / filename
        checksum = calculate_sha1(file_path)
        binary_info = {
            "checksum": checksum,
            "platform": platform,
            "url": f"{repo_url}/releases/download/{version}/{filename}"
        }
        binaries.append(binary_info)
        print(f"Added {platform}: {filename} (checksum: {checksum})")
    
    # Create the plugin repository entry
    plugin_entry = [{
        "authors": [{
            "contact": "johannes.bechberger@sap.com",
            "homepage": "https://github.com/SAP",
            "name": "Johannes Bechberger"
        }],
        "binaries": binaries,
        "company": "SAP",
        "created": "2024-01-01T00:00:00Z",  # Initial creation date
        "description": "Plugin for profiling Java applications and getting heap and thread-dumps",
        "homepage": repo_url,
        "name": "java",
        "updated": datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
        "version": version
    }]
    
    # Write the YAML file
    output_file = Path("plugin-repo-entry.yml")
    yaml_content = yaml.dump(plugin_entry, default_flow_style=False, sort_keys=False).replace('"', "'")
    with open(output_file, 'w') as f:
        f.write(yaml_content)
    
    print(f"Generated plugin repository YAML file: {output_file}")
    print(f"Version: {version}")
    print(f"Binaries: {len(binaries)} platforms")
    
    # Also create a human-readable summary
    summary_file = Path("plugin-repo-summary.txt")
    with open(summary_file, 'w') as f:
        f.write(f"CF CLI Java Plugin Repository Entry\n")
        f.write(f"====================================\n\n")
        f.write(f"Version: {version}\n")
        f.write(f"Updated: {plugin_entry[0]['updated']}\n")
        f.write(f"Binaries: {len(binaries)} platforms\n\n")
        f.write("Platform checksums:\n")
        for binary in binaries:
            f.write(f"  {binary['platform']}: {binary['checksum']}\n")
        f.write(f"\nRepository URL: {repo_url}\n")
        f.write(f"Release URL: {repo_url}/releases/tag/{version}\n")
    
    print(f"Generated summary file: {summary_file}")

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: generate_plugin_repo.py <version>")
        print("Example: generate_plugin_repo.py 4.1.0")
        sys.exit(1)
    generate_plugin_repo_yaml(sys.argv[1])