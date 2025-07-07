#!/bin/bash

# Python linting script for CF Java Plugin
# Usage: ./scripts/lint-python.sh [check|fix|ci]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TESTING_DIR="$PROJECT_ROOT/test"

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

# Check if Python test suite exists
if [ ! -f "$TESTING_DIR/requirements.txt" ] || [ ! -f "$TESTING_DIR/pyproject.toml" ]; then
    print_warning "Python test suite not found - skipping Python linting"
    exit 0
fi

# Change to testing directory
cd "$TESTING_DIR"

# Check if virtual environment exists
if [ ! -f "venv/bin/python" ]; then
    print_error "Python virtual environment not found. Run './setup.sh' first."
    exit 1
fi

# Activate virtual environment
source venv/bin/activate

MODE="${1:-check}"

case "$MODE" in
    "check")
        print_info "Running Python linting checks..."
        
        echo "🔍 Running flake8..."
        if ! flake8 --max-line-length=120 --ignore=E203,W503,E402 --exclude=venv,__pycache__,.git .; then
            print_error "Flake8 found linting issues"
            exit 1
        fi
        print_status "Flake8 passed"
        
        echo "🔍 Checking black formatting..."
        if ! black --line-length=120 --check .; then
            print_error "Black found formatting issues"
            exit 1
        fi
        print_status "Black formatting check passed"
        
        echo "🔍 Checking import sorting..."
        if ! isort --check-only --profile=black .; then
            print_error "Isort found import sorting issues"
            exit 1
        fi
        print_status "Import sorting check passed"
        
        print_status "All Python linting checks passed!"
        ;;
        
    "fix")
        print_info "Fixing Python code formatting..."
        
        echo "🔧 Running black formatter..."
        black --line-length=120 .
        print_status "Black formatting applied"
        
        echo "🔧 Sorting imports..."
        isort --profile=black .
        print_status "Import sorting applied"
        
        echo "🔍 Running flake8 check..."
        if ! flake8 --max-line-length=120 --ignore=E203,W503,E402 --exclude=venv,__pycache__,.git .; then
            print_warning "Flake8 still reports issues after auto-fixing"
            print_info "Manual fixes may be required"
            exit 1
        fi
        
        print_status "Python code formatting fixed!"
        ;;
        
    "ci")
        print_info "Running CI linting checks..."
        
        # For CI, we want to be strict and not auto-fix
        echo "🔍 Running flake8..."
        flake8 --max-line-length=120 --ignore=E203,W503,E402 --exclude=venv,__pycache__,.git . || {
            print_error "Flake8 linting failed"
            exit 1
        }
        
        echo "🔍 Checking black formatting..."
        black --line-length=120 --check . || {
            print_error "Black formatting check failed"
            exit 1
        }
        
        echo "🔍 Checking import sorting..."
        isort --check-only --profile=black . || {
            print_error "Import sorting check failed"
            exit 1
        }
        
        print_status "All CI linting checks passed!"
        ;;
        
    *)
        echo "Usage: $0 [check|fix|ci]"
        echo ""
        echo "Modes:"
        echo "  check  - Check code quality without making changes (default)"
        echo "  fix    - Auto-fix formatting and import sorting issues"
        echo "  ci     - Strict checking for CI environments"
        exit 1
        ;;
esac
