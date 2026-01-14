/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

// Package main implements a CF CLI plugin for Java applications, providing commands
// for heap dumps, thread dumps, profiling, and other Java diagnostics.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"code.cloudfoundry.org/cli/cf/terminal"
	"code.cloudfoundry.org/cli/cf/trace"
	"code.cloudfoundry.org/cli/plugin"

	"cf.plugin.ref/requires/utils"

	"github.com/simonleung8/flags"
)

// Assert that JavaPlugin implements plugin.Plugin.
var _ plugin.Plugin = (*JavaPlugin)(nil)

// JavaPlugin is a CF CLI plugin that supports taking heap and thread dumps on demand
type JavaPlugin struct {
	verbose bool
}

// logVerbosef logs a message with a format string if verbose mode is enabled
func (c *JavaPlugin) logVerbosef(format string, args ...any) {
	if c.verbose {
		fmt.Printf("[VERBOSE] "+format+"\n", args...)
	}
}

// InvalidUsageError indicates that the arguments passed as input to the command are invalid
type InvalidUsageError struct {
	message string
}

func (e InvalidUsageError) Error() string {
	return e.message
}

// Options holds all command-line options for the Java plugin
type Options struct {
	AppInstanceIndex int
	Keep             bool
	NoDownload       bool
	DryRun           bool
	Verbose          bool
	ContainerDir     string
	LocalDir         string
	Args             string
}

// FlagDefinition holds metadata for a command-line flag
type FlagDefinition struct {
	Name        string
	ShortName   string
	Usage       string
	Description string // Longer description for help text
	Type        string
	DefaultInt  int
}

// flagDefinitions contains all flag definitions in a centralized location
var flagDefinitions = []FlagDefinition{
	{
		Name:        "app-instance-index",
		ShortName:   "i",
		Usage:       "application `instance` to connect to",
		Description: "select to which instance of the app to connect",
		Type:        "int",
		DefaultInt:  0,
	},
	{
		Name:        "keep",
		ShortName:   "k",
		Usage:       "whether to `keep` the heap-dump/JFR/... files on the container of the application instance after having downloaded it locally",
		Description: "keep the heap dump in the container; by default the heap dump/JFR/... will be deleted from the container's filesystem after being downloaded",
		Type:        "bool",
	},
	{
		Name:        "no-download",
		ShortName:   "nd",
		Usage:       "do not download the heap-dump/JFR/... file to the local machine",
		Description: "don't download the heap dump/JFR/... file to local, only keep it in the container, implies '--keep'",
		Type:        "bool",
	},
	{
		Name:        "dry-run",
		ShortName:   "n",
		Usage:       "triggers the `dry-run` mode to show only the cf-ssh command that would have been executed",
		Description: "just output to command line what would be executed",
		Type:        "bool",
	},
	{
		Name:        "verbose",
		ShortName:   "v",
		Usage:       "enable verbose output for the plugin",
		Description: "enable verbose output for the plugin",
		Type:        "bool",
	},
	{
		Name:        "container-dir",
		ShortName:   "cd",
		Usage:       "specify the folder path where the dump/JFR/... file should be stored in the container",
		Description: "the directory path in the container that the heap dump/JFR/... file will be saved to",
		Type:        "string",
	},
	{
		Name:        "local-dir",
		ShortName:   "ld",
		Usage:       "specify the folder where the dump/JFR/... file will be downloaded to, dump file will not be copied to local if this parameter was not set",
		Description: "the local directory path that the dump/JFR/... file will be saved to, defaults to the current directory",
		Type:        "string",
	},
	{
		Name:        "args",
		ShortName:   "a",
		Usage:       "Miscellaneous arguments to pass to the command in the container, be aware to end it with a space if it is a simple option",
		Description: "Miscellaneous arguments to pass to the command (if supported) in the container, be aware to end it with a space if it is a simple option. For commands that create arbitrary files (jcmd, asprof), the environment variables @FSPATH, @ARGS, @APP_NAME, @FILE_NAME, and @STATIC_FILE_NAME are available in --args to reference the working directory path, arguments, application name, and generated file name respectively.",
		Type:        "string",
	},
}

func (c *JavaPlugin) createOptionsParser() flags.FlagContext {
	commandFlags := flags.New()

	// Create flags from centralized definitions
	for _, flagDef := range flagDefinitions {
		switch flagDef.Type {
		case "int":
			commandFlags.NewIntFlagWithDefault(flagDef.Name, flagDef.ShortName, flagDef.Usage, flagDef.DefaultInt)
		case "bool":
			commandFlags.NewBoolFlag(flagDef.Name, flagDef.ShortName, flagDef.Usage)
		case "string":
			commandFlags.NewStringFlag(flagDef.Name, flagDef.ShortName, flagDef.Usage)
		}
	}

	return commandFlags
}

// parseOptions creates and parses command-line flags, returning the Options struct
func (c *JavaPlugin) parseOptions(args []string) (*Options, []string, error) {
	commandFlags := c.createOptionsParser()
	parseErr := commandFlags.Parse(args...)
	if parseErr != nil {
		return nil, nil, parseErr
	}

	options := &Options{
		AppInstanceIndex: commandFlags.Int("app-instance-index"),
		Keep:             commandFlags.IsSet("keep"),
		NoDownload:       commandFlags.IsSet("no-download"),
		DryRun:           commandFlags.IsSet("dry-run"),
		Verbose:          commandFlags.IsSet("verbose"),
		ContainerDir:     commandFlags.String("container-dir"),
		LocalDir:         commandFlags.String("local-dir"),
		Args:             commandFlags.String("args"),
	}

	return options, commandFlags.Args(), nil
}

// generateOptionsMapFromFlags creates the options map for plugin metadata
func (c *JavaPlugin) generateOptionsMapFromFlags() map[string]string {
	options := make(map[string]string)

	// Generate options from the centralized flag definitions
	for _, flagDef := range flagDefinitions {
		// Create the prefix for the flag (short name with appropriate formatting)
		prefix := "-" + flagDef.ShortName
		if flagDef.Name == "app-instance-index" {
			prefix += " [index]"
		}
		prefix += ", "

		// Use the Description field for detailed help text
		options[flagDef.Name] = utils.WrapTextWithPrefix(flagDef.Description, prefix, 80, 27)
	}

	return options
}

const (
	// JavaDetectionCommand is the prologue command to detect if the Garden container contains a Java app.
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
// Run(...) is the entry point when the core CLI is invoking a command defined
// by a plugin. The first parameter, plugin.CliConnection, is a struct that can
// be used to invoke CLI commands. The second parameter, args, is a slice of
// strings. args[0] will be the name of the command, and will be followed by
// any additional arguments a CLI user typed in.
//
// Any error handling should be handled within the plugin itself (this means printing
// user-facing errors). The CLI will exit 0 if the plugin exits 0 and will exit
// 1 should the plugin exit nonzero.
func (c *JavaPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	// Check if verbose flag is in args for early logging
	for _, arg := range args {
		if arg == "-v" || arg == "--verbose" {
			c.verbose = true
			break
		}
	}

	c.logVerbosef("Run called with args: %v", args)

	_, err := c.DoRun(cliConnection, args)
	if err != nil {
		c.logVerbosef("Error occurred: %v", err)
		os.Exit(1)
	}
	c.logVerbosef("Run completed successfully")
}

// DoRun is an internal method used to wrap the cmd package with CommandExecutor for test purposes
func (c *JavaPlugin) DoRun(cliConnection plugin.CliConnection, args []string) (string, error) {
	traceLogger := trace.NewLogger(os.Stdout, true, os.Getenv("CF_TRACE"), "")
	ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), traceLogger)

	c.logVerbosef("DoRun called with args: %v", args)

	output, err := c.execute(cliConnection, args)
	if err != nil {
		if err.Error() == "unexpected EOF" {
			return output, err
		}
		ui.Failed(err.Error())

		var invalidUsageErr *InvalidUsageError
		if errors.As(err, &invalidUsageErr) {
			fmt.Println()
			fmt.Println()
			err := exec.Command("cf", "help", "java").Run()
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
	// Use @ prefix to avoid shell expansion issues, replaced directly in Go code
	// use @FILE_NAME to get the generated file name with a random UUID,
	// @STATIC_FILE_NAME without, and @FSPATH to get the path where the file is stored (for GenerateArbitraryFiles commands)
	SSHCommand    string
	FilePattern   string
	FileExtension string
	FileLabel     string
	FileNamePart  string
	// Run the command in a subfolder of the container
	GenerateArbitraryFiles           bool
	GenerateArbitraryFilesFolderName string
}

// HasMiscArgs checks whether the SSHCommand contains @ARGS
func (c *Command) HasMiscArgs() bool {
	return strings.Contains(c.SSHCommand, "@ARGS")
}

// replaceVariables replaces @-prefixed variables in the command with actual values.
// Returns the processed command string and an error if validation fails.
func (c *JavaPlugin) replaceVariables(command, appName, fspath, fileName, staticFileName, args string) (string, error) {
	// Validate: @ARGS cannot contain itself, other variables cannot contain any @ variables
	if strings.Contains(args, "@ARGS") {
		return "", fmt.Errorf("invalid variable reference: @ARGS cannot contain itself")
	}
	for varName, value := range map[string]string{"@APP_NAME": appName, "@FSPATH": fspath, "@FILE_NAME": fileName, "@STATIC_FILE_NAME": staticFileName} {
		if strings.Contains(value, "@") {
			return "", fmt.Errorf("invalid variable reference: %s cannot contain @ variables", varName)
		}
	}

	// First, replace variables within @ARGS value itself
	processedArgs := args
	processedArgs = strings.ReplaceAll(processedArgs, "@APP_NAME", appName)
	processedArgs = strings.ReplaceAll(processedArgs, "@FSPATH", fspath)
	processedArgs = strings.ReplaceAll(processedArgs, "@FILE_NAME", fileName)
	processedArgs = strings.ReplaceAll(processedArgs, "@STATIC_FILE_NAME", staticFileName)

	// Then replace all variables in the command template
	result := command
	result = strings.ReplaceAll(result, "@APP_NAME", appName)
	result = strings.ReplaceAll(result, "@FSPATH", fspath)
	result = strings.ReplaceAll(result, "@FILE_NAME", fileName)
	result = strings.ReplaceAll(result, "@STATIC_FILE_NAME", staticFileName)
	result = strings.ReplaceAll(result, "@ARGS", processedArgs)

	return result, nil
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
		SSHCommand: `if [ -f @FILE_NAME ]; then echo >&2 'Heap dump @FILE_NAME already exists'; exit 1; fi
JMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])
# SAP JVM: Wrap everything in an if statement in case jvmmon is available
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])
# if we have neither jmap nor jvmmon, we cannot generate a heap dump and should exit with an error
if [ -z "${JMAP_COMMAND}" ] && [ -z "${JVMMON_COMMAND}" ]; then
  echo >&2 "jvmmon or jmap are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
		---
		applications:
		- name: <APP_NAME>
		  memory: 1G
		  path: <PATH_TO_BUILD_ARTIFACT>
		  buildpack: https://github.com/cloudfoundry/java-buildpack
		  env:
			JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64", version: 21.+ } }'
		
	"
  exit 1
fi
if [ -n "${JMAP_COMMAND}" ]; then
OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=@FILE_NAME $(pidof java) ) || STATUS_CODE=$?
if [ ! -s @FILE_NAME ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
elif [ -n "${JVMMON_COMMAND}" ]; then
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=@FSPATH\ndump heap' > setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
sleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file
HEAP_DUMP_NAME=$(find @FSPATH -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
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
		SSHCommand: `JSTACK_COMMAND=$(find -executable -name jstack | head -1);
		JVMMON_COMMAND=$(find -executable -name jvmmon | head -1) 
		if [ -z "${JVMMON_COMMAND}" ] && [ -z "${JSTACK_COMMAND}" ]; then
		echo >&2 "jstack or jvmmon are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
				---
				applications:
				- name: <APP_NAME>
				memory: 1G
				path: <PATH_TO_BUILD_ARTIFACT>
				buildpack: https://github.com/cloudfoundry/java-buildpack
				env:
					JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64", version: 21.+ } }'
				
			"
		exit 1
		fi
		if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi;
		if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi`,
	},
	{
		Name:          "vm-info",
		Description:   "Print information about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.info | filter_jcmd_remote_message`,
	},
	{
		Name:                             "jcmd",
		Description:                      "Run a JCMD command on a running Java application via --args, downloads and deletes all files that are created in the current folder, use '--no-download' to prevent this. Environment variables available: @FSPATH (writable directory path, always set), @ARGS (command arguments), @APP_NAME (application name), @FILE_NAME (generated filename for file operations without UUID), and @STATIC_FILE_NAME (without UUID). Use single quotes around --args to prevent shell expansion.",
		RequiredTools:                    []string{"jcmd"},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: "jcmd",
		SSHCommand:                       `$JCMD_COMMAND $(pidof java) @ARGS`,
	},
	{
		Name:          "jfr-start",
		Description:   "Start a Java Flight Recorder default recording on a running Java application (stores in the container-dir)",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=default.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-start-profile",
		Description:   "Start a Java Flight Recorder profile recording on a running Java application (stores in the container-dir))",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=profile.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc",
		Description:            "Start a Java Flight Recorder GC recording on a running Java application (stores in the container-dir)",
		RequiredTools:          []string{"jcmd"},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileLabel:              "JFR recording",
		FileNamePart:           "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc-details",
		Description:            "Start a Java Flight Recorder detailed GC recording on a running Java application (stores in the container-dir)",
		RequiredTools:          []string{"jcmd"},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          ".jfr",
		FileLabel:              "JFR recording",
		FileNamePart:           "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc_details.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-stop",
		Description:   "Stop a Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + ` output=$($JCMD_COMMAND $(pidof java) JFR.stop name=JFR | filter_jcmd_remote_message);
		echo "$output"; echo ""; filename=$(echo "$output" | grep /.*.jfr --only-matching);
		if [ -z "$filename" ]; then echo "No JFR recording created"; exit 1; fi;
		if [ ! -f "$filename" ]; then echo "JFR recording $filename does not exist"; exit 1; fi;
		if [ ! -s "$filename" ]; then echo "JFR recording $filename is empty"; exit 1; fi;
		mv "$filename" @FILE_NAME;
		echo "JFR recording copied to @FILE_NAME"`,
	},
	{
		Name:          "jfr-dump",
		Description:   "Dump a Java Flight Recorder recording on a running Java application without stopping it",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: ".jfr",
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + ` output=$($JCMD_COMMAND $(pidof java) JFR.dump name=JFR | filter_jcmd_remote_message);
		echo "$output"; echo ""; filename=$(echo "$output" | grep /.*.jfr --only-matching);
		if [ -z "$filename" ]; then echo "No JFR recording created"; exit 1; fi;
		if [ ! -f "$filename" ]; then echo "JFR recording $filename does not exist"; exit 1; fi;
		if [ ! -s "$filename" ]; then echo "JFR recording $filename is empty"; exit 1; fi;
		cp "$filename" @FILE_NAME;
		echo "JFR recording copied to @FILE_NAME";
		echo "Use 'cf java jfr-stop @APP_NAME' to stop the recording and copy the final JFR file to the local folder"`,
	},
	{
		Name:          "jfr-status",
		Description:   "Check the running Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.check | filter_jcmd_remote_message`,
	},
	{
		Name:          "vm-version",
		Description:   "Print the version of the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.version | filter_jcmd_remote_message`,
	},
	{
		Name:          "vm-vitals",
		Description:   "Print vital statistics about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.vitals | filter_jcmd_remote_message`,
	},
	{
		Name:                             "asprof",
		Description:                      "Run async-profiler commands passed to asprof via --args, copies files in the current folder. Don't use in combination with asprof-* commands. Downloads and deletes all files that are created in the current folder, if not using 'start' asprof command, use '--no-download' to prevent this. Environment variables available: @FSPATH (writable directory path, always set), @ARGS (command arguments), @APP_NAME (application name), @FILE_NAME (generated filename for file operations), and @STATIC_FILE_NAME (without UUID). Use single quotes around --args to prevent shell expansion.",
		OnlyOnRecentSapMachine:           true,
		RequiredTools:                    []string{"asprof"},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: "asprof",
		SSHCommand:                       `$ASPROF_COMMAND $(pidof java) @ARGS`,
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
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e cpu -f @FILE_NAME; echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e wall -f @FILE_NAME; echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e alloc -f @FILE_NAME; echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e lock -f @FILE_NAME; echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand:             `$ASPROF_COMMAND stop $(pidof java)`,
	},
	{
		Name:                   "asprof-status",
		Description:            "Get the status of async-profiler on a running Java application",
		RequiredTools:          []string{"asprof"},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          false,
		SSHCommand:             `$ASPROF_COMMAND status $(pidof java)`,
	},
}

func (c *JavaPlugin) execute(_ plugin.CliConnection, args []string) (string, error) {
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

	options, arguments, parseErr := c.parseOptions(args[1:])
	if parseErr != nil {
		return "", &InvalidUsageError{message: fmt.Sprintf("Error while parsing command arguments: %v", parseErr)}
	}

	fileFlags := []string{"container-dir", "local-dir", "keep", "no-download"}

	c.logVerbosef("Starting command execution")
	c.logVerbosef("Command arguments: %v", args)

	noDownload := options.NoDownload
	keepAfterDownload := options.Keep || noDownload

	c.logVerbosef("Application instance: %d", options.AppInstanceIndex)
	c.logVerbosef("No download: %t", noDownload)
	c.logVerbosef("Keep after download: %t", keepAfterDownload)

	remoteDir := options.ContainerDir
	// strip trailing slashes from remoteDir
	remoteDir = strings.TrimRight(remoteDir, "/")
	localDir := options.LocalDir
	if localDir == "" {
		localDir = "."
	}

	c.logVerbosef("Remote directory: %s", remoteDir)
	c.logVerbosef("Local directory: %s", localDir)

	argumentLen := len(arguments)

	if argumentLen < 1 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	commandName := arguments[0]
	c.logVerbosef("Command name: %s", commandName)

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
	c.logVerbosef("Found command: %s - %s", command.Name, command.Description)
	if !command.GenerateFiles && !command.GenerateArbitraryFiles {
		c.logVerbosef("Command does not generate files, checking for invalid file flags")
		for _, flag := range fileFlags {
			if (flag == "container-dir" && options.ContainerDir != "") ||
				(flag == "local-dir" && options.LocalDir != "") ||
				(flag == "keep" && options.Keep) ||
				(flag == "no-download" && options.NoDownload) {
				c.logVerbosef("Invalid flag %q detected for command %s", flag, command.Name)
				return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", flag, command.Name)}
			}
		}
	}
	if command.Name == "asprof" {
		trimmedMiscArgs := strings.TrimLeft(options.Args, " ")
		if len(trimmedMiscArgs) > 6 && trimmedMiscArgs[:6] == "start " {
			noDownload = true
			c.logVerbosef("asprof start command detected, setting noDownload to true")
		} else {
			noDownload = trimmedMiscArgs == "start"
			if noDownload {
				c.logVerbosef("asprof start command detected, setting noDownload to true")
			}
		}
	}
	if !command.HasMiscArgs() && options.Args != "" {
		c.logVerbosef("Command %s does not support --args flag", command.Name)
		return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "args", command.Name)}
	}
	if argumentLen == 1 {
		return "", &InvalidUsageError{message: "No application name provided"}
	} else if argumentLen > 2 {
		return "", &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	applicationName := arguments[1]
	c.logVerbosef("Application name: %s", applicationName)

	cfSSHArguments := []string{"ssh", applicationName}
	if options.AppInstanceIndex > 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(options.AppInstanceIndex))
	}
	if options.AppInstanceIndex < 0 {
		// indexes can't be negative, so fail with an error
		return "", &InvalidUsageError{message: fmt.Sprintf("Invalid application instance index %d, must be >= 0", options.AppInstanceIndex)}
	}

	c.logVerbosef("CF SSH arguments: %v", cfSSHArguments)

	supported, err := utils.CheckRequiredTools(applicationName)

	if err != nil || !supported {
		return "required tools checking failed", err
	}

	c.logVerbosef("Required tools check passed")

	remoteCommandTokens := []string{JavaDetectionCommand}

	c.logVerbosef("Building remote command tokens")
	c.logVerbosef("Java detection command: %s", JavaDetectionCommand)

	for _, requiredTool := range command.RequiredTools {
		c.logVerbosef("Setting up required tool: %s", requiredTool)
		uppercase := strings.ToUpper(requiredTool)
		toolCommand := fmt.Sprintf(`%[1]s_TOOL_PATH=$(find -executable -name %[2]s | head -1 | tr -d [:space:]); if [ -z "$%[1]s_TOOL_PATH" ]; then echo "%[2]s not found"; exit 1; fi; %[1]s_COMMAND=$(realpath "$%[1]s_TOOL_PATH")`, uppercase, requiredTool)
		if requiredTool == "jcmd" {
			// add code that first checks whether asprof is present and if so use `asprof jcmd` instead of `jcmd`
			remoteCommandTokens = append(remoteCommandTokens, toolCommand, "ASPROF_COMMAND=$(realpath $(find -executable -name asprof | head -1 | tr -d [:space:])); if [ -n \"${ASPROF_COMMAND}\" ]; then JCMD_COMMAND=\"${ASPROF_COMMAND} jcmd\"; fi")
			c.logVerbosef("Added jcmd with asprof fallback")
		} else {
			remoteCommandTokens = append(remoteCommandTokens, toolCommand)
			c.logVerbosef("Added tool command for %s", requiredTool)
		}
	}
	fileName := ""
	staticFileName := ""
	fspath := remoteDir

	// Initialize fspath and fileName for commands that need them
	if command.GenerateFiles || command.NeedsFileName || command.GenerateArbitraryFiles {
		c.logVerbosef("Command requires file generation")
		fspath, err = utils.GetAvailablePath(applicationName, remoteDir)
		if err != nil {
			return "", fmt.Errorf("failed to get available path: %w", err)
		}
		if fspath == "" {
			return "", fmt.Errorf("no available path found for file generation")
		}
		c.logVerbosef("Available path: %s", fspath)

		if command.GenerateArbitraryFiles {
			fspath = fspath + "/" + command.GenerateArbitraryFilesFolderName
			c.logVerbosef("Updated path for arbitrary files: %s", fspath)
		}

		fileName = fspath + "/" + applicationName + "-" + command.FileNamePart + "-" + utils.GenerateUUID() + command.FileExtension
		staticFileName = fspath + "/" + applicationName + command.FileNamePart + command.FileExtension
		c.logVerbosef("Generated filename: %s", fileName)
		c.logVerbosef("Generated static filename without UUID: %s", staticFileName)
	}

	commandText := command.SSHCommand
	// Perform variable replacements directly in Go code
	var err2 error
	commandText, err2 = c.replaceVariables(commandText, applicationName, fspath, fileName, staticFileName, options.Args)
	if err2 != nil {
		return "", fmt.Errorf("variable replacement failed: %w", err2)
	}

	// For arbitrary files commands, insert mkdir and cd before the main command
	if command.GenerateArbitraryFiles {
		remoteCommandTokens = append(remoteCommandTokens, "mkdir -p "+fspath, "cd "+fspath, commandText)
		c.logVerbosef("Added directory creation and navigation before command execution")
	} else {
		remoteCommandTokens = append(remoteCommandTokens, commandText)
	}

	c.logVerbosef("Command text after replacements: %s", commandText)
	c.logVerbosef("Full remote command tokens: %v", remoteCommandTokens)

	cfSSHArguments = append(cfSSHArguments, "--command")
	remoteCommand := strings.Join(remoteCommandTokens, "; ")

	c.logVerbosef("Final remote command: %s", remoteCommand)

	if options.DryRun {
		c.logVerbosef("Dry-run mode enabled, returning command without execution")
		// When printing out the entire command line for separate execution, we wrap the remote command in single quotes
		// to prevent the shell processing it from running it in local
		cfSSHArguments = append(cfSSHArguments, "'"+remoteCommand+"'")
		return "cf " + strings.Join(cfSSHArguments, " "), nil
	}

	fullCommand := append([]string{}, cfSSHArguments...)
	fullCommand = append(fullCommand, remoteCommand)
	c.logVerbosef("Executing command: %v", fullCommand)

	cmdArgs := append([]string{"cf"}, fullCommand...)
	c.logVerbosef("Executing command: %v", cmdArgs)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	outputBytes, err := cmd.CombinedOutput()
	output := strings.TrimRight(string(outputBytes), "\n")
	if err != nil {
		if err.Error() == "unexpected EOF" {
			return "", fmt.Errorf("Command failed")
		}
		if len(output) == 0 {
			return "", fmt.Errorf("Command execution failed: %w", err)
		}
		return "", fmt.Errorf("Command execution failed: %w\nOutput: %s", err, output)
	}

	if command.GenerateFiles {
		c.logVerbosef("Processing file generation and download")

		var finalFile string
		var err error
		switch command.FileExtension {
		case ".hprof":
			c.logVerbosef("Finding heap dump file")
			finalFile, err = utils.FindHeapDumpFile(cfSSHArguments, fileName, fspath)
		case ".jfr":
			c.logVerbosef("Finding JFR file")
			finalFile, err = utils.FindJFRFile(cfSSHArguments, fileName, fspath)
		default:
			return "", &InvalidUsageError{message: fmt.Sprintf("Unsupported file extension %q", command.FileExtension)}
		}
		if err == nil && finalFile != "" {
			fileName = finalFile
			c.logVerbosef("Found file: %s", finalFile)
			fmt.Println("Successfully created " + command.FileLabel + " in application container at: " + fileName)
		} else if !noDownload {
			c.logVerbosef("Failed to find file, error: %v", err)
			fmt.Println("Failed to find " + command.FileLabel + " in application container")
			return "", err
		}

		if noDownload {
			fmt.Println("No download requested, skipping file download")
			return output, nil
		}

		localFileFullPath := localDir + "/" + applicationName + "-" + command.FileNamePart + "-" + utils.GenerateUUID() + command.FileExtension
		c.logVerbosef("Downloading file to: %s", localFileFullPath)
		err = utils.CopyOverCat(cfSSHArguments, fileName, localFileFullPath)
		if err == nil {
			c.logVerbosef("File download completed successfully")
			fmt.Println(utils.ToSentenceCase(command.FileLabel) + " file saved to: " + localFileFullPath)
		} else {
			c.logVerbosef("File download failed: %v", err)
			return "", err
		}

		if !keepAfterDownload {
			c.logVerbosef("Deleting remote file")
			err = utils.DeleteRemoteFile(cfSSHArguments, fileName)
			if err != nil {
				c.logVerbosef("Failed to delete remote file: %v", err)
				return "", err
			}
			c.logVerbosef("Remote file deleted successfully")
			fmt.Println(utils.ToSentenceCase(command.FileLabel) + " file deleted in application container")
		} else {
			c.logVerbosef("Keeping remote file as requested")
		}
	}
	if command.GenerateArbitraryFiles && !noDownload {
		c.logVerbosef("Processing arbitrary files download: %s", fspath)
		c.logVerbosef("cfSSHArguments: %v", cfSSHArguments)
		// download all files in the generic folder
		files, err := utils.ListFiles(cfSSHArguments, fspath)
		for i, file := range files {
			c.logVerbosef("File %d: %s", i+1, file)
		}
		if err != nil {
			c.logVerbosef("Failed to list files: %v", err)
			return "", err
		}
		c.logVerbosef("Found %d files to download", len(files))
		if len(files) != 0 {
			for _, file := range files {
				c.logVerbosef("Downloading file: %s", file)
				localFileFullPath := localDir + "/" + file
				err = utils.CopyOverCat(cfSSHArguments, fspath+"/"+file, localFileFullPath)
				if err == nil {
					c.logVerbosef("File %s downloaded successfully", file)
					fmt.Printf("File %s saved to: %s\n", file, localFileFullPath)
				} else {
					c.logVerbosef("Failed to download file %s: %v", file, err)
					return "", err
				}
			}

			if !keepAfterDownload {
				c.logVerbosef("Deleting remote file folder")
				err = utils.DeleteRemoteFile(cfSSHArguments, fspath)
				if err != nil {
					c.logVerbosef("Failed to delete remote folder: %v", err)
					return "", err
				}
				c.logVerbosef("Remote folder deleted successfully")
				fmt.Println("File folder deleted in application container")
			} else {
				c.logVerbosef("Keeping remote files as requested")
			}
		} else {
			c.logVerbosef("No files found to download")
		}
	}
	// We keep this around to make the compiler happy, but commandExecutor.Execute will cause an os.Exit
	c.logVerbosef("Command execution completed successfully")
	return output, err
}

// GetMetadata must be implemented as part of the plugin interface
// defined by the core CLI.
//
// GetMetadata() returns a PluginMetadata struct. The first field, Name,
// determines the name of the plugin, which should generally be without spaces.
// If there are spaces in the name, a user will need to properly quote the name
// during uninstall; otherwise, the name will be treated as separate arguments.
// The second value is a slice of Command structs. Our slice only contains one
// Command struct, but could contain any number of them. The first field Name
// defines the command `cf heapdump` once installed into the CLI. The
// second field, HelpText, is used by the core CLI to display help information
// to the user in the core commands `cf help`, `cf`, or `cf -h`.
func (c *JavaPlugin) GetMetadata() plugin.PluginMetadata {
	usageText := "cf java COMMAND APP_NAME [options]"
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
		// Wrap the description with proper indentation
		wrappedDescription := utils.WrapTextWithPrefix(command.Description, "        ", 80, 0)
		usageText += "\n" + wrappedDescription
	}
	return plugin.PluginMetadata{
		Name: "java",
		Version: plugin.VersionType{
			Major: 4,
			Minor: 0,
			Build: 2,
		},
		MinCliVersion: plugin.VersionType{
			Major: 4,
			Minor: 0,
			Build: 0,
		},
		Commands: []plugin.Command{
			{
				Name:     "java",
				HelpText: "Obtain a heap-dump, thread-dump or profile from a running, SSH-enabled Java application.",

				// UsageDetails is optional
				// It is used to show help of usage of each command
				UsageDetails: plugin.Usage{
					Usage:   usageText,
					Options: c.generateOptionsMapFromFlags(),
				},
			},
		},
	}
}

// Unlike most Go programs, the `main()` function will not be used to run all of the
// commands provided in your plugin. main will be used to initialize the plugin
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
