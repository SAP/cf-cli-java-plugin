#!/bin/bash

# Comprehensive linting script for CF Java Plugin
# Usage: ./scripts/lint-all.sh [check|fix|ci]

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

print_header() {
    echo -e "\n${BLUE}================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================${NC}\n"
}

MODE="${1:-check}"

# Change to project root
cd "$PROJECT_ROOT"

print_header "CF Java Plugin - Code Quality Check"

# Track overall success
OVERALL_SUCCESS=true

# Run Go linting
print_header "Go Code Quality"
if "$SCRIPT_DIR/lint-go.sh" "$MODE"; then
    print_status "Go linting passed"
else
    print_error "Go linting failed"
    OVERALL_SUCCESS=false
fi

# Run Python linting (if test suite exists)
print_header "Python Code Quality"
if [ -f "test/requirements.txt" ]; then
    if "$SCRIPT_DIR/lint-python.sh" "$MODE"; then
        print_status "Python linting passed"
    else
        print_error "Python linting failed"
        OVERALL_SUCCESS=false
    fi
else
    print_warning "Python test suite not found - skipping Python linting"
fi

# Run Markdown linting
print_header "Markdown Code Quality"
if "$SCRIPT_DIR/lint-markdown.sh" "$MODE"; then
    print_status "Markdown linting passed"
else
    print_error "Markdown linting failed"
    OVERALL_SUCCESS=false
fi

# Final summary
print_header "Summary"
if [ "$OVERALL_SUCCESS" = true ]; then
    print_status "All code quality checks passed!"
    echo -e "\n🚀 ${GREEN}Ready for commit/deployment!${NC}\n"
    exit 0
else
    print_error "Some code quality checks failed!"
    echo -e "\n❌ ${RED}Please fix the issues before committing.${NC}\n"
    exit 1
fi
