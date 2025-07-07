"""
File validation utilities for checking generated files.

This module provides validators to check if generated files look like valid
heap dumps, JFR files, etc.
"""

import glob
import os
import subprocess
from enum import Enum


class FileType(Enum):
    """Supported file types for validation."""

    HEAP_DUMP = "heap_dump"
    JFR = "jfr"


class FileValidationError(Exception):
    """Raised when file validation fails."""

    pass


class FileValidator:
    """Base class for file validators."""

    def __init__(self, file_type: str):
        self.file_type = file_type

    def validate_local_file(self, pattern: str) -> str:
        """
        Validate a local file matches the expected type.

        Args:
            pattern: Glob pattern to find the file

        Returns:
            Path to the validated file

        Raises:
            FileValidationError: If file doesn't exist or doesn't match expected type
        """
        files = glob.glob(pattern)
        if not files:
            raise FileValidationError(f"No {self.file_type} file found matching pattern: {pattern}")

        file_path = files[0]  # Use first match
        self._validate_file_content(file_path)
        return file_path

    def validate_remote_file(self, pattern: str, ssh_result: str) -> None:
        """
        Validate a remote file exists and matches expected type.

        Args:
            pattern: File pattern to check
            ssh_result: Output from SSH command listing files

        Raises:
            FileValidationError: If file doesn't exist or validation fails
        """
        # This would be implemented for remote validation
        # For now, just check if the pattern appears in SSH output
        if pattern.replace("*", "") not in ssh_result:
            raise FileValidationError(f"No {self.file_type} file found in remote location")

    def _validate_file_content(self, file_path: str) -> None:
        """Override in subclasses to validate specific file types."""
        raise NotImplementedError("Subclasses must implement _validate_file_content")


class HeapDumpValidator(FileValidator):
    """Validator for heap dump files (.hprof)."""

    def __init__(self):
        super().__init__("heap dump")

    def _validate_file_content(self, file_path: str) -> None:
        """Validate that file looks like a valid heap dump."""
        # Check file size
        file_size = os.path.getsize(file_path)
        if file_size < 1024:
            raise FileValidationError(f"Heap dump file {file_path} is too small ({file_size} bytes)")

        # Check HPROF header
        try:
            with open(file_path, "rb") as f:
                header = f.read(20)
                if not header.startswith(b"JAVA PROFILE"):
                    raise FileValidationError(
                        f"File {file_path} does not appear to be a valid heap dump " f"(missing HPROF header)"
                    )
        except IOError as e:
            raise FileValidationError(f"Could not read heap dump file {file_path}: {e}")


class JFRValidator(FileValidator):
    """Validator for Java Flight Recorder files (.jfr)."""

    def __init__(self):
        super().__init__("JFR file")

    def _validate_file_content(self, file_path: str) -> None:
        """Validate that file looks like a valid JFR file."""
        # Check file size
        file_size = os.path.getsize(file_path)
        if file_size < 512:
            raise FileValidationError(f"JFR file {file_path} is too small ({file_size} bytes)")

        try:
            # Use 'jfr summary' command to validate the file
            result = subprocess.run(["jfr", "summary", file_path], capture_output=True, text=True, timeout=30)

            if result.returncode != 0:
                raise FileValidationError(f"JFR file {file_path} failed validation with jfr summary: {result.stderr}")

            # Check that summary output is not empty
            if not result.stdout.strip():
                raise FileValidationError(f"JFR file {file_path} appears invalid - jfr summary returned empty output")

        except subprocess.TimeoutExpired:
            raise FileValidationError(f"JFR validation timed out for file {file_path}")
        except FileNotFoundError:
            # Fallback to basic binary file check if jfr command is not available
            try:
                with open(file_path, "rb") as f:
                    header = f.read(8)

                    if len(header) < 4:
                        raise FileValidationError(f"JFR file {file_path} is too small to contain valid header")

                    # Basic check: JFR files are binary and should not be pure text
                    try:
                        header.decode("ascii")
                        raise FileValidationError(f"File {file_path} appears to be text, not a binary JFR file")
                    except UnicodeDecodeError:
                        # Good! Binary data as expected for JFR
                        pass
            except IOError as e:
                raise FileValidationError(f"Could not read JFR file {file_path}: {e}")
        except IOError as e:
            raise FileValidationError(f"Could not validate JFR file {file_path}: {e}")


def validate_thread_dump_output(output: str) -> None:
    """
    Validate that output looks like a valid thread dump.

    Args:
        output: Thread dump output string to validate

    Raises:
        FileValidationError: If output doesn't look like a valid thread dump
    """
    if not output or not output.strip():
        raise FileValidationError("Thread dump output is empty")

    # Check for required thread dump header
    if "Full thread dump" not in output:
        raise FileValidationError("Thread dump output missing 'Full thread dump' header")

    # Check for at least one thread entry
    if '"' not in output or "java.lang.Thread.State:" not in output:
        raise FileValidationError("Thread dump output does not contain valid thread information")

    # Count lines to ensure substantial output
    lines = output.split("\n")
    non_empty_lines = [line for line in lines if line.strip()]
    if len(non_empty_lines) < 10:
        raise FileValidationError(
            f"Thread dump output too short ({len(non_empty_lines)} non-empty lines), expected at least 5"
        )

    # Check for common thread dump patterns
    has_thread_names = any('"' in line and "#" in line for line in lines)  # Thread lines contain quotes and thread IDs
    has_thread_states = any("java.lang.Thread.State:" in line for line in lines)

    if not has_thread_names:
        raise FileValidationError("Thread dump output missing thread names with quotes")

    if not has_thread_states:
        raise FileValidationError("Thread dump output missing thread states")


# Factory function to create validators
_VALIDATORS = {
    FileType.HEAP_DUMP: HeapDumpValidator,
    FileType.JFR: JFRValidator,
}


def create_validator(file_type: FileType) -> FileValidator:
    """
    Create a validator for the specified file type.

    Args:
        file_type: Type of file to validate

    Returns:
        Appropriate validator instance

    Raises:
        ValueError: If file_type is not supported
    """
    if file_type not in _VALIDATORS:
        supported_types = ", ".join([ft.value for ft in FileType])
        raise ValueError(f"Unsupported file type: {file_type}. Supported: {supported_types}")

    return _VALIDATORS[file_type]()


def validate_generated_file(file_pattern: str, file_type: FileType) -> str:
    """
    Convenience function to validate a generated local file.

    Args:
        file_pattern: Glob pattern to find the file
        file_type: Expected type of file

    Returns:
        Path to the validated file
    """
    validator = create_validator(file_type)
    return validator.validate_local_file(file_pattern)


def validate_generated_remote_file(file_pattern: str, file_type: FileType, ssh_output: str) -> None:
    """
    Convenience function to validate a generated remote file.

    Args:
        file_pattern: File pattern to check
        file_type: Expected type of file
        ssh_output: Output from SSH command
    """
    validator = create_validator(file_type)
    validator.validate_remote_file(file_pattern, ssh_output)
