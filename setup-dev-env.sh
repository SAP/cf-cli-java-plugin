#!/bin/bash

# Setup script for CF Java Plugin development environment
# Installs pre-commit hooks and validates the development setup

echo "🚀 Setting up CF Java Plugin development environment"
echo "====================================================="

# Check if we're in the right directory
if [ ! -f "cf_cli_java_plugin.go" ]; then
    echo "❌ Error: Not in the CF Java Plugin root directory"
    exit 1
fi

echo "✅ In correct project directory"

# Install pre-commit hook
echo "📦 Installing pre-commit hooks..."
if [ ! -f ".git/hooks/pre-commit" ]; then
    echo "❌ Error: Pre-commit hook file not found"
    echo "This script should be run from the repository root where .git/hooks/pre-commit exists"
    exit 1
fi

chmod +x .git/hooks/pre-commit
echo "✅ Pre-commit hooks installed"

# Setup Go environment
echo "🔧 Checking Go environment..."
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.23.5 or later."
    exit 1
fi

GO_VERSION=$(go version | grep -o 'go[0-9]\+\.[0-9]\+' | head -1)
echo "✅ Go version: $GO_VERSION"

# Install Go dependencies
echo "📦 Installing Go dependencies..."
go mod tidy
echo "✅ Go dependencies installed"

# Setup Python environment (if test suite exists)
if [ -f "test/requirements.txt" ]; then
    echo "🐍 Setting up Python test environment..."
    cd test
    
    if [ ! -d "venv" ]; then
        echo "Creating Python virtual environment..."
        python3 -m venv venv
    fi
    
    source venv/bin/activate
    pip3 install --upgrade pip
    pip3 install -r requirements.txt
    echo "✅ Python test environment ready"
    cd ..
else
    echo "⚠️  Python test suite not found - skipping Python setup"
fi

# VS Code setup validation
if [ -f "cf-java-plugin.code-workspace" ]; then
    echo "✅ VS Code workspace configuration found"
    if [ -f "./test-vscode-config.sh" ]; then
        echo "🔧 Running VS Code configuration test..."
        ./test-vscode-config.sh
    fi
else
    echo "⚠️  VS Code workspace configuration not found"
fi

# Test the pre-commit hook
echo ""
echo "🧪 Testing pre-commit hook..."
echo "This will run all checks without committing..."
if .git/hooks/pre-commit; then
    echo "✅ Pre-commit hook test passed"
else
    echo "❌ Pre-commit hook test failed"
    echo "Please fix the issues before proceeding"
    exit 1
fi

echo ""
echo "🎉 Development Environment Setup Complete!"
echo "=========================================="
echo ""
echo "📋 What's configured:"
echo "  ✅ Pre-commit hooks (run on every git commit)"
echo "  ✅ Go development environment"
if [ -f "test/requirements.txt" ]; then
    echo "  ✅ Python test suite environment"
else
    echo "  ⚠️  Python test suite (not found)"
fi
if [ -f "cf-java-plugin.code-workspace" ]; then
    echo "  ✅ VS Code workspace with debugging support"
fi

echo "Setup Python Testing Environment:"
(cd test && ./test.sh setup)

echo ""
echo "🚀 Quick Start:"
echo "  • Build plugin:        make build"
if [ -f "test/requirements.txt" ]; then
    echo "  • Run Python tests:    cd test && ./test.sh all"
    echo "  • VS Code debugging:   code cf-java-plugin.code-workspace"
fi
echo "  • Manual hook test:    .git/hooks/pre-commit"
echo ""
echo "📚 Documentation:"
echo "  • Main README:         README.md"
if [ -f "test/README.md" ]; then
    echo "  • Test documentation:  test/README.md"
fi
if [ -f ".vscode/README.md" ]; then
    echo "  • VS Code guide:       .vscode/README.md"
fi
echo ""
echo "Happy coding! 🎯"
