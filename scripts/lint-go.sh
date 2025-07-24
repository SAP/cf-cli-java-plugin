#!/bin/bash

# Go linting and testing script for CF Java Plugin
# Usage: ./scripts/lint-go.sh [check|test|ci]

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

case "$MODE" in
    "check")
        print_info "Running Go code quality checks..."
        
        echo "🔍 Running gofumpt..."
        if command -v gofumpt >/dev/null 2>&1; then
            if ! gofumpt -l -w *.go cmd/ utils/; then
                print_error "Go formatting issues found with gofumpt"
                exit 1
            fi
            print_status "gofumpt formatting check passed"
        else
            echo "🔍 Running go fmt..."
            if ! go fmt ./...; then
                print_error "Go formatting issues found. Run 'go fmt ./...' to fix."
                exit 1
            fi
            print_status "Go formatting check passed"
            print_info "For better formatting, install gofumpt: go install mvdan.cc/gofumpt@latest"
        fi
        
        echo "🔍 Running go vet..."
        if ! go vet .; then
            print_error "Go vet issues found"
            exit 1
        fi
        print_status "Go vet check passed"
        
        echo "🔍 Running golangci-lint..."
        if command -v golangci-lint >/dev/null 2>&1; then
            if ! golangci-lint run --timeout=3m *.go; then
                print_error "golangci-lint issues found"
                exit 1
            fi
            print_status "golangci-lint check passed"
        else
            print_warning "golangci-lint not found, skipping comprehensive linting"
            print_info "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        fi
        
        print_status "All Go linting checks passed!"
        ;;
        
    "ci")
        print_info "Running CI checks for Go..."
        
        echo "🔍 Installing dependencies..."
        go mod tidy -e || true
        
        echo "🔍 Running gofumpt..."
        if command -v gofumpt >/dev/null 2>&1; then
            if ! gofumpt -l -w *.go cmd/ utils/; then
                print_error "Go formatting issues found with gofumpt"
                exit 1
            fi
        else
            echo "🔍 Running go fmt..."
            if ! go fmt ./...; then
                print_error "Go formatting issues found"
                exit 1
            fi
        fi
        
        echo "🔍 Running go vet..."
        if ! go vet .; then
            print_error "Go vet issues found"
            exit 1
        fi
        
        echo "🔍 Running golangci-lint..."
        if command -v golangci-lint >/dev/null 2>&1; then
            if ! golangci-lint run --timeout=5m *.go; then
                print_error "golangci-lint issues found"
                exit 1
            fi
            print_status "golangci-lint check passed"
        else
            print_error "golangci-lint not found in CI environment"
            print_info "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
            exit 1
        fi
        
        print_status "All CI checks passed for Go!"
        ;;
        
    *)
        echo "Usage: $0 [check|ci]"
        echo ""
        echo "Modes:"
        echo "  check  - Run linting checks only (default)"
        echo "  ci     - Run all checks for CI environments"
        exit 1
        ;;
esac
