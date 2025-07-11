#!/usr/bin/env python3
"""
Generate plugin repository YAML file for CF CLI plugin repository.
This script creates the YAML file in the required format for the CF CLI plugin repository.
"""

import os
import sys
import yaml
import hashlib
import requests
from datetime import datetime
from pathlib import Path

def calculate_sha1(file_path):
    """Calculate SHA1 checksum of a file."""
    sha1_hash = hashlib.sha1()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(4096), b""):
            sha1_hash.update(chunk)
    return sha1_hash.hexdigest()

def get_version_from_tag():
    """Get version from git tag or environment variable."""
    version = os.environ.get('GITHUB_REF_NAME', '')
    if version.startswith('v'):
        version = version[1:]  # Remove 'v' prefix
    return version or "dev"

def generate_plugin_repo_yaml():
    """Generate the plugin repository YAML file."""
    version = get_version_from_tag()
    repo_url = "https://github.com/SAP/cf-cli-java-plugin"
    
    # Define the binary platforms and their corresponding file extensions
    platforms = {
        "osx": "cf-cli-java-plugin-osx",
        "win64": "cf-cli-java-plugin-win64.exe", 
        "win32": "cf-cli-java-plugin-win32.exe",
        "linux32": "cf-cli-java-plugin-linux32",
        "linux64": "cf-cli-java-plugin-linux64"
    }
    
    binaries = []
    dist_dir = Path("dist")
    
    for platform, filename in platforms.items():
        file_path = dist_dir / filename
        if file_path.exists():
            checksum = calculate_sha1(file_path)
            binary_info = {
                "checksum": checksum,
                "platform": platform,
                "url": f"{repo_url}/releases/download/v{version}/{filename}"
            }
            binaries.append(binary_info)
            print(f"Added {platform}: {filename} (checksum: {checksum})")
        else:
            print(f"Warning: Binary not found for {platform}: {filename}")
    
    if not binaries:
        print("Error: No binaries found in dist/ directory")
        sys.exit(1)
    
    # Create the plugin repository entry
    plugin_entry = {
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
    }
    
    # Write the YAML file
    output_file = Path("plugin-repo-entry.yml")
    with open(output_file, 'w') as f:
        yaml.dump(plugin_entry, f, default_flow_style=False, sort_keys=False)
    
    print(f"Generated plugin repository YAML file: {output_file}")
    print(f"Version: {version}")
    print(f"Binaries: {len(binaries)} platforms")
    
    # Also create a human-readable summary
    summary_file = Path("plugin-repo-summary.txt")
    with open(summary_file, 'w') as f:
        f.write(f"CF CLI Java Plugin Repository Entry\n")
        f.write(f"====================================\n\n")
        f.write(f"Version: {version}\n")
        f.write(f"Updated: {plugin_entry['updated']}\n")
        f.write(f"Binaries: {len(binaries)} platforms\n\n")
        f.write("Platform checksums:\n")
        for binary in binaries:
            f.write(f"  {binary['platform']}: {binary['checksum']}\n")
        f.write(f"\nRepository URL: {repo_url}\n")
        f.write(f"Release URL: {repo_url}/releases/tag/v{version}\n")
    
    print(f"Generated summary file: {summary_file}")

if __name__ == "__main__":
    generate_plugin_repo_yaml()