/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package main

import (
	"github.com/SAP/cf-cli-java-plugin/cmd"
	"github.com/SAP/cf-cli-java-plugin/uuid"

	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"code.cloudfoundry.org/cli/cf/terminal"
	"code.cloudfoundry.org/cli/cf/trace"
	"code.cloudfoundry.org/cli/plugin"

	"utils"

	guuid "github.com/satori/go.uuid"
	"github.com/simonleung8/flags"
)

// The JavaPlugin is a cf cli plugin that supports taking heap and thread dumps on demand
type JavaPlugin struct {
}

// InvalidUsageError errors mean that the arguments passed in input to the command are invalid
type InvalidUsageError struct {
	message string
}

func (e InvalidUsageError) Error() string {
	return e.message
}

type commandExecutorImpl struct {
	cliConnection plugin.CliConnection
}

func (c commandExecutorImpl) Execute(args []string) ([]string, error) {
	output, err := c.cliConnection.CliCommand(args...)

	return output, err
}

type uuidGeneratorImpl struct {
}

func (u uuidGeneratorImpl) Generate() string {
	return guuid.NewV4().String()
}

const (
	// JavaDetectionCommand is the prologue command to detect on the Garden container if it contains a Java app. Visible for tests
	JavaDetectionCommand = "if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi"
	heapDumpCommand      = "heap-dump"
	threadDumpCommand    = "thread-dump"
)

// Run must be implemented by any plugin because it is part of the
// plugin interface defined by the core CLI.
//
// Run(....) is the entry point when the core CLI is invoking a command defined
// by a plugin. The first parameter, plugin.CliConnection, is a struct that can
// be used to invoke cli commands. The second paramter, args, is a slice of
// strings. args[0] will be the name of the command, and will be followed by
// any additional arguments a cli user typed in.
//
// Any error handling should be handled with the plugin itself (this means printing
// user facing errors). The CLI will exit 0 if the plugin exits 0 and will exit
// 1 should the plugin exit nonzero.
func (c *JavaPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	_, err := c.DoRun(&commandExecutorImpl{cliConnection: cliConnection}, &uuidGeneratorImpl{}, utils.CfJavaPluginUtilImpl{}, args)
	if err != nil {
		os.Exit(1)
	}
}

// DoRun is an internal method that we use to wrap the cmd package with CommandExecutor for test purposes
func (c *JavaPlugin) DoRun(commandExecutor cmd.CommandExecutor, uuidGenerator uuid.UUIDGenerator, util utils.CfJavaPluginUtil, args []string) (string, error) {
	traceLogger := trace.NewLogger(os.Stdout, true, os.Getenv("CF_TRACE"), "")
	ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), traceLogger)

	output, err := c.execute(commandExecutor, uuidGenerator, util, args)
	if err != nil {
		ui.Failed(err.Error())

		if _, invalidUsageErr := err.(*InvalidUsageError); invalidUsageErr {
			fmt.Println()
			fmt.Println()
			commandExecutor.Execute([]string{"help", "java"})
		}
	} else if output != "" {
		ui.Say(output)
	}

	return output, err
}

func (c *JavaPlugin) execute(commandExecutor cmd.CommandExecutor, uuidGenerator uuid.UUIDGenerator, util utils.CfJavaPluginUtil, args []string) (string, error) {
	if len(args) == 0 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	switch args[0] {
	case "CLI-MESSAGE-UNINSTALL":
		// Nothing to uninstall, we keep no local state
		return "", nil
	case "java":
		break
	default:
		return "", &InvalidUsageError{message: fmt.Sprintf("Unexpected command name '%s' (expected : 'java')", args[0])}
	}

	if os.Getenv("CF_TRACE") == "true" {
		return "", errors.New("The environment variable CF_TRACE is set to true. This prevents download of the dump from succeeding")
	}

	commandFlags := flags.New()

	commandFlags.NewIntFlagWithDefault("app-instance-index", "i", "application `instance` to connect to", -1)
	commandFlags.NewBoolFlag("keep", "k", "whether to `keep` the heap/thread-dump on the container of the application instance after having downloaded it locally")
	commandFlags.NewBoolFlag("dry-run", "n", "triggers the `dry-run` mode to show only the cf-ssh command that would have been executed")
	commandFlags.NewStringFlag("container-dir", "cd", "specify the folder path where the dump file should be stored in the container")
	commandFlags.NewStringFlag("local-dir", "ld", "specify the folder where the dump file will be downloaded to, dump file wil not be copied to local if this parameter  was not set")

	parseErr := commandFlags.Parse(args[1:]...)
	if parseErr != nil {
		return "", &InvalidUsageError{message: fmt.Sprintf("Error while parsing command arguments: %v", parseErr)}
	}

	applicationInstance := commandFlags.Int("app-instance-index")
	keepAfterDownload := commandFlags.IsSet("keep")

	remoteDir := commandFlags.String("container-dir")
	localDir := commandFlags.String("local-dir")

	copyToLocal := len(localDir) > 0

	arguments := commandFlags.Args()
	argumentLen := len(arguments)

	if argumentLen < 1 {
		return "", &InvalidUsageError{message: fmt.Sprintf("No command provided")}
	}

	command := arguments[0]
	switch command {
	case heapDumpCommand:
		break
	case threadDumpCommand:
		if commandFlags.IsSet("keep") {
			return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for thread-dumps", "keep")}
		}
		if commandFlags.IsSet("container-dir") {
			return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for thread-dumps", "container-dir")}
		}
		if commandFlags.IsSet("local-dir") {
			return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for thread-dumps", "local-dir")}
		}
	default:
		return "", &InvalidUsageError{message: fmt.Sprintf("Unrecognized command %q: supported commands are 'heap-dump' and 'thread-dump' (see cf help)", command)}
	}

	if argumentLen == 1 {
		return "", &InvalidUsageError{message: fmt.Sprintf("No application name provided")}
	} else if argumentLen > 2 {
		return "", &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	applicationName := arguments[1]

	cfSSHArguments := []string{"ssh", applicationName}
	if applicationInstance > 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(applicationInstance))
	}

	var remoteCommandTokens = []string{JavaDetectionCommand}
	heapdumpFileName := ""
	fspath := remoteDir
	switch command {
	case heapDumpCommand:

		supported, err := util.CheckRequiredTools(applicationName)
		if err != nil || !supported {
			return "required tools checking failed", err
		}

		fspath, err = util.GetAvailablePath(applicationName, remoteDir)
		if err != nil {
			return "", err
		}
		heapdumpFileName = fspath + "/" + applicationName + "-heapdump-" + uuidGenerator.Generate() + ".hprof"

		remoteCommandTokens = append(remoteCommandTokens,
			// Check file does not already exist
			"if [ -f "+heapdumpFileName+" ]; then echo >&2 'Heap dump "+heapdumpFileName+" already exists'; exit 1; fi",
			/*
			 * If there is not enough space on the filesystem to write the dump, jmap will create a file
			 * with size 0, output something about not enough space left on device and exit with status code 0.
			 * Because YOLO.
			 *
			 * Also: if the heap dump file already exists, jmap will output something about the file already
			 * existing and exit with status code 0. At least it is consistent.
			 */
			// OpenJDK: Wrap everything in an if statement in case jmap is available
			"JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`",
			// SAP JVM: Wrap everything in an if statement in case jvmmon is available
			"JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`",
			"if [ -n \"${JMAP_COMMAND}\" ]; then true",
			"OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file="+heapdumpFileName+" $(pidof java) ) || STATUS_CODE=$?",
			"if [ ! -s "+heapdumpFileName+" ]; then echo >&2 ${OUTPUT}; exit 1; fi",
			"if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi",
			"elif [ -n \"${JVMMON_COMMAND}\" ]; then true",
			"echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath="+fspath+"\ndump heap' > setHeapDumpOnDemandPath.sh",
			"OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?",
			"sleep 5", // Writing the heap dump is triggered asynchronously -> give the jvm some time to create the file
			"HEAP_DUMP_NAME=`find "+fspath+" -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`",
			"SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done",
			"if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi",
			"if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi",
			"fi")

	case threadDumpCommand:
		// OpenJDK
		remoteCommandTokens = append(remoteCommandTokens, "JSTACK_COMMAND=`find -executable -name jstack | head -1`; if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi")
		// SAP JVM
		remoteCommandTokens = append(remoteCommandTokens, "JVMMON_COMMAND=`find -executable -name jvmmon | head -1`; if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi")
	}

	cfSSHArguments = append(cfSSHArguments, "--command")
	remoteCommand := strings.Join(remoteCommandTokens, "; ")

	if commandFlags.IsSet("dry-run") {
		// When printing out the entire command line for separate execution, we wrap the remote command in single quotes
		// to prevent the shell processing it from running it in local
		cfSSHArguments = append(cfSSHArguments, "'"+remoteCommand+"'")
		return "cf " + strings.Join(cfSSHArguments, " "), nil
	}

	cfSSHArguments = append(cfSSHArguments, remoteCommand)

	output, err := commandExecutor.Execute(cfSSHArguments)
	if command == heapDumpCommand {

		finalFile, err := util.FindDumpFile(applicationName, heapdumpFileName, fspath)
		if err == nil && finalFile != "" {
			heapdumpFileName = finalFile
			fmt.Println("Successfully created heap dump in application container at: " + heapdumpFileName)
		} else {
			fmt.Println("Failed to find heap dump in application container")
			fmt.Println(finalFile)
			fmt.Println(heapdumpFileName)
			fmt.Println(fspath)
			return "", err
		}

		if copyToLocal {
			localFileFullPath := localDir + "/" + applicationName + "-heapdump-" + uuidGenerator.Generate() + ".hprof"
			err = util.CopyOverCat(applicationName, heapdumpFileName, localFileFullPath)
			if err == nil {
				fmt.Println("Heap dump file saved to: " + localFileFullPath)
			} else {
				return "", err
			}
		} else {
			fmt.Println("Heap dump will not be copied as parameter `local-dir` was not set")
		}

		if !keepAfterDownload {
			err = util.DeleteRemoteFile(applicationName, heapdumpFileName)
			if err != nil {
				return "", err
			}
			fmt.Println("Heap dump file deleted in app container")
		}
	}
	// We keep this around to make the compiler happy, but commandExecutor.Execute will cause an os.Exit
	return strings.Join(output, "\n"), err
}

// GetMetadata must be implemented as part of the plugin interface
// defined by the core CLI.
//
// GetMetadata() returns a PluginMetadata struct. The first field, Name,
// determines the name of the plugin which should generally be without spaces.
// If there are spaces in the name a user will need to properly quote the name
// during uninstall otherwise the name will be treated as seperate arguments.
// The second value is a slice of Command structs. Our slice only contains one
// Command Struct, but could contain any number of them. The first field Name
// defines the command `cf heapdump` once installed into the CLI. The
// second field, HelpText, is used by the core CLI to display help information
// to the user in the core commands `cf help`, `cf`, or `cf -h`.
func (c *JavaPlugin) GetMetadata() plugin.PluginMetadata {
	return plugin.PluginMetadata{
		Name: "java",
		Version: plugin.VersionType{
			Major: 3,
			Minor: 0,
			Build: 2,
		},
		MinCliVersion: plugin.VersionType{
			Major: 6,
			Minor: 7,
			Build: 0,
		},
		Commands: []plugin.Command{
			{
				Name:     "java",
				HelpText: "Obtain a heap-dump or thread-dump from a running, SSH-enabled Java application.",

				// UsageDetails is optional
				// It is used to show help of usage of each command
				UsageDetails: plugin.Usage{
					Usage: "cf java [" + heapDumpCommand + "|" + threadDumpCommand + "] APP_NAME",
					Options: map[string]string{
						"app-instance-index": "-i [index], select to which instance of the app to connect",
						"keep":               "-k, keep the heap dump in the container; by default the heap dump will be deleted from the container's filesystem after been downloaded",
						"dry-run":            "-n, just output to command line what would be executed",
						"container-dir":      "-cd, the directory path in the container that the heap dump file will be saved to",
						"local-dir":          "-ld, the local directory path that the dump file will be saved to",
					},
				},
			},
		},
	}
}

// Unlike most Go programs, the `Main()` function will not be used to run all of the
// commands provided in your plugin. Main will be used to initialize the plugin
// process, as well as any dependencies you might require for your
// plugin.
func main() {
	// Any initialization for your plugin can be handled here
	//
	// Note: to run the plugin.Start method, we pass in a pointer to the struct
	// implementing the interface defined at "code.cloudfoundry.org/cli/plugin/plugin.go"
	//
	// Note: The plugin's main() method is invoked at install time to collect
	// metadata. The plugin will exit 0 and the Run([]string) method will not be
	// invoked.
	plugin.Start(new(JavaPlugin))
	// Plugin code should be written in the Run([]string) method,
	// ensuring the plugin environment is bootstrapped.
}
