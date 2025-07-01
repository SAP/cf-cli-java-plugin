# Linting Scripts Documentation

This directory contains centralized linting and code quality scripts for the CF Java Plugin project.

## Scripts Overview

### `lint-python.sh`
Python-specific linting and formatting script.

**Usage:**
```bash
./scripts/lint-python.sh [check|fix|ci]
```

**Modes:**
- `check` (default): Check code quality without making changes
- `fix`: Auto-fix formatting and import sorting issues  
- `ci`: Strict checking for CI environments

**Tools used:**
- `flake8`: Code linting (line length, style issues)
- `black`: Code formatting
- `isort`: Import sorting

### `lint-go.sh`
Go-specific linting and testing script.

**Usage:**
```bash
./scripts/lint-go.sh [check|test|ci]
```

**Modes:**
- `check` (default): Run linting checks only
- `ci`: Run all checks for CI environments (lint + dependencies)

**Tools used:**
- `go fmt`: Code formatting
- `go vet`: Static analysis

### `lint-all.sh`
Comprehensive script that runs both Go and Python linting.

**Usage:**
```bash
./scripts/lint-all.sh [check|fix|ci]
```

**Features:**
- Runs Go linting first, then Python (if test suite exists)
- Provides unified exit codes and summary
- Color-coded output with status indicators

## Integration Points

### Pre-commit Hooks
- Uses `lint-go.sh check` for Go code
- Uses `lint-python.sh fix` for Python code (auto-fixes issues)

### GitHub Actions CI
- **Build & Snapshot**: Uses `ci` mode for strict checking
- **PR Validation**: Uses `ci` mode for comprehensive validation  
- **Release**: Uses `check` and `test` modes

### Development Workflow
- **Local development**: Use `check` mode for quick validation
- **Before commit**: Use `fix` mode to auto-resolve formatting issues
- **CI/CD**: Uses `ci` mode for strict validation

## Benefits

1. **No Duplication**: Eliminates repeated linting commands across files
2. **Consistency**: Same linting rules applied everywhere
3. **Maintainability**: Single place to update linting configurations
4. **Flexibility**: Different modes for different use cases
5. **Error Handling**: Proper exit codes and error messages
6. **Auto-fixing**: Reduces manual intervention for formatting issues

## Configuration

All linting tools are configured via:
- `test/pyproject.toml`: Python tool configurations
- `test/requirements.txt`: Python tool dependencies  
- Project-level files: Go module and dependencies

Virtual environments and build artifacts are automatically excluded from all linting operations.
