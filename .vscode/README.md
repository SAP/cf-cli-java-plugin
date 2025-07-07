# VS Code Development Setup

This directory contains a comprehensive VS Code configuration for developing and testing the CF Java Plugin test suite.

## Quick Start

1. **Open the workspace**: Use the workspace file in the root directory:
   ```bash
   code ../cf-java-plugin.code-workspace
   ```

2. **Install recommended extensions**: VS Code will prompt you to install recommended extensions.

3. **Setup environment**: Run the setup task from the Command Palette:
   - `Ctrl/Cmd + Shift + P` → "Tasks: Run Task" → "Setup Virtual Environment"

## Features

### 🚀 Launch Configurations (F5 or Debug Panel)

- **Debug Current Test File** - Debug the currently open test file
- **Debug Current Test Method** - Debug a specific test method (prompts for class/method)
- **Debug Custom Filter** - Debug tests matching a custom filter pattern
- **Run All Tests** - Run the entire test suite
- **Run Basic Commands Tests** - Run only basic command tests
- **Run JFR Tests** - Run only JFR (Java Flight Recorder) tests
- **Run Async-profiler Tests** - Run only async-profiler tests (SapMachine)
- **Run Integration Tests** - Run full integration tests
- **Run Heap Tests** - Run all heap-related tests
- **Run Profiling Tests** - Run all profiling tests (JFR + async-profiler)
- **Interactive Test Runner** - Launch the interactive test runner

### ⚡ Tasks (Ctrl/Cmd + Shift + P → "Tasks: Run Task")

#### Test Execution
- **Run All Tests** - Execute all tests
- **Run Current Test File** - Run the currently open test file
- **Run Basic Commands Tests** - Basic command functionality
- **Run JFR Tests** - Java Flight Recorder tests
- **Run Async-profiler Tests** - Async-profiler tests
- **Run Integration Tests** - Full integration tests
- **Run Heap Tests (Pattern)** - Tests matching "heap"
- **Run Profiling Tests (Pattern)** - Tests matching "jfr or asprof"
- **Run Tests in Parallel** - Parallel test execution
- **Generate HTML Test Report** - Create HTML test report

#### Development Tools
- **Setup Virtual Environment** - Initialize/setup the Python environment
- **Clean Test Artifacts** - Clean up test files and artifacts
- **Interactive Test Runner** - Launch interactive test selector
- **Install/Update Dependencies** - Update Python packages

### 🔧 Integrated Settings

- **Python Environment**: Automatic virtual environment detection (`./venv/bin/python`)
- **Test Discovery**: Automatic pytest test discovery
- **Formatting**: Black formatter with 120-character line length
- **Linting**: Flake8 with custom rules
- **Type Checking**: Basic type checking enabled
- **Import Organization**: Automatic import sorting on save

### 📝 Code Snippets

Type these prefixes and press Tab for instant code generation:

- **`cftest`** - Basic CF Java test method
- **`cfheap`** - Heap dump test template
- **`cfjfr`** - JFR test template
- **`cfasprof`** - Async-profiler test template
- **`cftestclass`** - Test class template
- **`cfimport`** - Import test framework
- **`cfmulti`** - Multi-step workflow test
- **`cfsleep`** - Time.sleep with comment
- **`cfcleanup`** - Test cleanup code

## Test Organization & Filtering

### By File
```bash
pytest test_basic_commands.py -v    # Basic commands
pytest test_jfr.py -v              # JFR tests
pytest test_asprof.py -v            # Async-profiler tests
pytest test_cf_java_plugin.py -v    # Integration tests
```

### By Test Class
```bash
pytest test_basic_commands.py::TestHeapDump -v      # Only heap dump tests
pytest test_jfr.py::TestJFRBasic -v                 # Basic JFR functionality
pytest test_asprof.py::TestAsprofProfiles -v        # Async-profiler profiles
```

### By Pattern
```bash
pytest -k "heap" -v                 # All heap-related tests
pytest -k "jfr or asprof" -v        # All profiling tests
```

### By Markers
```bash
pytest -m "sapmachine21" -v         # SapMachine-specific tests
```

## Debugging Tips

1. **Set Breakpoints**: Click in the gutter or press F9
2. **Step Through**: Use F10 (step over) and F11 (step into)
3. **Inspect Variables**: Hover over variables or use the Variables panel
4. **Debug Console**: Use the Debug Console for live evaluation
5. **Conditional Breakpoints**: Right-click on breakpoint for conditions

## Test Execution Patterns

### Quick Development Cycle
1. Edit test file
2. Press F5 → "Debug Current Test File"
3. Fix issues and repeat

### Focused Testing
1. Use custom filter: F5 → "Debug Custom Filter"
2. Enter pattern like "heap and download"
3. Debug only matching tests

## File Organization

```
test/
├── .vscode/                    # VS Code configuration
│   ├── launch.json            # Debug configurations
│   ├── tasks.json             # Build/test tasks
│   ├── settings.json          # Workspace settings
│   ├── extensions.json        # Recommended extensions
│   └── python.code-snippets   # Code snippets
├── framework/                 # Test framework
├── test_*.py                  # Test modules
├── requirements.txt           # Dependencies
├── setup.sh                   # Environment setup script
└── test_runner.py            # Interactive test runner
```

## Keyboard Shortcuts

- **F5** - Start debugging
- **Ctrl/Cmd + F5** - Run without debugging
- **Shift + F5** - Stop debugging
- **F9** - Toggle breakpoint
- **F10** - Step over
- **F11** - Step into
- **Ctrl/Cmd + Shift + P** - Command palette
- **Ctrl/Cmd + `** - Open terminal

## Troubleshooting

### Python Environment Issues
1. Ensure virtual environment is created: Run "Setup Virtual Environment" task
2. Check Python interpreter: Bottom left corner should show `./venv/bin/python`
3. Reload window: Ctrl/Cmd + Shift + P → "Developer: Reload Window"

### Test Discovery Issues
1. Save all files (tests auto-discover on save)
2. Check PYTHONPATH in terminal
3. Verify test files follow `test_*.py` naming

### Extension Issues
1. Install recommended extensions when prompted
2. Check Extensions panel for any issues
3. Restart VS Code if needed

## Advanced Features

### Parallel Testing
Use the "Run Tests in Parallel" task for faster execution on multi-core systems.

### HTML Reports
Generate comprehensive HTML test reports with the "Generate HTML Test Report" task.

### Interactive Runner
Launch `test_runner.py` for menu-driven test selection and execution.
