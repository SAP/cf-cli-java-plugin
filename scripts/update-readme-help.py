#!/usr/bin/env python3
"""
Script to update README.md with current plugin help text.
Usage: ./scripts/update-readme-help.py
"""

import subprocess
import sys
import os
import tempfile
import shutil
from pathlib import Path


class Colors:
    """ANSI color codes for terminal output."""
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    NC = '\033[0m'  # No Color


def print_status(message: str) -> None:
    """Print a success message with green checkmark."""
    print(f"{Colors.GREEN}✅{Colors.NC} {message}")


def print_warning(message: str) -> None:
    """Print a warning message with yellow warning sign."""
    print(f"{Colors.YELLOW}⚠️{Colors.NC} {message}")


def print_error(message: str) -> None:
    """Print an error message with red X."""
    print(f"{Colors.RED}❌{Colors.NC} {message}")


def check_repository_root() -> None:
    """Check if we're in the correct repository root directory."""
    if not Path("cf_cli_java_plugin.go").exists():
        print_error("Not in CF Java Plugin root directory")
        print("Please run this script from the repository root")
        sys.exit(1)
    
    if not Path("README.md").exists():
        print_error("README.md not found")
        sys.exit(1)


def ensure_plugin_installed() -> None:
    """Ensure the java plugin is installed in cf CLI."""
    print("🔍 Checking if cf java plugin is installed...")
    
    try:
        # Check if the java plugin is installed
        result = subprocess.run(
            ["cf", "plugins"],
            capture_output=True,
            text=True,
            check=True
        )
        
        if "java" not in result.stdout:
            print_error("CF Java plugin is not installed")
            print("Please install the plugin first with:")
            print("  cf install-plugin <path-to-plugin>")
            sys.exit(1)
        else:
            print_status("CF Java plugin is installed")
            
    except subprocess.CalledProcessError as e:
        print_error("Failed to check cf plugins")
        print("Make sure the cf CLI is installed and configured")
        print(f"Error: {e}")
        sys.exit(1)


def get_plugin_help() -> str:
    """Extract help text from the plugin, skipping first 3 lines."""
    print("📝 Extracting help text from plugin...")
    
    try:
        # Use 'cf java help' to get the help text
        result = subprocess.run(
            ["cf", "java", "help"],
            capture_output=True,
            text=True
        )
        
        # Combine stdout and stderr since plugin might write to stderr
        output = result.stdout + result.stderr
        
        # Skip first 3 lines as requested
        lines = output.splitlines()
        help_text = '\n'.join(lines[3:]) if len(lines) > 3 else output
        
        # Validate that we got reasonable help text
        if not help_text.strip():
            print_error("Failed to get help text from plugin")
            sys.exit(1)
        
        # Check if it looks like actual help text (should contain USAGE or similar)
        if "USAGE:" not in help_text and "Commands:" not in help_text:
            print_warning("Help text doesn't look like expected format")
            print(f"Got: {help_text[:100]}...")
            
        return help_text
        
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print_error(f"Failed to run 'cf java help': {e}")
        print("Make sure the cf CLI is installed and the java plugin is installed")
        sys.exit(1)


def update_readme_help(help_text: str) -> bool:
    """Update README.md with the new help text."""
    readme_path = Path("README.md")
    
    print("🔄 Updating README.md...")
    
    with open(readme_path, 'r', encoding='utf-8') as f:
        lines = f.readlines()
    
    # Create temporary file
    with tempfile.NamedTemporaryFile(mode='w', delete=False, encoding='utf-8') as temp_file:
        temp_path = temp_file.name
        
        in_help_section = False
        help_updated = False
        found_pre = False
        
        for i, line in enumerate(lines):
            # Look for <pre> tag after a line mentioning cf java --help
            if not in_help_section and '<pre>' in line.strip():
                # Check if previous few lines mention "cf java"
                context_start = max(0, i - 3)
                context = ''.join(lines[context_start:i])
                if 'cf java' in context:
                    found_pre = True
                    in_help_section = True
                    temp_file.write(line)
                    temp_file.write(help_text + '\n')
                    help_updated = True
                else:
                    temp_file.write(line)
            # Look for the end of help section
            elif in_help_section and '</pre>' in line:
                in_help_section = False
                temp_file.write(line)
            # Write lines that are not in the help section
            elif not in_help_section:
                temp_file.write(line)
            # Skip lines that are inside the help section (old help text)
    
    if help_updated:
        # Replace the original file
        shutil.move(temp_path, readme_path)
        print_status("README.md help text updated successfully")
        
        # Stage changes if in git repository
        try:
            subprocess.run(
                ["git", "rev-parse", "--git-dir"],
                capture_output=True,
                check=True
            )
            subprocess.run(["git", "add", "README.md"], check=True)
            print_status("Changes staged for commit")
        except subprocess.CalledProcessError:
            # Not in a git repository or git command failed
            pass
        
        return True
    else:
        # Clean up temp file
        os.unlink(temp_path)
        print_warning("Help section not found in README.md")
        print("Expected to find a <pre> tag following a mention of 'cf java'")
        return False


def main() -> None:
    """Main function to orchestrate the README update process."""
    try:
        check_repository_root()
        ensure_plugin_installed()
        help_text = get_plugin_help()
        
        if update_readme_help(help_text):
            print("\n🎉 README help text update complete!")
        else:
            sys.exit(1)
            
    except KeyboardInterrupt:
        print_error("\nOperation cancelled by user")
        sys.exit(1)
    except Exception as e:
        print_error(f"Unexpected error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
