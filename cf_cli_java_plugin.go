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
type JavaPlugin struct{}

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
	JavaDetectionCommand              = "if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi"
	CheckNoCurrentJFRRecordingCommand = `OUTPUT=$($JCMD_COMMAND $(pidof java) JFR.check 2>&1); if [[ ! "$OUTPUT" == *"No available recording"* ]]; then echo "JFR recording already running. Stop it before starting a new recording."; exit 1; fi;`
	FilterJCMDRemoteMessage           = `filter_jcmd_remote_message() {
  if command -v grep >/dev/null 2>&1; then
    grep -v -e "Connected to remote JVM" -e "JVM response code = 0"
  else
    cat  # fallback: just pass through the input unchanged
  fi
};`
)

// Run must be implemented by any plugin because it is part of the
// plugin interface defined by the core CLI.
//
// Run(....) is the entry point when the core CLI is invoking a command defined
// by a plugin. The first parameter, plugin.CliConnection, is a struct that can
// be used to invoke cli commands. The second paramter, args, is a slice of
// strings. args[0] will be the Name of the command, and will be followed by
// any additional arguments a cli user typed in.
//
// Any error handling should be handled with the plugin itself (this means printing
// user facing errors). The CLI will exit 0 if the plugin exits 0 and will exit
// 1 should the plugin exit nonzero.
func (c *JavaPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	// Check if verbose flag is in args for early logging
	verbose := false
	for _, arg := range args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}

	if verbose {
		fmt.Printf("[VERBOSE] Run called with args: %v\n", args)
	}

	_, err := c.DoRun(&commandExecutorImpl{cliConnection: cliConnection}, &uuidGeneratorImpl{}, utils.CfJavaPluginUtilImpl{}, args)
	if err != nil {
		if verbose {
			fmt.Printf("[VERBOSE] Error occurred: %v\n", err)
		}
		if err.Error() != "unexpected EOF" {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
	if verbose {
		fmt.Printf("[VERBOSE] Run completed successfully\n")
	}
}

// DoRun is an internal method that we use to wrap the cmd package with CommandExecutor for test purposes
func (c *JavaPlugin) DoRun(commandExecutor cmd.CommandExecutor, uuidGenerator uuid.UUIDGenerator, util utils.CfJavaPluginUtil, args []string) (string, error) {
	traceLogger := trace.NewLogger(os.Stdout, true, os.Getenv("CF_TRACE"), "")
	ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), traceLogger)

	// Check if verbose flag is in args for early logging
	verbose := false
	for _, arg := range args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}

	if verbose {
		fmt.Printf("[VERBOSE] DoRun called with args: %v\n", args)
	}

	output, err := c.execute(commandExecutor, uuidGenerator, util, args)
	if err != nil {
		if err.Error() == "unexpected EOF" {
			return output, err
		}
		ui.Failed(err.Error())

		if _, invalidUsageErr := err.(*InvalidUsageError); invalidUsageErr {
			fmt.Println()
			fmt.Println()
			_, err := commandExecutor.Execute([]string{"help", "java"})
			if err != nil {
				ui.Failed("Failed to show help")
			}
		}
	} else if output != "" {
		ui.Say(output)
	}

	return output, err
}

type Command struct {
	Name                   string
	Description            string
	OnlyOnRecentSapMachine bool
	// Required tools, checked and $TOOL_COMMAND set in the remote command
	// jcmd is special: it uses asprof if available
	RequiredTools []string
	GenerateFiles bool
	NeedsFileName bool
	// use $$FILE_NAME to get the generated file Name and $$FSPATH to get the path where the file is stored
	SshCommand    string
	FilePattern   string
	FileExtension string
	FileLabel     string
	FileNamePart  string
	// Run the command in a subfolder of the container
	GenerateArbitraryFiles           bool
	GenerateArbitraryFilesFolderName string
}

// function names "HasMiscArgs" that is used on Command and checks whethere the SSHCommand contains $$ARGS
func (c *Command) HasMiscArgs() bool {
	return strings.Contains(c.SshCommand, "$$ARGS")
}

var commands = []Command{
	{
		Name:          "heap-dump",
		Description:   "Generate a heap dump from a running Java application",
		GenerateFiles: true,
		FileExtension: ".hprof",
		/*
					If there is not enough space on the filesystem to write the dump, jmap will create a file
			with size 0, output something about not enough space left on the device, and exit with status code 0.
			Because YOLO.

			Also: if the heap dump file already exists, jmap will output something about the file already
			existing and exit with status code 0. At least it is consistent.

			OpenJDK: Wrap everything in an if statement in case jmap is available
		*/
		SshCommand: `if [ -f $$FILE_NAME ]; then echo >&2 'Heap dump $$FILE_NAME already exists'; exit 1; fi
JMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])
# SAP JVM: Wrap everything in an if statement in case jvmmon is available
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])
if [ -n "${JMAP_COMMAND}" ]; then
OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=$$FILE_NAME $(pidof java) ) || STATUS_CODE=$?
if [ ! -s $$FILE_NAME ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
elif [ -n "${JVMMON_COMMAND}" ]; then
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=$$FSPATH\ndump heap' > setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
sleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file
HEAP_DUMP_NAME=$(find $$FSPATH -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done
if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
fi`,
		FileLabel:    "heap dump",
		FileNamePart: "heapdump",
	},
	{
		Name:          "thread-dump",
		Description:   "Generate a thread dump from a running Java application",
		GenerateFiles: false,
		SshCommand: "JSTACK_COMMAND=`find -executable -name jstack | head -1`; if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi; " +
			"JVMMON_COMMAND=`find -executable -name jvmmon | head -1`; if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi",
	},
	{
		Name:          "vm-info",
		Description:   "Print information about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.info | filter_jcmd_remote_message`,
	},
	{
		Name:                             "jcmd",
		Description:                      "Run a JCMD command on a running Java application via --args, downloads and deletes all files that are created in the current folder, use '--no-download' to prevent this",
		RequiredTools:                    []string{"jcmd"},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: "jcmd",
		SshCommand:                       `$JCMD_COMMAND $(pidof java) $$ARGS`,
	},
	{
		Name:          "jfr-start",
		Description:   "Start a Java Flight Recorder default recording on a running Java application (stores in the the container-dir)",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SshCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=default.jfc filename=$$FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-start-profile",
		Description:   "Start a Java Flight Recorder profile recording on a running Java application (stores in the the container-dir))",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SshCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=profile.jfc filename=$$FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc",
		Description:            "Start a Java Flight Recorder GC recording on a running Java application (stores in the the container-dir)",
		RequiredTools:          []string{"jcmd"},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileLabel:              "JFR recording",
		FileNamePart:           "jfr",
		SshCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc.jfc filename=$$FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc-details",
		Description:            "Start a Java Flight Recorder detailed GC recording on a running Java application (stores in the the container-dir)",
		RequiredTools:          []string{"jcmd"},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileLabel:              "JFR recording",
		FileNamePart:           "jfr",
		SshCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc_details.jfc filename=$$FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-stop",
		Description:   "Stop a Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.stop name=JFR | filter_jcmd_remote_message`,
	},
	{
		Name:          "jfr-dump",
		Description:   "Dump a Java Flight Recorder recording on a running Java application without stopping it",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.dump | filter_jcmd_remote_message`,
	},
	{
		Name:          "jfr-status",
		Description:   "Check the running Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.check | filter_jcmd_remote_message`,
	},
	{
		Name:          "vm-version",
		Description:   "Print the version of the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.version | filter_jcmd_remote_message`,
	},
	{
		Name:          "vm-vitals",
		Description:   "Print vital statistics about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.vitals | filter_jcmd_remote_message`,
	},
	{
		Name:                             "asprof",
		Description:                      "Run async-profiler commands passed to asprof via --args, copies files in the current folder. Don't use in combination with asprof-* commands. Downloads and deletes all files that are created in the current folder, if not using 'start' asprof command, use '--no-download' to prevent this.",
		OnlyOnRecentSapMachine:           true,
		RequiredTools:                    []string{"asprof"},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: "asprof",
		SshCommand:                       `$ASPROF_COMMAND $(pidof java) $$ARGS`,
	},
	{
		Name:                   "asprof-start-cpu",
		Description:            "Start an async-profiler CPU-time profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{"asprof"},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileNamePart:           "asprof",
		SshCommand:             `$ASPROF_COMMAND start $(pidof java) -e cpu -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-wall",
		Description:            "Start an async-profiler wall-clock profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{"asprof"},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileNamePart:           "asprof",
		SshCommand:             `$ASPROF_COMMAND start $(pidof java) -e wall -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-alloc",
		Description:            "Start an async-profiler allocation profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{"asprof"},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileNamePart:           "asprof",
		SshCommand:             `$ASPROF_COMMAND start $(pidof java) -e alloc -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-lock",
		Description:            "Start an async-profiler lock profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{"asprof"},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileNamePart:           "asprof",
		SshCommand:             `$ASPROF_COMMAND start $(pidof java) -e lock -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-stop",
		Description:            "Stop an async-profiler profile recording on a running Java application",
		RequiredTools:          []string{"asprof"},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          true,
		FileExtension:          ".jfr",
		FileLabel:              "JFR recording",
		FileNamePart:           "asprof",
		SshCommand:             `$ASPROF_COMMAND stop $(pidof java)`,
	},
	{
		Name:                   "asprof-status",
		Description:            "Get the status of async-profiler on a running Java application",
		RequiredTools:          []string{"asprof"},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          false,
		SshCommand:             `$ASPROF_COMMAND status $(pidof java)`,
	},
}

func toSentenceCase(input string) string {
	// Convert the first character to uppercase and the rest to lowercase
	if len(input) == 0 {
		return input
	}

	// Convert the first letter to uppercase
	return strings.ToUpper(string(input[0])) + strings.ToLower(input[1:])
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
		return "", &InvalidUsageError{message: fmt.Sprintf("Unexpected command Name '%s' (expected : 'java')", args[0])}
	}

	if os.Getenv("CF_TRACE") == "true" {
		return "", errors.New("the environment variable CF_TRACE is set to true. This prevents download of the dump from succeeding")
	}

	commandFlags := flags.New()

	commandFlags.NewIntFlagWithDefault("app-instance-index", "i", "application `instance` to connect to", -1)
	commandFlags.NewBoolFlag("keep", "k", "whether to `keep` the heap-dump/JFR/... files on the container of the application instance after having downloaded it locally")
	commandFlags.NewBoolFlag("no-download", "nd", "do not download the heap-dump/JFR/... file to the local machine")
	commandFlags.NewBoolFlag("dry-run", "n", "triggers the `dry-run` mode to show only the cf-ssh command that would have been executed")
	commandFlags.NewBoolFlag("verbose", "v", "enable verbose output for the plugin")
	commandFlags.NewStringFlag("container-dir", "cd", "specify the folder path where the dump/JFR/... file should be stored in the container")
	commandFlags.NewStringFlag("local-dir", "ld", "specify the folder where the dump/JFR/... file will be downloaded to, dump file wil not be copied to local if this parameter was not set")
	commandFlags.NewStringFlag("args", "a", "Miscellaneous arguments to pass to the command in the container, be aware to end it with a space if it is a simple option")

	fileFlags := []string{"container-dir", "local-dir", "keep"}

	parseErr := commandFlags.Parse(args[1:]...)
	if parseErr != nil {
		return "", &InvalidUsageError{message: fmt.Sprintf("Error while parsing command arguments: %v", parseErr)}
	}

	miscArgs := ""
	if commandFlags.IsSet("args") {
		miscArgs = commandFlags.String("args")
	}

	verbose := commandFlags.IsSet("verbose")

	// Helper function for verbose logging with format strings
	logVerbose := func(format string, args ...interface{}) {
		if verbose {
			fmt.Printf("[VERBOSE] "+format+"\n", args...)
		}
	}

	logVerbose("Starting command execution")
	logVerbose("Command arguments: %v", args)

	applicationInstance := commandFlags.Int("app-instance-index")
	noDownload := commandFlags.IsSet("no-download")
	keepAfterDownload := commandFlags.IsSet("keep") || noDownload

	logVerbose("Application instance: %d", applicationInstance)
	logVerbose("No download: %t", noDownload)
	logVerbose("Keep after download: %t", keepAfterDownload)

	remoteDir := commandFlags.String("container-dir")
	localDir := commandFlags.String("local-dir")
	if localDir == "" {
		localDir = "."
	}

	logVerbose("Remote directory: %s", remoteDir)
	logVerbose("Local directory: %s", localDir)

	arguments := commandFlags.Args()
	argumentLen := len(arguments)

	if argumentLen < 1 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	commandName := arguments[0]
	logVerbose("Command name: %s", commandName)

	index := -1
	for i, command := range commands {
		if command.Name == commandName {
			index = i
			break
		}
	}
	if index == -1 {
		avCommands := make([]string, 0, len(commands))
		for _, command := range commands {
			avCommands = append(avCommands, command.Name)
		}
		matches := utils.FuzzySearch(commandName, avCommands, 3)
		return "", &InvalidUsageError{message: fmt.Sprintf("Unrecognized command %q, did you mean: %s?", commandName, utils.JoinWithOr(matches))}
	}

	command := commands[index]
	logVerbose("Found command: %s - %s", command.Name, command.Description)
	if !command.GenerateFiles && !command.GenerateArbitraryFiles {
		logVerbose("Command does not generate files, checking for invalid file flags")
		for _, flag := range fileFlags {
			if commandFlags.IsSet(flag) {
				logVerbose("Invalid flag %q detected for command %s", flag, command.Name)
				return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", flag, command.Name)}
			}
		}
	}
	if command.Name == "asprof" {
		trimmedMiscArgs := strings.TrimLeft(miscArgs, " ")
		if len(trimmedMiscArgs) > 6 && trimmedMiscArgs[:6] == "start " {
			noDownload = true
			logVerbose("asprof start command detected, setting noDownload to true")
		} else {
			noDownload = trimmedMiscArgs == "start"
			if noDownload {
				logVerbose("asprof start command detected, setting noDownload to true")
			}
		}
	}
	if !command.HasMiscArgs() && commandFlags.IsSet("args") {
		logVerbose("Command %s does not support --args flag", command.Name)
		return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "args", command.Name)}
	}
	if argumentLen == 1 {
		return "", &InvalidUsageError{message: "No application name provided"}
	} else if argumentLen > 2 {
		return "", &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	applicationName := arguments[1]
	logVerbose("Application name: %s", applicationName)

	cfSSHArguments := []string{"ssh", applicationName}
	if applicationInstance > 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(applicationInstance))
	}

	logVerbose("CF SSH arguments: %v", cfSSHArguments)

	supported, err := util.CheckRequiredTools(applicationName)

	if err != nil || !supported {
		return "required tools checking failed", err
	}

	logVerbose("Required tools check passed")

	var remoteCommandTokens = []string{JavaDetectionCommand}

	logVerbose("Building remote command tokens")
	logVerbose("Java detection command: %s", JavaDetectionCommand)

	for _, requiredTool := range command.RequiredTools {
		logVerbose("Setting up required tool: %s", requiredTool)
		uppercase := strings.ToUpper(requiredTool)
		var toolCommand = fmt.Sprintf("%s_COMMAND=$(realpath $(find -executable -name %s | head -1 | tr -d [:space:])); if [ -z \"${%s_COMMAND}\" ]; then echo \"%s not found\"; exit 1; fi", uppercase, requiredTool, uppercase, requiredTool)
		if requiredTool == "jcmd" {
			// add code that first checks whether asprof is present and if so use `asprof jcmd` instead of `jcmd`
			remoteCommandTokens = append(remoteCommandTokens, toolCommand, "ASPROF_COMMAND=$(realpath $(find -executable -name asprof | head -1 | tr -d [:space:])); if [ -n \"${ASPROF_COMMAND}\" ]; then JCMD_COMMAND=\"${ASPROF_COMMAND} jcmd\"; fi")
			logVerbose("Added jcmd with asprof fallback")
		} else {
			remoteCommandTokens = append(remoteCommandTokens, toolCommand)
			logVerbose("Added tool command for %s", requiredTool)
		}
	}
	fileName := ""
	fspath := remoteDir

	var replacements = map[string]string{
		"$$ARGS":     miscArgs,
		"$$APP_NAME": applicationName,
	}

	if command.GenerateFiles || command.NeedsFileName || command.GenerateArbitraryFiles {
		logVerbose("Command requires file generation")
		fspath, err = util.GetAvailablePath(applicationName, remoteDir)
		if err != nil {
			return "", err
		}
		logVerbose("Available path: %s", fspath)
		if command.GenerateArbitraryFiles {
			fspath = fspath + "/" + command.GenerateArbitraryFilesFolderName
			logVerbose("Updated path for arbitrary files: %s", fspath)
		}

		fileName = fspath + "/" + applicationName + "-" + command.FileNamePart + "-" + uuidGenerator.Generate() + command.FileExtension
		logVerbose("Generated filename: %s", fileName)
		replacements["$$FILE_NAME"] = fileName
		replacements["$$FSPATH"] = fspath
		if command.GenerateArbitraryFiles {
			// prepend 'mkdir -p $$FSPATH' to the command to create the directory if it does not exist
			remoteCommandTokens = append([]string{"mkdir -p " + fspath}, remoteCommandTokens...)
			remoteCommandTokens = append(remoteCommandTokens, "cd "+fspath)
			logVerbose("Added directory creation and navigation commands for: %s", fspath)
		}
	}

	var commandText = command.SshCommand
	for key, value := range replacements {
		commandText = strings.ReplaceAll(commandText, key, value)
	}
	remoteCommandTokens = append(remoteCommandTokens, commandText)

	logVerbose("Command text after replacements: %s", commandText)
	logVerbose("Full remote command tokens: %v", remoteCommandTokens)

	cfSSHArguments = append(cfSSHArguments, "--command")
	remoteCommand := strings.Join(remoteCommandTokens, "; ")

	logVerbose("Final remote command: %s", remoteCommand)

	if commandFlags.IsSet("dry-run") {
		logVerbose("Dry-run mode enabled, returning command without execution")
		// When printing out the entire command line for separate execution, we wrap the remote command in single quotes
		// to prevent the shell processing it from running it in local
		cfSSHArguments = append(cfSSHArguments, "'"+remoteCommand+"'")
		return "cf " + strings.Join(cfSSHArguments, " "), nil
	}

	fullCommand := append(cfSSHArguments, remoteCommand)
	logVerbose("Executing command: %v", fullCommand)

	output, err := commandExecutor.Execute(fullCommand)
	logVerbose("Command execution completed")

	if command.GenerateFiles && !noDownload {
		logVerbose("Processing file generation and download")

		finalFile := ""
		var err error
		switch command.FileExtension {
		case ".hprof":
			logVerbose("Finding heap dump file")
			finalFile, err = util.FindHeapDumpFile(cfSSHArguments, fileName, fspath)
		case ".jfr":
			logVerbose("Finding JFR file")
			finalFile, err = util.FindJFRFile(cfSSHArguments, fileName, fspath)
		default:
			return "", &InvalidUsageError{message: fmt.Sprintf("Unsupported file extension %q", command.FileExtension)}
		}
		if err == nil && finalFile != "" {
			fileName = finalFile
			logVerbose("Found file: %s", finalFile)
			fmt.Println("Successfully created " + command.FileLabel + " in application container at: " + fileName)
		} else {
			logVerbose("Failed to find file, error: %v", err)
			fmt.Println("Failed to find " + command.FileLabel + " in application container")
			return "", err
		}

		localFileFullPath := localDir + "/" + applicationName + "-" + command.FileNamePart + "-" + uuidGenerator.Generate() + command.FileExtension
		logVerbose("Downloading file to: %s", localFileFullPath)
		err = util.CopyOverCat(cfSSHArguments, fileName, localFileFullPath)
		if err == nil {
			logVerbose("File download completed successfully")
			fmt.Println(toSentenceCase(command.FileLabel) + " file saved to: " + localFileFullPath)
		} else {
			logVerbose("File download failed: %v", err)
			return "", err
		}

		if !keepAfterDownload {
			logVerbose("Deleting remote file")
			err = util.DeleteRemoteFile(cfSSHArguments, fileName)
			if err != nil {
				logVerbose("Failed to delete remote file: %v", err)
				return "", err
			}
			logVerbose("Remote file deleted successfully")
			fmt.Println(toSentenceCase(command.FileLabel) + " file deleted in application container")
		} else {
			logVerbose("Keeping remote file as requested")
		}
	}
	if command.GenerateArbitraryFiles && !noDownload {
		logVerbose("Processing arbitrary files download: %s", fspath)
		logVerbose("cfSSHArguments: %v", cfSSHArguments)
		// download all files in the generic folder
		files, err := util.ListFiles(cfSSHArguments, fspath)
		for i, file := range files {
			logVerbose("File %d: %s", i+1, file)
		}
		if err != nil {
			logVerbose("Failed to list files: %v", err)
			return "", err
		}
		logVerbose("Found %d files to download", len(files))
		if len(files) != 0 {
			for _, file := range files {
				logVerbose("Downloading file: %s", file)
				localFileFullPath := localDir + "/" + file
				err = util.CopyOverCat(cfSSHArguments, fspath+"/"+file, localFileFullPath)
				if err == nil {
					logVerbose("File %s downloaded successfully", file)
					fmt.Printf("File %s saved to: %s\n", file, localFileFullPath)
				} else {
					logVerbose("Failed to download file %s: %v", file, err)
					return "", err
				}
			}

			if !keepAfterDownload {
				logVerbose("Deleting remote file folder")
				err = util.DeleteRemoteFile(cfSSHArguments, fspath)
				if err != nil {
					logVerbose("Failed to delete remote folder: %v", err)
					return "", err
				}
				logVerbose("Remote folder deleted successfully")
				fmt.Println("File folder deleted in application container")
			} else {
				logVerbose("Keeping remote files as requested")
			}
		} else {
			logVerbose("No files found to download")
		}
	}
	// We keep this around to make the compiler happy, but commandExecutor.Execute will cause an os.Exit
	logVerbose("Command execution completed successfully")
	return strings.Join(output, "\n"), err
}

// GetMetadata must be implemented as part of the plugin interface
// defined by the core CLI.
//
// GetMetadata() returns a PluginMetadata struct. The first field, Name,
// determines the Name of the plugin which should generally be without spaces.
// If there are spaces in the Name a user will need to properly quote the Name
// during uninstall otherwise the Name will be treated as seperate arguments.
// The second value is a slice of Command structs. Our slice only contains one
// Command Struct, but could contain any number of them. The first field Name
// defines the command `cf heapdump` once installed into the CLI. The
// second field, HelpText, is used by the core CLI to display help information
// to the user in the core commands `cf help`, `cf`, or `cf -h`.
func (c *JavaPlugin) GetMetadata() plugin.PluginMetadata {
	var usageText = "cf java COMMAND APP_NAME [options]"
	for _, command := range commands {
		usageText += "\n\n     " + command.Name
		if command.OnlyOnRecentSapMachine || command.HasMiscArgs() {
			usageText += " ("
			if command.OnlyOnRecentSapMachine {
				usageText += "recent SapMachine only"
			}
			if command.HasMiscArgs() {
				if command.OnlyOnRecentSapMachine {
					usageText += ", "
				}
				usageText += "supports --args"
			}
			usageText += ")"
		}
		usageText += "\n        " + command.Description
	}
	return plugin.PluginMetadata{
		Name: "java",
		Version: plugin.VersionType{
			Major: 4,
			Minor: 0,
			Build: 0,
		},
		MinCliVersion: plugin.VersionType{
			Major: 6,
			Minor: 7,
			Build: 0,
		},
		Commands: []plugin.Command{
			{
				Name:     "java",
				HelpText: "Obtain a heap-dump, thread-dump or profile from a running, SSH-enabled Java application.",

				// UsageDetails is optional
				// It is used to show help of usage of each command
				UsageDetails: plugin.Usage{
					Usage: usageText,
					Options: map[string]string{
						"app-instance-index": "-i [index], select to which instance of the app to connect",
						"no-download":        "-nd, don't download the heap dump/JFR/... file to local, only keep it in the container, implies '--keep'",
						"keep":               "-k, keep the heap dump in the container; by default the heap dump/JFR/... will be deleted from the container's filesystem after been downloaded",
						"dry-run":            "-n, just output to command line what would be executed",
						"container-dir":      "-cd, the directory path in the container that the heap dump/JFR/... file will be saved to",
						"local-dir":          "-ld, the local directory path that the dump/JFR/... file will be saved to, defaults to the current directory",
						"args":               "-a, Miscellaneous arguments to pass to the command (if supported) in the container, be aware to end it with a space if it is a simple option",
						"verbose":            "-v, enable verbose output for the plugin",
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
