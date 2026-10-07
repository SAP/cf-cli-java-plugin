[![REUSE status](https://api.reuse.software/badge/github.com/SAP/cf-cli-java-plugin)](https://api.reuse.software/info/github.com/SAP/cf-cli-java-plugin)
[![Build and Snapshot Release](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/build-and-snapshot.yml/badge.svg)](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/build-and-snapshot.yml)
[![PR Validation](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/pr-validation.yml/badge.svg)](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/pr-validation.yml)

# Cloud Foundry Command Line Java plugin

This plugin for the [Cloud Foundry Command Line](https://github.com/cloudfoundry/cli) provides convenience utilities to
work with Java applications deployed on Cloud Foundry by the [SapMachine](https://sapmachine.io) team.

Currently, it allows you to:

- Capture heap dumps and thread dumps from a running Cloud Foundry Java application
- Run `jcmd` remotely against your application
- Start, stop, and retrieve JFR and [async-profiler](https://github.com/jvm-profiling-tools/async-profiler)
  ([SapMachine](https://sapmachine.io) only) profiles
- Run [jstall](https://github.com/parttimenerd/jstall) for one-shot JVM inspection (deadlock detection, hot threads,
  dependency graphs, and more): bundled directly in the plugin, requires Java 17+ locally
- Redact heap dumps before saving to remove sensitive data (`--redact`, `--redact-complete`) using the bundled
  [`hprof-redact`](https://github.com/parttimenerd/hprof-analyzer) binary from the
  [`hprof-analyzer`](https://github.com/parttimenerd/hprof-analyzer) project
- Automatically compress heap dump transfers over SSH on JDK 17+ containers;
  use `--compress` to keep the local file as `.hprof.gz`
- Open heap dumps directly in the hosted
  [`hprof-analyzer`](https://parttimenerd.github.io/hprof-analyzer) web app after downloading (`--open`) or point the
  plugin at another `hprof-analyzer` instance via `--open-url`

## Installation

### Installation via CF Community Repository

Make sure you have the CF Community plugin repository configured (or add it via
`cf add-plugin-repo CF-Community http://plugins.cloudfoundry.org`)

Trigger installation of the plugin via

```sh
cf install-plugin java
```

The releases in the community repository are older than the actual releases on GitHub, that you can install manually, so
we recommend the manual installation.

### Manual Installation

Download the latest release from [GitHub](https://github.com/SAP/cf-cli-java-plugin/releases/latest).

To install a new version of the plugin, run the following:

```sh
# on Mac arm64 (Apple Silicon only)
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-macos-arm64
# on Windows amd64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-windows-amd64.exe
# on Linux amd64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-linux-amd64
# on Linux arm64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-linux-arm64
```

macOS plugin binaries currently require Apple Silicon; macOS Intel (`darwin/amd64`) is not supported.

You can verify that the plugin is successfully installed by looking for `java` in the output of `cf plugins`.

### Manual Installation of Snapshot Release

Download the current snapshot release from [GitHub](https://github.com/SAP/cf-cli-java-plugin/releases/tag/snapshot).
This is intended for experimentation and might fail.

To install a new version of the plugin, run the following:

```sh
# on Mac arm64 (Apple Silicon only)
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-macos-arm64
# on Windows amd64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-windows-amd64.exe
# on Linux amd64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-linux-amd64
# on Linux arm64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-linux-arm64
```

macOS snapshot binaries currently require Apple Silicon; macOS Intel (`darwin/amd64`) is not supported.

## Common Tasks

### My CF app is not responding — find what it is stuck on

Run `jstall` first — it detects deadlocks, identifies BLOCKED threads, and shows what
each thread is waiting for:

```bash
cf java jstall $APP_NAME
```

If there is a deadlock, it will be listed at the top with the cycle of threads and monitors.
For a focused deadlock check only:

```bash
cf java jstall $APP_NAME --args 'deadlock all'
```

If no deadlock, look for threads in `BLOCKED` state and the monitor they are waiting on.
The thread that *holds* that monitor is the bottleneck. For a plain thread dump:

```bash
cf java thread-dump $APP_NAME
```

### My CF app is using too much CPU

`most-work` takes repeated thread dumps and ranks threads by on-CPU frequency —
no async-profiler needed:

```bash
cf java jstall $APP_NAME --args 'most-work --dumps 5 all'
```

For a proper CPU flame graph (slower, but much more detail):

```bash
cf java jstall $APP_NAME --args 'flame all'
# Downloads an HTML flamegraph to your current directory
```

Or use the two-step async-profiler approach to capture a specific window:

```bash
cf java asprof-start-cpu $APP_NAME
# reproduce the slow operation or wait 30–60 s
cf java asprof-stop $APP_NAME
# Downloads $APP_NAME-asprof-<random>.jfr — open in JDK Mission Control
```

### My CF app crashed with OutOfMemoryError — take a heap dump

Take a heap dump from the running (or restarted) instance and download it:

```bash
cf java heap-dump $APP_NAME
# Downloads $APP_NAME-heapdump-<random>.hprof to current directory
```

Analyse with [hprof-analyzer](https://github.com/parttimenerd/hprof-analyzer) for Leak Suspects and Top Consumers:

```bash
hprof-analyzer $APP_NAME-heapdump-*.hprof report.html
# Open report.html → "Leak Suspects" and "Top Consumers" tabs
```

**Note:** On JRE-only containers without `jmap`, heap dumps are taken via the HotSpot attach socket —
see [JRE-only containers](#jre-only-containers) below.

## Usage

### Prerequisites

#### Container Requirements

Most commands work out of the box on any Java container. The table below shows what each command needs:

| Command | JDK tools needed | JRE-only fallback |
|---|---|---|
| `heap-dump` | `jmap` (or `jvmmon` on SapMachine) | `nc -U` (netcat-openbsd / nmap-ncat) |
| `thread-dump` | `jstack` (or `jvmmon` on SapMachine) | `nc -U` |
| `vm-info`, `vm-version` | `jcmd` | `nc -U` |
| `jcmd` | `jcmd` | `nc -U` |
| `jfr-*` | `jcmd` | — (JFR requires JDK) |
| `asprof-*` | `asprof` (SapMachine / manual install) | — |
| `status`, `jstall`, `record-status` | none (runs locally via jstall) | works on any JRE |

#### JRE-only containers

The Cloud Foundry Java Buildpack deploys a JRE by default (no `jmap`, `jstack`, or `jcmd`). Most `cf java` commands
now work on JRE-only containers via the HotSpot attach socket: the plugin sends jcmd-protocol requests directly to
the JVM over a Unix-domain socket using `nc`.

**Requirement:** `netcat-openbsd` or `nmap-ncat` must be installed in the container (provides `nc` with `-U` support).
Most Debian/Ubuntu-based containers include `netcat-openbsd` by default. If it is missing, install it via:

```yaml
env:
  VCAP_SERVICES_PACKAGE_INSTALL: "netcat-openbsd"
```

Or in your Dockerfile/buildpack configuration: `apt-get install -y netcat-openbsd`.

Commands that require a full JDK and have no nc fallback are `jfr-*` (JFR is a JDK feature) and `asprof-*`
(async-profiler must be installed separately). Use `jstall`-based commands for diagnostics on JRE-only containers.

To use a full JDK instead (gives access to all commands), set `JBP_CONFIG_OPEN_JDK_JRE` in your manifest:

```yaml
---
applications:
  - name: <APP_NAME>
    buildpack: https://github.com/cloudfoundry/java-buildpack
    env:
      JBP_CONFIG_OPEN_JDK_JRE:
        ‘{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64", version: 21.+ } }’
      JBP_CONFIG_JAVA_OPTS: "[java_opts: ‘-XX:+UnlockDiagnosticVMOptions -XX:+DebugNonSafepoints’]"
```

Note: this requires an online buildpack (`buildpack` property), not a system buildpack (system buildpacks don’t
cache JDK artifacts and will fail staging).

#### SSH Access

As it is built directly on `cf ssh`, the `cf java` plugin can work only with Cloud Foundry applications that have
`cf ssh` enabled. To check if your app fulfills the requirements, you can find out by running the
`cf ssh-enabled [app-name]` command. If not enabled yet, run `cf enable-ssh [app-name]`.

**Note:** You must restart your app after enabling SSH access.

In case a proxy server is used, ensure that `cf ssh` is configured accordingly. Refer to the
[official documentation](https://docs.cloudfoundry.org/cf-cli/http-proxy.html#v3-ssh-socks5) of the Cloud Foundry
Command Line for more information. If `cf java` is having issues connecting to your app, chances are the problem is in
the networking issues encountered by `cf ssh`. To verify, run your `cf java` command in "dry-run" mode by adding the
`--dry-run` flag and try to execute the command line that `cf java` gives you back. The plugin now wraps common SSH
failures with user-facing guidance instead of printing the raw `ssh` error verbatim, so running the generated `cf ssh`
command directly is the quickest way to inspect the underlying transport error. If that direct command fails, the issue
is not in `cf java`, but in whatever makes `cf ssh` fail.

### Examples

Getting a heap dump:

```sh
# Basic — plain .hprof saved locally.
# On JDK 17+ containers, transfer is always gzip-compressed automatically (faster on slow connections).
cf java heap-dump $APP_NAME

# Redact sensitive values (passwords, tokens, personal data) before saving
cf java heap-dump $APP_NAME --redact            # lean: zeros primitive arrays
cf java heap-dump $APP_NAME --redact-complete   # complete: zeros all primitive values

# Keep the local file compressed as .hprof.gz (transfer is already compressed on JDK 17+)
cf java heap-dump $APP_NAME --compress

# Redact and keep compressed
cf java heap-dump $APP_NAME --redact --compress

# Open in hprof-analyzer web app after downloading (spins up a local server, opens browser)
cf java heap-dump $APP_NAME --open

# Open with redaction and compression applied first
cf java heap-dump $APP_NAME --open --redact --compress

# Open using a locally running hprof-analyzer instance
cf java heap-dump $APP_NAME --open-url http://localhost:8080
```

The browser integration uses the [`hprof-analyzer`](https://github.com/parttimenerd/hprof-analyzer) project. By
default, `--open` launches the hosted web app at <https://parttimenerd.github.io/hprof-analyzer>. Use `--open-url` if
you run your own local or internal `hprof-analyzer` deployment.

> **macOS note:** On macOS with the Application Firewall enabled, a dialog will appear asking
> *"Do you want the application 'cf-cli-java-plugin' to accept incoming network connections?"*
> Click **Allow** — the plugin binds a temporary local server on `127.0.0.1` to serve the file
> to the browser. The server serves only the exact one-time heap-dump URL generated for that download,
> rejects alternate paths or query parameters, and shuts down automatically after one successful fetch.

Getting a thread dump:

```sh
cf java thread-dump $APP_NAME
```

Creating a CPU profile via async-profiler:

```sh
cf java asprof-start-cpu $APP_NAME
# wait some time to gather data
cf java asprof-stop $APP_NAME
```

Running arbitrary jcmd commands, like `VM.uptime`:

```sh
cf java jcmd $APP_NAME --args 'VM.uptime'
```

Quick status check of the remote JVM (requires Java 17+ locally):

```sh
cf java status $APP_NAME
```

Running [JStall](https://github.com/parttimenerd/jstall) for more specific JVM inspection (requires Java 17+ locally):

```sh
# Default: run status analysis with deadlock detection, hot threads, etc.
> cf java jstall $APP_NAME

# Run a specific jstall subcommand
> cf java jstall $APP_NAME --args 'deadlock all'
> cf java jstall $APP_NAME --args 'most-work --dumps 3 all'
> cf java jstall $APP_NAME --args 'flame all'
```

> **Tip:** You can also use JStall directly (without this plugin) via its `--cf` option:
>
> ```sh
> jstall --cf $APP_NAME status all
> ```
>
> This is useful if you want to use a newer JStall version than the one bundled in the plugin. See the
> [JStall README](https://github.com/parttimenerd/jstall) for installation and usage.

Recording JVM diagnostic data for later analysis or sharing:

```sh
# Record all JVM diagnostic data into a zip file (default: APP_NAME-status.zip)
> cf java record-status $APP_NAME

# Record to a specific output file
> cf java record-status $APP_NAME diagnostics.zip

# Record with full data (including expensive jcmd commands, flame graph, and JFR)
> cf java record-status $APP_NAME --full

# Replay the recording locally with jstall
> jstall -f diagnostics.zip status all
> jstall -f diagnostics.zip threads all
```

#### Variable Replacements for JCMD and Asprof Commands

When using `jcmd` and `asprof` commands with the `--args` parameter, the following variables are automatically replaced
in your command strings:

- `@FSPATH`: A writable directory path on the remote container (always set, typically `/tmp/jcmd` or `/tmp/asprof`)
- `@ARGS`: The command arguments you provided via `--args`
- `@APP_NAME`: The name of your Cloud Foundry application
- `@FILE_NAME`: Generated filename for file operations (includes full path with UUID)

Example usage:

```sh
# Create a heap dump in the available directory
cf java jcmd $APP_NAME --args 'GC.heap_dump @FSPATH/my_heap.hprof'

# Use an absolute path instead
cf java jcmd $APP_NAME --args "GC.heap_dump /tmp/absolute_heap.hprof"

# Access the application name in your command
cf java jcmd $APP_NAME --args 'echo "Processing app: @APP_NAME"'
```

**Note**: Variables use the `@` prefix to avoid shell expansion issues. The plugin automatically creates the `@FSPATH`
directory and downloads any files created there to your local directory (unless `--no-download` is used).

### Commands

The following is a list of all available commands (some are SapMachine-specific), generated via `cf java --help`:

<!-- prettier-ignore-start -->
<!-- markdownlint-disable MD036 -->

*Run `cf java --help` to see the full list of commands.*

<!-- markdownlint-enable MD036 -->
<!-- prettier-ignore-end -->

### Security Note on `--args`

The `--args` parameter passes values directly into remote shell commands via `cf ssh`. This is by design to support
shell features like environment variable expansion and piping. **Do not pass untrusted input to `--args`** — treat it
with the same caution as a shell command.

### File Output

The heap dumps and profiles will be downloaded to a local file automatically (to the current directory by default). Use
`--local-dir` to specify a different download location. To save disk space of the application container, the files are
automatically deleted unless the `--keep` option is set.

Providing `--container-dir` is optional. If specified, the plugin will create the heap dump or profile at that path
inside the application container. Without it, the file is created at `/tmp` or at the mount point of an attached
file system service.

```shell
cf java [heap-dump|jfr-stop|jfr-dump|asprof-stop] [my-app] --local-dir /local/path [--container-dir /var/fspath]
```

Thread dumps are streamed to stdout. To save one to a file:

```shell
cf java thread-dump [my_app] -i [my_instance_index] > thread-dump.txt
```

The `--keep` flag is not applicable to commands that stream output directly (e.g., `thread-dump`).

Heap dumps support additional local post-processing and analysis options:

- `--redact`: lean redaction mode; streams the heap dump through `hprof-redact` and zeros primitive arrays such as
  `byte[]`, `char[]`, and similar bulk buffers
- `--redact-complete`: complete redaction mode; streams the heap dump through `hprof-redact` and zeros primitive arrays
  and individual primitive fields
- `--redact-keep-on-error`: keeps a partially written redacted output file if local redaction fails; otherwise failed
  redaction leaves no local heap dump behind
- `--compress`: keeps the local output as `.hprof.gz` instead of transparently decompressing it
- `--open`: starts a temporary local HTTP server on `127.0.0.1`, serves the downloaded heap dump once, and opens
  [`hprof-analyzer`](https://parttimenerd.github.io/hprof-analyzer) automatically in your browser
- `--open-url <URL>`: same as `--open`, but targets a custom hosted or self-managed `hprof-analyzer` instance

These features can be combined, for example: `cf java heap-dump APP --redact --compress --open`.
`--open` requires a local file and therefore cannot be used with `--no-download`.

### Heap Dump Privacy

Heap dumps contain the full in-memory state of a JVM, including strings, byte arrays, and field values, which can
hold passwords, tokens, session data, or personal information. Before sharing a dump outside a trusted environment,
use `--redact` or `--redact-complete` to zero out sensitive values.

| Flag                | What gets zeroed                                                                             |
| ------------------- | -------------------------------------------------------------------------------------------- |
| `--redact`          | Primitive arrays (`byte[]`, `char[]`, `int[]`, …) — covers most strings and serialized data  |
| `--redact-complete` | All primitive arrays **and** individual primitive fields — maximum privacy                   |

Both modes preserve the full object graph (class names, references, instance counts), so the dump remains useful for
memory analysis. The two flags are mutually exclusive.

The redacted file is saved to the requested local heap-dump path with no extra suffix. When redaction is enabled, the
heap dump is streamed directly into `hprof-redact` exactly as downloaded, including gzip-compressed `.hprof.gz`
streams, so the unredacted dump is never written to local disk.
Use `--redact --compress` to also compress the output (produces a `.hprof.gz`).

Redaction runs locally via the bundled [hprof-redact](https://github.com/parttimenerd/hprof-analyzer) binary while the
dump is being downloaded. The binary is embedded from the
[`hprof-analyzer`](https://github.com/parttimenerd/hprof-analyzer) project, so no separate installation is required.

### Compressed Transfer

When bandwidth or container disk space is a concern, use `--compress` to transfer the dump in gzip format.

- On **JDK 17+**: `jmap` (or the nc fallback) compresses the dump on the container before transfer; the
  local file is saved as `.hprof.gz`.
- On **JDK < 17**: the container JDK does not support `gz=1`; a warning is printed and the dump is downloaded
  uncompressed as usual.

Without `--compress`, the plugin still uses `gz=1` automatically when the remote JDK supports it — the transfer is
compressed but the local file is transparently decompressed to a plain `.hprof`. This is the default behaviour
starting from JDK 17 and costs nothing from the user's perspective.

### Opening a Heap Dump in hprof-analyzer

Use `--open` to inspect the downloaded heap dump immediately in
[`hprof-analyzer`](https://github.com/parttimenerd/hprof-analyzer), either via the hosted instance at
<https://parttimenerd.github.io/hprof-analyzer> or via your own deployment with `--open-url`.

For safety, the plugin does **not** expose an arbitrary local directory. Instead, it starts a temporary local HTTP
server bound to `127.0.0.1`, serves only the exact generated heap-dump for that one download, rejects alternate
paths and query parameters, and shuts the server down automatically after one successful browser fetch or after a
timeout if the browser never connects.

## Limitations

Some commands depend on writable filesystem space inside the application container. In particular, `cf java heap-dump`,
`cf java asprof-stop`, and `cf java jfr-stop` first create a file in the container, then stream that file back over SSH,
and finally remove it again unless the `--keep` flag is set.

The available container filesystem space is controlled by the Cloud Foundry landscape configuration and may be limited.
Heap dumps can be large, roughly scaling with heap usage, and profile files can also grow substantially depending on
recording duration and settings. If the container does not have enough free space, the dump or recording cannot be
created and the command will fail.

The plugin uses `pidof java` to locate the target JVM inside the container. If a container runs multiple Java processes,
the plugin may target the wrong one. In such cases, use `jcmd` with an explicit PID obtained via
`cf ssh APP -c 'ps aux | grep java'`.

The plugin is also constrained by limitations in the current `cf-cli` plugin framework:

- `CF_TRACE=true` will break file-producing commands (`heap-dump`, `jfr-stop`, `asprof-stop`, `jcmd`). Disable
  `CF_TRACE` before using these commands, or use `--dry-run` and run the generated command directly.
- There is no distinction between `stdout` and `stderr` output from the underlying `cf ssh` command (see
  [this issue on the `cf-cli` project](https://github.com/cloudfoundry/cli/issues/1074))
  - `cf java` will still usually exit with status code `1` when the underlying `cf ssh` command fails
  - If you need separate `stdout` and `stderr`, run the plugin in dry-run mode (`--dry-run`) and execute the generated
    command directly

### Known Command Limitations

#### jstall Flame Graph May Fail in Containerized Environments

The `jstall flame` command may fail with an error like:

```text
Error: profiling was skipped: profiling-failed
jstall execution failed: exit status 1
```

**Reason:** Flame graph generation depends on low-level profiling capabilities such as `perf` events. These are often
restricted in containerized environments for security reasons.

**Workarounds:**

1. Use async-profiler directly for CPU profiling:

   ```bash
   cf java asprof-start-cpu $APP_NAME
   # ... wait for profiling ...
   cf java asprof-stop $APP_NAME
   ```

2. Use other jstall commands that don't require perf:

   ```bash
   cf java jstall $APP_NAME --args 'status all'      # JVM status & diagnostics
   cf java jstall $APP_NAME --args 'deadlock all'    # Deadlock detection
   cf java jstall $APP_NAME --args 'threads all'     # Thread information
   ```

3. Record diagnostic data for later analysis:

   ```bash
   cf java record-status $APP_NAME diagnostics.zip
   # Then replay locally
   jstall -f diagnostics.zip status all
   ```

#### SSH Connection Failures

When the plugin cannot establish SSH connectivity, it reports a categorized error message with likely causes and
suggested next steps instead of only printing the raw `cf ssh` transport error. A typical message looks like:

```text
Cannot connect to app 'APP_NAME' via SSH.
Possible causes and solutions:
1. SSH may not be enabled on the application. Try:
   cf enable-ssh APP_NAME
   cf restart APP_NAME
2. Check your network connection and firewall settings.
3. Verify the application is running: cf app APP_NAME
```

**Common causes and fixes:**

| Error                                 | Likely Cause                   | Solution                                                    |
| ------------------------------------- | ------------------------------ | ----------------------------------------------------------- |
| `connection refused` or `not enabled` | SSH not enabled on application | `cf enable-ssh APP_NAME && cf restart APP_NAME`             |
| `connection reset`                    | Network interruption           | Retry the command; check internet connection                |
| `timeout`                             | Network unreachable            | Check firewall/proxy settings; verify platform connectivity |
| `Permission denied`                   | Authentication failed          | `cf logout && cf login` with correct credentials            |

**Debugging:** If you need the original SSH transport error from Cloud Foundry, run `cf ssh APP_NAME -c 'echo ok'`
directly.

## Side-effects on the running instance

Creating dumps and profile recordings consumes container filesystem space. If too much space is used, other operations
inside the container (for example, writing temporary files) may fail, which can lead to unexpected application errors.

### Thread-Dumps

Capturing a thread dump with `cf java` usually has low overhead on the JVM, unless the process has a very large number
of threads.

### Heap-Dumps

Heap dumps require more care. Triggering a heap dump typically causes a full GC, during which the JVM can become
temporarily unresponsive. The overall impact depends on heap size (larger heaps usually take longer), GC behavior, and
especially whether the container is swapping memory to disk. Swapping is generally very costly for JVM performance.

Because Cloud Foundry cells can be overcommitted, a container may begin swapping during dump generation (or may already
be swapping before it starts). In stressed conditions, generating a heap dump can therefore further degrade application
performance.

### Profiles

Profiling introduces overhead that depends on the selected mode and settings, but default configurations are usually
designed to keep that overhead moderate.

## Development

### Quick Start

```bash
# Setup environment and build
./setup-dev-env.sh
make build

# Run all quality checks and tests
./scripts/lint-all.sh ci

# Auto-fix formatting before commit
./scripts/lint-all.sh fix
```

### Build Configuration

**JStall Version**: By default, the build downloads the latest stable JStall release. To test with the latest
development build from GitHub Actions instead, use:

```bash
JSTALL_DEV=1 make build

# Or download a specific GitHub Actions run by ID
JSTALL_DEV=<run-id> make build
```

This pulls the latest JStall build directly from the GitHub Actions artifacts instead of the released version.

### Testing

**Python Tests**: Modern pytest-based test suite.

```bash
cd test && ./setup.sh && ./test.py all
```

### Test Suite Resumption

The Python test runner in `test/` supports resuming tests from any point using the `--start-with` option:

```bash
./test.py --start-with TestClass::test_method all  # Start with a specific test (inclusive)
```

This is useful for long test suites or after interruptions. See `test/README.md` for more details.

### Code Quality

Centralized linting scripts:

```bash
./scripts/lint-all.sh check    # Quality check
./scripts/lint-all.sh fix      # Auto-fix formatting
./scripts/lint-all.sh ci       # CI validation
```

### CI/CD

- Multi-platform builds (Linux, macOS, Windows)
- Automated linting and testing on PRs
- Pre-commit hooks with auto-formatting

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via
[GitHub issues](https://github.com/SAP/cf-cli-java-plugin/issues). Contribution and feedback are encouraged and always
welcome. Just be aware that this plugin is limited in scope to keep it maintainable. For more information about how to
contribute, the project structure, as well as additional contribution information, see our
[Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at
[in our security policy](https://github.com/SAP/cf-cli-java-plugin/security/policy) on how to report it. Please do not
create GitHub issues for security-related doubts or problems.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for a detailed list of changes.

## License

Copyright 2017 - 2026 SAP SE or an SAP affiliate company and contributors. Please see our LICENSE for copyright and
license information.
