[![REUSE status](https://api.reuse.software/badge/github.com/SAP/cf-cli-java-plugin)](https://api.reuse.software/info/github.com/SAP/cf-cli-java-plugin)

# Cloud Foundry Command Line Java plugin

This plugin for the [Cloud Foundry Command Line](https://github.com/cloudfoundry/cli) provides convenience utilities to work with Java applications deployed on Cloud Foundry.

Currently, it allows to:
* Trigger and retrieve a heap dump and a thread dump from an instance of a Cloud Foundry Java application
* To run jcmd remotely on your application
* To start, stop and retrieve JFR and async-profiler profiles from your application

## Installation

### Installation via CF Community Repository

Make sure you have the CF Community plugin repository configured or add it via (```cf add-plugin-repo CF-Community http://plugins.cloudfoundry.org```)

Trigger installation of the plugin via
```
cf install-plugin -r CF-Community "java"
```

### Manual Installation
Download the binary file for your target OS from the [latest release](https://github.com/SAP/cf-cli-java-plugin/releases/latest).

If you've already installed the plugin and are updating it, you must first execute the `cf uninstall-plugin java` command.

Install the plugin with `cf install-plugin [cf-cli-java-plugin]` (replace `[cf-cli-java-plugin]` with the actual binary name you will use, which depends on the OS you are running).

You can verify that the plugin is successfully installed by looking for `java` in the output of `cf plugins`.

### Updating from version 1.x to 2.x

With release 2.0 we aligned the convention of the plugin having the same name as the command it contributes (in our case, `java`).
This change mostly affects you in the way you update your plugin.
If you have the version 1.x installed, you will need to uninstall the old version first by using the command: `cf uninstall-plugin JavaPlugin`.
You know you have the version 1.x installed if `JavaPlugin` appears in the output of `cf plugins`.

### Permission Issues

On Linux and macOS, if you get a permission error, run `chmod +x [cf-cli-java-plugin]` (replace `[cf-cli-java-plugin]` with the actual binary name you will use, which depends on the OS you are running) on the plugin binary.
On Windows, the plugin will refuse to install unless the binary has the `.exe` file extension.

## Usage

### Prerequisites

#### JDK Tools
This plugin internally uses `jmap` for OpenJDK-like Java virtual machines. When using the [Cloud Foundry Java Buildpack](https://github.com/cloudfoundry/java-buildpack), `jmap` is no longer shipped by default in order to meet the legal obligations of the Cloud Foundry Foundation.
To ensure that `jmap` is available in the container of your application, you have to explicitly request a full JDK in your application manifest via the `JBP_CONFIG_OPEN_JDK_JRE` environment variable. This could be done like this:

```yaml
---
applications:
- name: <APP_NAME>
  memory: 1G
  path: <PATH_TO_BUILD_ARTIFACT>
  buildpack: https://github.com/cloudfoundry/java-buildpack
  env:
    JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/bionic/x86_64", version: 11.+ } }'
```
Please note that this requires the use of an online buildpack (configured in the `buildpack` property). When system buildpacks are used, staging will fail with cache issues, because the system buildpacks don’t have the JDK chached.
Please also note that this is not to be considered a recommendation to use a full JDK. It's just one option to get the tools required for the use of this plugin when you need it, e.g., for troubleshooting.
The `version` property is optional and can be used to request a specific Java version.

#### SSH Access
As it is built directly on `cf ssh`, the `cf java` plugin can work only with Cloud Foundry applications that have `cf ssh` enabled.
To check if your app fulfills the requirements, you can find out by running the `cf ssh-enabled [app-name]` command.
If not enabled yet, run `cf enable-ssh [app-name]`.

**Note:** You must restart your app after enabling SSH access.

In case a proxy server is used, ensure that `cf ssh` is configured accordingly.
Refer to the [official documentation](https://docs.cloudfoundry.org/cf-cli/http-proxy.html#v3-ssh-socks5) of the Cloud Foundry Command Line for more information.
If `cf java` is having issues connecting to your app, chances are the problem is in the networking issues encountered by `cf ssh`.
To verify, run your `cf java` command in "dry-run" mode by adding the `-n` flag and try to execute the command line that `cf java` gives you back.
If it fails, the issue is not in `cf java`, but in whatever makes `cf ssh` fail.

### Commands
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
        Print information about the Java Virtual Machine running a Java application

     jcmd
        Run a JCMD command on a running Java application via --args

     start-jfr
        Start a Java Flight Recorder recording on a running Java application (additional options via --args)

     stop-jfr
        Stop a Java Flight Recorder recording on a running Java application (additional options via --args)

     vm-version
        Print the version of the Java Virtual Machine running a Java application

     vm-vitals
        Print vital statistics about the Java Virtual Machine running a Java application

     asprof (recent SapMachine only)
        Run async-profiler commands passed to asprof via --args

     start-asprof (recent SapMachine only)
        Start async-profiler profiling on a running Java application (additional options via --args)

     start-asprof-cpu-profile (recent SapMachine only)
        Start async-profiler CPU profiling on a running Java application (additional options via --args)

     start-asprof-wall-clock-profile (recent SapMachine only)
        Start async-profiler wall-clock profiling on a running Java application (additional options via --args)

     stop-asprof
        Stop async-profiler profiling on a running Java application (additional options via --args)

OPTIONS:
   -args                     -a, Miscellaneous arguments to pass to the command in the container, be aware to end it with a space if it is a simple option
   -container-dir            -cd, the directory path in the container that the heap dump/JFR/... file will be saved to
   -dry-run                  -n, just output to command line what would be executed
   -keep                     -k, keep the heap dump in the container; by default the heap dump/JFR/... will be deleted from the container's filesystem after been downloaded
   -local-dir                -ld, the local directory path that the dump/JFR/... file will be saved to
   -app-instance-index       -i [index], select to which instance of the app to connect
</pre>

The heap dumps and profiles will be copied to a local file if `-local-dir` is specified as a full folder path. Without providing `-local-dir` the heap dump will only be created in the container and not transferred.
To save disk space of the application container, the files are automatically deleted unless the `-keep` option is set.

Providing `-container-dir` is optional. If specified the plugin will create the heap dump or profile at the given file path in the application container. Without providing this parameter, the file will be created either at `/tmp` or at the file path of a file system service if attached to the container.

```shell
cf java [heap-dump|stop-jfr|stop-asprof] [my-app] -local-dir /local/path [-container-dir /var/fspath]
```

Everything else, like thread dumps, will be outputted to `std-out`.
You may want to redirect the command's output to file, e.g., by executing:

```shell
cf java thread-dump [my_app] -i [my_instance_index] > heap-dump.hprof
```

The `-k` flag is invalid when invoking non file producing commands.
(Unlike with heap dumps, the JVM does not need to output the thread dump to file before streaming it out.)

## Limitations

The capability of creating heap dumps and profiles is also limited by the filesystem available to the container.
The `cf java heap-dump` command triggers the heap dump to file system, read the content of the file over the SSH connection, and then remove the heap dump file from the container's file system (unless you have the `-k` flag set).
The amount of filesystem space available to a container is set for the entire Cloud Foundry landscape with a global configuration.
The size of a heap dump is roughly linear with the allocated memory of the heap.
So, it could be that, in case of large heaps or the filesystem having too much stuff in it, there is not enough space on the filesystem for creating the heap dump.
In that case, the creation of the heap dump and thus the command will fail.
The same is true for stopping and thereby retrieving
profiles.

From the perspective of integration in workflows and overall shell-friendliness, the `cf java` plugin suffers from some shortcomings in the current `cf-cli` plugin framework:
* There is no distinction between `stdout` and `stderr` output from the underlying `cf ssh` command (see [this issue on the `cf-cli` project](https://github.com/cloudfoundry/cli/issues/1074))
  * The `cf java` will however exit with status code `1` when the underpinning `cf ssh` command fails
  * If split between `stdout` and `stderr` is needed, you can run the `cf java` plugin in dry-run mode (`--dry-run` flag) and execute its output instead
* The plugin is not current capability of storing output directly to file (see [this issue on the `cf-cli` project](https://github.com/cloudfoundry/cli/issues/1069))
  * The upstream change needed to fix this issue has been scheduled at Pivotal; when they provide the new API we need, we'll update the `cf java` command to save output to file.

## Side-effects on the running instance

Executing a thread dump via the `cf java` command does not have much of an overhead on the affected JVM.
(Unless you have **a lot** of threads, that is.)

Heap dumps, on the other hand, have to be treated with a little more care.
First of all, triggering the heap dump of a JVM makes the latter execute in most cases a full garbage collection, which will cause your JVM to become unresponsive for the duration.
How much time is needed to execute the heap dump, depends on the size of the heap (the bigger, the slower), the algorithm used and, above all, whether your container is swapping memory to disk or not (swap is *bad* for the JVM).
Since Cloud Foundry allows for over-commit in its cells, it is possible that a container would begin swapping when executing a full garbage collection.
(To be fair, it could be swapping even *before* the garbage collection begins, but let's not knit-pick here.)
So, it is theoretically possible that execuing a heap dump on a JVM in poor status of health will make it go even worse.

Profiles might cause overhead depending on the configuration, but the default configurations
typically have a limited overhead.

Secondly, as the JVMs output heap dumps to the filesystem, creating a heap dump may lead to to not enough space on the filesystem been available for other tasks (e.g., temp files).
In that case, the application in the container may suffer unexpected errors.

## Tests and Mocking

The tests are written using [Ginkgo](https://onsi.github.io/ginkgo/) with [Gomega](https://onsi.github.io/gomega/) for the BDD structure, and [Counterfeiter](https://github.com/maxbrunsfeld/counterfeiter) for the mocking generation.
Unless modifications to the helper interfaces `cmd.CommandExecutor` and `uuid.UUIDGenerator` are needed, there should be no need to regenerate the mocks.

To run the tests, go to the root of the repository and simply run `gingko` (you may need to install Ginkgo first, e.g., `go get github.com/onsi/ginkgo/ginkgo` puts the executable under `$GOPATH/bin`).
