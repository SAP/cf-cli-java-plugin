[![REUSE status](https://api.reuse.software/badge/github.com/SAP/cf-cli-java-plugin)](https://api.reuse.software/info/github.com/SAP/cf-cli-java-plugin)
[![Build and Snapshot Release](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/build-and-snapshot.yml/badge.svg)](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/build-and-snapshot.yml)
[![PR Validation](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/pr-validation.yml/badge.svg)](https://github.com/SAP/cf-cli-java-plugin/actions/workflows/pr-validation.yml)

# Cloud Foundry Command Line Java plugin

This plugin for the [Cloud Foundry Command Line](https://github.com/cloudfoundry/cli) provides convenience utilities to
work with Java applications deployed on Cloud Foundry.

Currently, it allows you to:

- Trigger and retrieve a heap dump and a thread dump from an instance of a Cloud Foundry Java application
- To run jcmd remotely on your application
- To start, stop and retrieve JFR and [async-profiler](https://github.com/jvm-profiling-tools/async-profiler)
  ([SapMachine](https://sapmachine.io) only) profiles from your application

## Installation

### Manual Installation

Download the latest release from [GitHub](https://github.com/SAP/cf-cli-java-plugin/releases/latest).

To install a new version of the plugin, run the following:

```sh
# on Mac arm64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-macos-arm64
# on Windows x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-windows-amd64
# on Linux x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/latest/download/cf-cli-java-plugin-linux-amd64
```

You can verify that the plugin is successfully installed by looking for `java` in the output of `cf plugins`.

### Installation via CF Community Repository

Make sure you have the CF Community plugin repository configured (or add it via
`cf add-plugin-repo CF-Community http://plugins.cloudfoundry.org`)

Trigger installation of the plugin via

```sh
cf install-plugin java
```

The releases in the community repository are older than the actual releases on GitHub, that you can install manually, so
we recommend the manual installation.

### Manual Installation of Snapshot Release

Download the current snapshot release from [GitHub](https://github.com/SAP/cf-cli-java-plugin/releases/tag/snapshot).
This is intended for experimentation and might fail.

To install a new version of the plugin, run the following:

```sh
# on Mac arm64
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-macos-arm64
# on Windows x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-windows-amd64
# on Linux x86
cf install-plugin https://github.com/SAP/cf-cli-java-plugin/releases/download/snapshot/cf-cli-java-plugin-linux-amd64
```

### Updating from version 1.x to 2.x

With release 2.0 we aligned the convention of the plugin having the same name as the command it contributes (in our
case, `java`). This change mostly affects you in the way you update your plugin. If you have the version 1.x installed,
you will need to uninstall the old version first by using the command: `cf uninstall-plugin JavaPlugin`. You know you
have the version 1.x installed if `JavaPlugin` appears in the output of `cf plugins`.

### Permission Issues

On Linux and macOS, if you get a permission error, run `chmod +x [cf-cli-java-plugin]` (replace `[cf-cli-java-plugin]`
with the actual binary name you will use, which depends on the OS you are running) on the plugin binary. On Windows, the
plugin will refuse to install unless the binary has the `.exe` file extension.

## Usage

### Prerequisites

#### JDK Tools

This plugin internally uses `jmap` for OpenJDK-like Java virtual machines. When using the
[Cloud Foundry Java Buildpack](https://github.com/cloudfoundry/java-buildpack), `jmap` is no longer shipped by default
in order to meet the legal obligations of the Cloud Foundry Foundation. To ensure that `jmap` is available in the
container of your application, you have to explicitly request a full JDK in your application manifest via the
`JBP_CONFIG_OPEN_JDK_JRE` environment variable. This could be done like this:

```yaml
---
applications:
  - name: <APP_NAME>
    memory: 1G
    path: <PATH_TO_BUILD_ARTIFACT>
    buildpack: https://github.com/cloudfoundry/java-buildpack
    env:
      JBP_CONFIG_OPEN_JDK_JRE:
        '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64", version: 11.+ }
        }'
      JBP_CONFIG_JAVA_OPTS: "[java_opts: '-XX:+UnlockDiagnosticVMOptions -XX:+DebugNonSafepoints']"
```

`-XX:+UnlockDiagnosticVMOptions -XX:+DebugNonSafepoints` is used to improve profiling accuracy and has no known negative
performance impacts.

Please note that this requires the use of an online buildpack (configured in the `buildpack` property). When system
buildpacks are used, staging will fail with cache issues, because the system buildpacks don’t have the JDK cached.
Please also note that this is not to be considered a recommendation to use a full JDK. It's just one option to get the
tools required for the use of this plugin when you need it, e.g., for troubleshooting. The `version` property is
optional and can be used to request a specific Java version.

#### SSH Access

As it is built directly on `cf ssh`, the `cf java` plugin can work only with Cloud Foundry applications that have
`cf ssh` enabled. To check if your app fulfills the requirements, you can find out by running the
`cf ssh-enabled [app-name]` command. If not enabled yet, run `cf enable-ssh [app-name]`.

**Note:** You must restart your app after enabling SSH access.

In case a proxy server is used, ensure that `cf ssh` is configured accordingly. Refer to the
[official documentation](https://docs.cloudfoundry.org/cf-cli/http-proxy.html#v3-ssh-socks5) of the Cloud Foundry
Command Line for more information. If `cf java` is having issues connecting to your app, chances are the problem is in
the networking issues encountered by `cf ssh`. To verify, run your `cf java` command in "dry-run" mode by adding the
`-n` flag and try to execute the command line that `cf java` gives you back. If it fails, the issue is not in `cf java`,
but in whatever makes `cf ssh` fail.

### Examples

Getting a heap-dump:

```sh
> cf java heap-dump $APP_NAME
-> ./$APP_NAME-heapdump-$RANDOM.hprof
```

Getting a thread-dump:

```sh
> cf java thread-dump $APP_NAME
...
Full thread dump OpenJDK 64-Bit Server VM ...
...
```

Creating a CPU-time profile via async-profiler:

```sh
> cf java asprof-start-cpu $APP_NAME
Profiling started
# wait some time to gather data
> cf java asprof-stop $APP_NAME
-> ./$APP_NAME-asprof-$RANDOM.jfr
```

Running arbitrary JCMD commands, like `VM.uptime`:

```sh
> cf java jcmd $APP_NAME -a VM.uptime
Connected to remote JVM
JVM response code = 0
$TIME s
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

The following is a list of all available commands (some of the SapMachine specific), generated via `cf java --help`:

<pre>

NAME:
   java - Obtain a heap-dump, thread-dump or profile from a running, SSH-enabled Java application.

USAGE:
   cf java COMMAND APP_NAME [options]

     heap-dump
        Generate a heap dump from a running Java application

     thread-dump
        Generate a thread dump from a running Java application

     vm-info
        Print information about the Java Virtual Machine running a Java
        application

     jcmd (supports --args)
        Run a JCMD command on a running Java application via --args, downloads
        and deletes all files that are created in the current folder, use
        '--no-download' to prevent this. Environment variables available:
        @FSPATH (writable directory path, always set), @ARGS (command
        arguments), @APP_NAME (application name), @FILE_NAME (generated filename
        for file operations without UUID), and @STATIC_FILE_NAME (without UUID).
        Use single quotes around --args to prevent shell expansion.

     jfr-start
        Start a Java Flight Recorder default recording on a running Java
        application (stores in the container-dir)

     jfr-start-profile
        Start a Java Flight Recorder profile recording on a running Java
        application (stores in the container-dir))

     jfr-start-gc (recent SapMachine only)
        Start a Java Flight Recorder GC recording on a running Java application
        (stores in the container-dir)

     jfr-start-gc-details (recent SapMachine only)
        Start a Java Flight Recorder detailed GC recording on a running Java
        application (stores in the container-dir)

     jfr-stop
        Stop a Java Flight Recorder recording on a running Java application

     jfr-dump
        Dump a Java Flight Recorder recording on a running Java application
        without stopping it

     jfr-status
        Check the running Java Flight Recorder recording on a running Java
        application

     vm-version
        Print the version of the Java Virtual Machine running a Java application

     vm-vitals
        Print vital statistics about the Java Virtual Machine running a Java
        application

     asprof (recent SapMachine only, supports --args)
        Run async-profiler commands passed to asprof via --args, copies files in
        the current folder. Don't use in combination with asprof-* commands.
        Downloads and deletes all files that are created in the current folder,
        if not using 'start' asprof command, use '--no-download' to prevent
        this. Environment variables available: @FSPATH (writable directory path,
        always set), @ARGS (command arguments), @APP_NAME (application name),
        @FILE_NAME (generated filename for file operations), and
        @STATIC_FILE_NAME (without UUID). Use single quotes around --args to
        prevent shell expansion.

     asprof-start-cpu (recent SapMachine only)
        Start an async-profiler CPU-time profile recording on a running Java
        application

     asprof-start-wall (recent SapMachine only)
        Start an async-profiler wall-clock profile recording on a running Java
        application

     asprof-start-alloc (recent SapMachine only)
        Start an async-profiler allocation profile recording on a running Java
        application

     asprof-start-lock (recent SapMachine only)
        Start an async-profiler lock profile recording on a running Java
        application

     asprof-stop (recent SapMachine only)
        Stop an async-profiler profile recording on a running Java application

     asprof-status (recent SapMachine only)
        Get the status of async-profiler on a running Java application

OPTIONS:
   -dry-run                  -n, just output to command line what would be executed
   -keep                     -k, keep the heap dump in the container; by default the heap dump/JFR/... will
                               be deleted from the container's filesystem after being downloaded
   -local-dir                -ld, the local directory path that the dump/JFR/... file will be saved to,
                                defaults to the current directory
   -no-download              -nd, don't download the heap dump/JFR/... file to local, only keep it in the
                                container, implies '--keep'
   -verbose                  -v, enable verbose output for the plugin
   -app-instance-index       -i [index], select to which instance of the app to connect
   -args                     -a, Miscellaneous arguments to pass to the command (if supported) in the
                               container, be aware to end it with a space if it is a simple option. For
                               commands that create arbitrary files (jcmd, asprof), the environment
                               variables @FSPATH, @ARGS, @APP_NAME, @FILE_NAME, and @STATIC_FILE_NAME are
                               available in --args to reference the working directory path, arguments,
                               application name, and generated file name respectively.
   -container-dir            -cd, the directory path in the container that the heap dump/JFR/... file will be
                                saved to

</pre>

The heap dumps and profiles will be copied to a local file if `-local-dir` is specified as a full folder path. Without
providing `-local-dir` the heap dump will only be created in the container and not transferred. To save disk space of
the application container, the files are automatically deleted unless the `-keep` option is set.

Providing `-container-dir` is optional. If specified the plugin will create the heap dump or profile at the given file
path in the application container. Without providing this parameter, the file will be created either at `/tmp` or at the
file path of a file system service if attached to the container.

```shell
cf java [heap-dump|stop-jfr|stop-asprof] [my-app] -local-dir /local/path [-container-dir /var/fspath]
```

Everything else, like thread dumps, will be output to `std-out`. You may want to redirect the command's output to file,
e.g., by executing:

```shell
cf java thread-dump [my_app] -i [my_instance_index] > heap-dump.hprof
```

The `-k` flag is invalid when invoking non file producing commands. (Unlike with heap dumps, the JVM does not need to
output the thread dump to file before streaming it out.)

## Limitations

The capability of creating heap dumps and profiles is also limited by the filesystem available to the container. The
`cf java heap-dump`, `cf java asprof-stop` and `cf java jfr-stop` commands trigger a write to the file system, read the
content of the file over the SSH connection, and then remove the file from the container's file system (unless you have
the `-k` flag set). The amount of filesystem space available to a container is set for the entire Cloud Foundry
landscape with a global configuration. The size of a heap dump is roughly linear with the allocated memory of the heap
and the size of the profile is related to the length of the recording. So, it could be that, in case of large heaps,
long profiling durations or the filesystem having too much stuff in it, there is not enough space on the filesystem for
creating the file. In that case, the creation of the heap dump or profile recording and thus the command will fail.

From the perspective of integration in workflows and overall shell-friendliness, the `cf java` plugin suffers from some
shortcomings in the current `cf-cli` plugin framework:

- There is no distinction between `stdout` and `stderr` output from the underlying `cf ssh` command (see
  [this issue on the `cf-cli` project](https://github.com/cloudfoundry/cli/issues/1074))
  - The `cf java` will however (mostly) exit with status code `1` when the underpinning `cf ssh` command fails
  - If split between `stdout` and `stderr` is needed, you can run the `cf java` plugin in dry-run mode (`--dry-run`
    flag) and execute its output instead

## Side-effects on the running instance

Storing dumps or profile recordings to the filesystem may lead to to not enough space on the filesystem been available
for other tasks (e.g., temp files). In that case, the application in the container may suffer unexpected errors.

### Thread-Dumps

Executing a thread dump via the `cf java` command does not have much of an overhead on the affected JVM. (Unless you
have **a lot** of threads, that is.)

### Heap-Dumps

Heap dumps, on the other hand, have to be treated with a little more care. First of all, triggering the heap dump of a
JVM makes the latter execute in most cases a full garbage collection, which will cause your JVM to become unresponsive
for the duration. How much time is needed to execute the heap dump, depends on the size of the heap (the bigger, the
slower), the algorithm used and, above all, whether your container is swapping memory to disk or not (swap is _bad_ for
the JVM). Since Cloud Foundry allows for over-commit in its cells, it is possible that a container would begin swapping
when executing a full garbage collection. (To be fair, it could be swapping even _before_ the garbage collection begins,
but let's not knit-pick here.) So, it is theoretically possible that execuing a heap dump on a JVM in poor status of
health will make it go even worse.

### Profiles

Profiles might cause overhead depending on the configuration, but the default configurations typically have a limited
overhead.

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

### Snapshot

### 4.0.0-snapshot

- Create a proper test suite
- Fix many bugs discovered during testing
- Profiling and JCMD related features

## License

Copyright 2017 - 2025 SAP SE or an SAP affiliate company and contributors. Please see our LICENSE for copyright and
license information.
