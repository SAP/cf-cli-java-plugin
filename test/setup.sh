#!/bin/bash
# CF Java Plugin Test Environment Setup Script
# Sets up Python virtual environment and dependencies

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/venv"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}✅${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ️${NC} $1"
}

show_help() {
    echo "CF Java Plugin Test Environment Setup"
    echo ""
    echo "Usage: $0 [--help|-h]"
    echo ""
    echo "This script sets up the Python virtual environment and installs dependencies"
    echo "required for running the CF Java Plugin test suite."
    echo ""
    echo "Options:"
    echo "  --help, -h    Show this help message"
    echo ""
}

# Parse arguments
case "${1:-}" in
    "--help"|"-h")
        show_help
        exit 0
        ;;
    "")
        # No arguments, proceed with setup
        ;;
    *)
        echo "❌ Unknown option: $1"
        echo ""
        show_help
        exit 1
        ;;
esac

print_info "Setting up CF Java Plugin test environment..."

# Create virtual environment if it doesn't exist
if [[ ! -d "$VENV_DIR" ]]; then
    print_info "Creating Python virtual environment..."
    python3 -m venv "$VENV_DIR"
else
    print_info "Virtual environment already exists"
fi

# Activate virtual environment
print_info "Activating virtual environment..."
source "$VENV_DIR/bin/activate"

# Upgrade pip
print_info "Upgrading pip..."
pip install --upgrade pip

# Install dependencies
print_info "Installing dependencies from requirements.txt..."
pip install -r "$SCRIPT_DIR/requirements.txt"

print_status "Setup complete!"
echo ""
echo "To activate the virtual environment manually:"
echo "  source $VENV_DIR/bin/activate"
echo ""
echo "To run tests:"
echo "  ./test.py all"
echo ""
echo "To clean artifacts:"
echo "  ./test.py clean"
