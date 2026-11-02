#!/bin/bash

# Go linting and testing script for CF Java Plugin
# Usage: ./scripts/lint-go.sh [check|fix|ci]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}✅${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠️${NC} $1"
}

print_error() {
    echo -e "${RED}❌${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ️${NC} $1"
}

# Change to project root
cd "$PROJECT_ROOT"

# Check if this is a Go project
if [ ! -f "go.mod" ]; then
    print_error "Not a Go project (go.mod not found)"
    exit 1
fi

MODE="${1:-check}"

run_go_format_check() {
    echo "🔍 Running gofumpt/go fmt check..."

    # Get only Git-tracked Go files
    GO_FILES=$(git ls-files '*.go')
    if [ -z "$GO_FILES" ]; then
        print_warning "No Git-tracked Go files found"
        return 0
    fi

    if command -v gofumpt >/dev/null 2>&1; then
        UNFORMATTED_FILES=$(echo "$GO_FILES" | xargs gofumpt -l)
        if [ -n "$UNFORMATTED_FILES" ]; then
            print_error "Go formatting issues found with gofumpt"
            echo "$UNFORMATTED_FILES"
            print_info "Run './scripts/lint-go.sh fix' to auto-fix formatting"
            return 1
        fi
        print_status "gofumpt formatting check passed on Git-tracked files"
    else
        UNFORMATTED_FILES=$(echo "$GO_FILES" | xargs gofmt -l)
        if [ -n "$UNFORMATTED_FILES" ]; then
            print_error "Go formatting issues found"
            echo "$UNFORMATTED_FILES"
            print_info "Run './scripts/lint-go.sh fix' to auto-fix formatting"
            return 1
        fi
        print_status "Go formatting check passed"
        print_info "For better formatting, install gofumpt: go install mvdan.cc/gofumpt@latest"
    fi
}

run_go_format_fix() {
    echo "🔧 Auto-fixing Go formatting..."

    # Get only Git-tracked Go files
    GO_FILES=$(git ls-files '*.go')
    if [ -z "$GO_FILES" ]; then
        print_warning "No Git-tracked Go files found"
        return 0
    fi

    if command -v gofumpt >/dev/null 2>&1; then
        echo "$GO_FILES" | xargs gofumpt -w
        print_status "Applied gofumpt formatting to Git-tracked files"
    else
        echo "$GO_FILES" | xargs gofmt -w
        print_status "Applied go fmt formatting to Git-tracked files"
        print_info "For better formatting, install gofumpt: go install mvdan.cc/gofumpt@latest"
    fi
}

case "$MODE" in
    "check")
        print_info "Running Go code quality checks..."

        if ! run_go_format_check; then
            exit 1
        fi
        
        echo "🔍 Running go vet..."
        if ! go vet .; then
            print_error "Go vet issues found"
            exit 1
        fi
        print_status "Go vet check passed"
        
        echo "🔍 Running golangci-lint..."
        if command -v golangci-lint >/dev/null 2>&1; then
            if (! golangci-lint run --timeout=3m *.go || ! golangci-lint run utils/*.go); then
                print_error "golangci-lint issues found"
                exit 1
            fi
        else
            print_warning "golangci-lint not found, skipping comprehensive linting"
            print_info "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        fi

        echo "🔍 Running govulncheck..."
        if command -v govulncheck >/dev/null 2>&1; then
            # Report vulnerabilities but don't fail — remaining findings are stdlib-only
            # and require a Go major version bump (1.25.x) to resolve.
            govulncheck . || true
        else
            print_warning "govulncheck not found, skipping vulnerability scan"
            print_info "Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"
        fi

        print_status "All Go linting checks passed!"
        ;;

        "fix")
            print_info "Running Go code quality auto-fixes..."

            run_go_format_fix

            echo "🔍 Running go vet..."
            if ! go vet .; then
                print_error "Go vet issues found"
                exit 1
            fi
            print_status "Go vet check passed"

            echo "🔍 Running golangci-lint (auto-fix where possible)..."
            if command -v golangci-lint >/dev/null 2>&1; then
                # --fix is best-effort and only applies to supported linters
                if (! golangci-lint run --fix --timeout=3m *.go || ! golangci-lint run --fix utils/*.go); then
                    print_error "golangci-lint issues found (some issues may require manual fixes)"
                    exit 1
                fi
            else
                print_warning "golangci-lint not found, skipping comprehensive linting"
                print_info "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
            fi

            print_status "Go auto-fixes completed successfully!"
            ;;
        
    "ci")
        print_info "Running CI checks for Go..."

        # CI must never rewrite tagged source before building release binaries.
        GO_FILES=$(git ls-files '*.go')
        if [ -z "$GO_FILES" ]; then
            print_error "No Git-tracked Go files found"
            exit 1
        fi

        echo "🔍 Checking Go formatting..."
        if command -v gofumpt >/dev/null 2>&1; then
            UNFORMATTED_FILES=$(echo "$GO_FILES" | xargs gofumpt -l)
        else
            UNFORMATTED_FILES=$(echo "$GO_FILES" | xargs gofmt -l)
        fi
        if [ -n "$UNFORMATTED_FILES" ]; then
            print_error "Go formatting issues found:"
            echo "$UNFORMATTED_FILES"
            exit 1
        fi

        echo "🔍 Running go vet..."
        if ! go vet .; then
            print_error "Go vet issues found"
            exit 1
        fi
        
        print_status "All CI checks passed for Go!"
        ;;
        
    *)
        echo "Usage: $0 [check|fix|ci]"
        echo ""
        echo "Modes:"
        echo "  check  - Run linting checks only (default)"
        echo "  fix    - Auto-fix formatting and run lint checks"
        echo "  ci     - Run all checks for CI environments"
        exit 1
        ;;
esac
