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

- `gofumpt`: Stricter Go code formatting (fallback to `go fmt`)
- `go vet`: Static analysis
- `golangci-lint`: Comprehensive linting (detects unused interfaces, code smells, etc.)

**Line Length Management:**

The project enforces a 120-character line length limit via the `lll` linter. Note that Go
formatters (`gofumpt`/`go fmt`) do not automatically wrap long lines - this is by design
in the Go community. Manual line breaking is required for lines exceeding the limit.

### `lint-markdown.sh`

Markdown-specific linting and formatting script.

**Usage:**

```bash
./scripts/lint-markdown.sh [check|fix|ci]
```

**Modes:**

- `check` (default): Check markdown quality without making changes
- `fix`: Auto-fix formatting issues
- `ci`: Strict checking for CI environments

**Tools used:**

- `markdownlint-cli`: Markdown linting (structure, style, consistency)
- `prettier`: Markdown formatting

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

### `update-readme-help.py`

Automatically updates README.md with current plugin help text.

**Usage:**

```bash
./scripts/update-readme-help.py
```

**Features:**

- Extracts help text using `cf java help`
- Updates help section in README.md
- Stages changes for git commit
- Integrated into pre-commit hooks

**Requirements:** CF CLI and CF Java plugin must be installed.

## Tool Requirements

### Go Linting Tools

- **golangci-lint**: Install with `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- **go**: Go compiler and tools (comes with Go installation)

### Python Linting Tools

- **flake8**: Install with `pip install flake8`
- **black**: Install with `pip install black`
- **isort**: Install with `pip install isort`

### Markdown Linting Tools

- **markdownlint-cli**: Install with `npm install -g markdownlint-cli`

### Installation

To install all linting tools at once, run:

```bash
./setup-dev-env.sh
```

This will install all required linters and development tools.

## Integration Points

### Pre-commit Hooks

- Uses `lint-go.sh check` for Go code
- Uses `lint-python.sh fix` for Python code (auto-fixes issues)
- Uses `update-readme-help.py` to keep README help text current

### GitHub Actions CI

- **Build & Snapshot**: Uses `ci` mode for strict checking
- **PR Validation**: Uses `ci` mode for comprehensive validation  
- **Release**: Uses `check` and `test` modes

### Development Workflow

- **Local development**: Use `check` mode for quick validation
- **Before commit**: Use `fix` mode to auto-resolve formatting issues
- **CI/CD**: Uses `ci` mode for strict validation

## Configuration

All linting tools are configured via:

- `.golangci.yml`: golangci-lint configuration (enables all linters except gochecknoglobals)
- `test/pyproject.toml`: Python tool configurations
- `test/requirements.txt`: Python tool dependencies  
- Project-level files: Go module and dependencies

Virtual environments and build artifacts are automatically excluded from all linting operations.
