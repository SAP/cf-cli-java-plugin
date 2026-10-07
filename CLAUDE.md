# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a [Cloud Foundry CLI](https://github.com/cloudfoundry/cli) plugin written in Go that provides Java
diagnostics for CF-deployed applications. It exposes `cf java <subcommand> <app>` and communicates with the
JVM exclusively via `cf ssh`. Two external tools are embedded as binary blobs at build time:

- **jstall** (`dist/jstall-minimal.jar`) — JVM inspection tool (deadlock detection, hot threads, flame
  graphs). Requires Java 17+ locally.
- **hprof-redact** (`dist/hprof-redact-{platform}`) — strips sensitive data from heap dumps before writing
  to disk.

Both are extracted to `~/.cache/cf-java-plugin/` at runtime with SHA-256 change detection to avoid redundant extraction.

## Build Commands

Before the first build, download embedded binaries:

```bash
make download-jstall        # downloads dist/jstall-minimal.jar from GitHub releases
make download-hprof-redact  # downloads hprof-redact binaries for all platforms
```

Then build:

```bash
make compile                # builds build/cf-cli-java-plugin for current platform
make compile-all            # cross-compiles for Linux/macOS/Windows × amd64/arm64
make install                # compile + cf install-plugin (requires cf CLI logged in)
```

To use a development jstall build instead of the released version:

```bash
JSTALL_DEV=1 make compile          # latest GitHub Actions artifact
JSTALL_DEV=<run-id> make compile   # specific GHA run
```

## Running Tests

Go unit tests (no CF connection needed):

```bash
go test ./...              # all Go tests
go test -run TestFoo ./... # single test by name
```

Integration tests (require a live CF environment configured in `test/test_config.yml`):

```bash
cd test && ./setup.sh      # one-time venv setup
cd test && ./test.py all   # run all Python integration tests
cd test && ./test.py --start-with TestClass::test_method all  # resume from a specific test
```

## Linting

```bash
./scripts/lint-all.sh check   # check Go + Python + Markdown
./scripts/lint-all.sh fix     # auto-fix formatting issues
./scripts/lint-all.sh ci      # same as check, used in CI
```

Individual linters:

```bash
./scripts/lint-go.sh check|fix
./scripts/lint-python.sh check|fix
./scripts/lint-markdown.sh check|fix
```

Run `./setup-dev-env.sh` once to install pre-commit hooks that auto-run formatting on commit.

## Architecture

### Code Layout

| File | Role |
| --- | --- |
| `cf_cli_java_plugin.go` | Core plugin: `Run`/`GetMetadata` entry points, `Command` struct definitions, `execute()` dispatch, `Options`/flag parsing, SSH error wrapping |
| `jstall.go` | `jstall`/`status`/`record-status` commands: Java 17+ discovery, JAR extraction, `buildJstallArgs`, `executeJstall` |
| `redact.go` | Heap dump redaction: embedded binary extraction, `pipeHeapDumpThroughRedact` pipeline |
| `open.go` | `--open`/`--open-url`: one-shot local HTTP server (`serveFileOnce`), `buildOpenURL`, `openBrowser` |
| `utils/cfutils.go` | CF SSH helpers: `CopyOverCat`, `StreamOverCat`, `CopyOverCatGunzip`, `ProbeRemoteFileGzip`, `GetAvailablePath`, `FindReasonForAccessError`, fuzzy app-name search |
| `cmd/cmd.go` | `CommandExecutor` interface used to inject mock CF CLI calls in tests |

### Command Execution Flow

1. `Run()` → `DoRun()` → `execute()`
2. `execute()` parses flags into `Options`, resolves command name against the `commands []Command` slice,
   then dispatches based on `Command` fields.
3. Remote shell snippets are embedded as multi-line strings in each `Command.SSHCommand`. Variables
   (`@FILE_NAME`, `@FSPATH`, `@APP_NAME`, `@ARGS`, `@STATIC_FILE_NAME`) are expanded by
   `replaceVariables()` before execution.
4. File transfer from container → local machine goes over `cf ssh` via `cat` piped to stdout (see
   `utils.CopyOverCat` / `StreamOverCat`). There is no distinct stderr from `cf ssh`; both streams are
   merged.
5. For heap dumps: the plugin auto-detects whether the remote JDK supports `gz=1` (JDK 17+) by inspecting
   `jmap -h` output, enabling transparent compressed transfer. `--compress` keeps the local file as
   `.hprof.gz`; otherwise it decompresses on the fly via `CopyOverCatGunzip`.

### Embedded Binaries

`redact.go` and `jstall.go` use `//go:embed dist/...` directives — those `dist/` files **must exist
before `go build`**. The `compile` Make target lists them as prerequisites. If they are missing, the build
fails with "no matching files found".

### `Command` Struct Fields

The `commands` slice in `cf_cli_java_plugin.go` is the sole source of truth for available subcommands. Key fields:

- `GenerateFiles` — command creates a file on the container that must be downloaded
- `GenerateArbitraryFiles` / `GenerateArbitraryFilesFolderName` — command may create files in a named
  subdirectory (e.g. `jcmd`); all files in that folder are downloaded
- `RequiredTools` — tools (`jcmd`, `asprof`) that must be resolved on the container before running
- `IsLocal` — command runs locally (jstall, status, record-status) rather than via SSH
- `AcceptsTrailingArgs` — command takes positional arguments after the app name (e.g. `record-status APP outfile.zip`)
- `OnlyOnRecentSapMachine` — marks SapMachine-specific commands

### Testing Approach

Go unit tests in `cf_cli_java_plugin_test.go` and `open_test.go`/`redact_test.go` use a `fakeConn` stub
for `plugin.CliConnection`. They test flag parsing, error messages, and local logic without a real CF
connection. Integration tests in `test/` are Python (pytest) and require a configured CF environment.
