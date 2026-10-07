# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Bundle [jstall](https://github.com/parttimenerd/jstall) v0.8.1 (jstall-minimal.jar) for one-shot JVM
  inspection via `cf java jstall APP_NAME`. Requires Java 17+ locally. Key subcommands: `status` (thread
  analysis, metaspace, GC, compiler queue), `record-status` (repeated sampling), `flame-graph`, `heap-info`.
  Supports all jstall subcommands via `--args`.
- `heap-dump --redact`: zeros primitive arrays (`byte[]`, `char[]`, etc.) in the downloaded dump before saving
  (lean redaction mode), using the bundled [hprof-redact](https://github.com/parttimenerd/hprof-analyzer) binary.
  Supported on Linux (amd64, arm64), macOS (Apple Silicon), and Windows (amd64, arm64).
- `heap-dump --redact-complete`: zeros all primitive arrays and individual primitive fields (complete redaction mode,
  maximum privacy). Mutually exclusive with `--redact`.
- `heap-dump --compress`: saves the local file as `.hprof.gz` instead of decompressing it after transfer
  (requires JDK 17+ on the container). Useful when you want to store or share the compressed dump directly.
- Transparent compressed transfer: on JDK 17+ containers, the plugin always uses `jmap gz=1` to compress
  the dump during SSH transfer (faster on slow connections), then decompresses on the fly so the local file
  is a plain `.hprof`. Use `--compress` to keep the file compressed locally.
- `heap-dump --open`: after downloading (and optionally redacting/compressing) the dump, spins up a temporary local
  HTTP server and opens the [hprof-analyzer](https://parttimenerd.github.io/hprof-analyzer) web app in the default
  browser with the dump pre-loaded. The server serves the file exactly once via a random token URL and shuts down
  automatically after the browser fetches it.
- `heap-dump --open-url <URL>`: override the hprof-analyzer base URL (e.g. a locally running instance). Implies
  `--open`.
- JRE-only container support: `heap-dump`, `thread-dump`, `vm-info`, `vm-version`, and `jcmd` now work on
  containers without JDK tools (`jmap`, `jstack`, `jcmd`) by falling back to the HotSpot attach socket via
  `nc -U`. Requires `netcat-openbsd` or `nmap-ncat` on the container. Supports JDK 9–25 on Linux and macOS.
  The `jstall`-based commands (`status`, `record-status`, etc.) use the same attach-socket path and also work
  on JRE-only containers.

### Changed

- macOS plugin support now requires Apple Silicon. macOS Intel (`darwin/amd64`) is not supported.
- SSH errors now include actionable diagnostics: connection reset/refused errors suggest retrying,
  "instance does not exist" errors explain why (app stopped/scaled down), and permission errors
  indicate SSH is disabled for the space.

### Fixed

- Windows: `cf install-plugin <URL>` now works — the Windows binary is released with the required `.exe` extension
  (`cf-cli-java-plugin-windows-amd64.exe`). Previously, CF CLI rejected the downloaded temp file with
  *"temp/...exe doesn't exist"*.
- Windows: `jstall`-based commands (e.g. `cf java status`) now work correctly — the JVM system property
  `jdk.lang.Process.allowAmbiguousCommands=false` is set so embedded quotes in the remote shell payload are
  properly escaped when CF CLI is invoked via Java's `ProcessBuilder`.
- Better error when app name and subcommand are accidentally swapped (e.g. `cf java my-app heap-dump` instead
  of `cf java heap-dump my-app`): if the first argument matches a known app in the current space, the error now
  suggests the corrected command.
- Bumped Go toolchain to 1.26.8 to resolve six standard-library vulnerabilities
  (GO-2026-5856, GO-2026-5972, GO-2026-5039, GO-2026-5037, GO-2026-6089, GO-2026-6090).

## [4.0.2]

### Fixed

- Fix rare ssh connection issue

## [4.0.1]

### Fixed

- Fix thread-dump command

## [4.0.0]

### Added

- Create a proper test suite
- Profiling and JCMD related features

### Fixed

- Fix many bugs discovered during testing
