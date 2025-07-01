# CF Java Plugin Test Suite

A modern, efficient testing framework for the CF Java Plugin using Python and pytest.

## Quick Start

```bash
# Setup
./test.py setup

# Run tests
./test.py all           # Run all tests
./test.py basic         # Basic commands
./test.py jfr           # JFR tests
./test.py asprof        # Async-profiler (SapMachine)
./test.py profiling     # All profiling tests

# Common options
./test.py --failed all                        # Re-run failed tests
./test.py --html basic                        # Generate HTML report
./test.py --parallel all                      # Parallel execution
./test.py --fail-fast all                     # Stop on first failure
./test.py --no-initial-restart all            # Skip app restarts (faster)
./test.py --stats all                         # Enable CF command statistics
./test.py --start-with TestClass::test_method all  # Start with a specific test (inclusive)
```

## State of Testing

- `heap-dump` is thoroughly tested, including all flags, so that less has to be tested for the other commands.

## Test Discovery

Use the `list` command to explore available tests:

```bash
# Show all tests with class prefixes (ready to copy/paste)
./test.py list

# Show only method names without class prefixes
./test.py list --short

# Show with line numbers and docstrings
./test.py list --verbose

# Show only application names used in tests
./test.py list --apps-only
```

Example output:

```text
📁 test_asprof.py
  📋 TestAsprofBasic - Basic async-profiler functionality.
    🎯 App: sapmachine21
      • TestAsprofBasic::test_status_no_profiling
      • TestAsprofBasic::test_cpu_profiling
```

## Test Files

- **`test_basic_commands.py`** - Core commands (heap-dump, vm-info, thread-dump, etc.)
- **`test_jfr.py`** - Java Flight Recorder profiling tests
- **`test_asprof.py`** - Async-profiler tests (SapMachine only)
- **`test_cf_java_plugin.py`** - Integration and workflow tests
- **`test_disk_full.py`** - Tests for disk full scenarios (e.g., heap dump with no space left)

## Test Selection & Execution

### Run Specific Tests

```bash
# Copy test name from `./test.py list` and run directly
./test.py run TestAsprofBasic::test_cpu_profiling

# Run by test class
./test.py run test_asprof.py::TestAsprofBasic

# Run by file
./test.py run test_basic_commands.py

# Search by pattern
./test.py run test_cpu_profiling
```

### Test Resumption

After interruption or failure, the CLI shows actionable suggestions:

```bash
❌ Tests failed
💡 Use --failed to re-run only failed tests
💡 Use --start-with TestClass::test_method to resume from a specific test (inclusive)
```

## Application Dependencies

Tests are organized by application requirements:

- **`all`** - Tests that run on any Java application (sapmachine21)
- **`sapmachine21`** - Tests specific to SapMachine (async-profiler support)

## Key Features

### CF Command Statistics

```bash
./test.py --stats all   # Track all CF commands with performance insights
```

### Environment Variables

```bash
export RESTART_APPS="never"           # Skip app restarts (faster)
export CF_COMMAND_STATS="true"        # Global command tracking
```

### Fast Development Mode

```bash
# Skip app restarts for faster test iterations
./test.py --no-initial-restart basic

# Stop immediately on first failure
./test.py --fail-fast all

# Combine for fastest feedback
./test.py --no-initial-restart --fail-fast basic
```

### Parallel Testing

Tests are automatically grouped by app to prevent interference:

```bash
./test.py --parallel all    # Safe parallel execution
```

### HTML Reports

```bash
./test.py --html all        # Generate detailed HTML test report
```

## Development

```bash
./test.py setup         # Setup environment
./test.py clean         # Clean artifacts
```

## Test Framework

The framework uses a decorator-based approach:

```python
from framework.decorators import test
from framework.runner import TestBase

class TestExample(TestBase):
    @test("all")  # or @test("sapmachine21")
    def test_heap_dump_basic(self, t, app):
        t.heap_dump("--local-dir .") \
            .should_succeed() \
            .should_create_file(f"{app}-heapdump-*.hprof")
```


## Tips

1. **Start with `./test.py list`** to see all available tests
2. **Use `--apps-only`** to see which applications are needed
3. **Copy test names directly** from the list output to run specific tests
4. **Use `--failed`** to quickly re-run only failed tests after fixing issues
5. **Use `--parallel`** for faster execution of large test suites
6. **Use `--html`** to get detailed reports with logs and timing information
