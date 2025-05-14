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
	JavaDetectionCommand              = "if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; $$EXIT 1; fi"
	CheckNoCurrentJFRRecordingCommand = `OUTPUT=$($JCMD_COMMAND $$PIDOF_JAVA_APP JFR.check 2>&1); if [[ ! "$OUTPUT" == *"No available recording"* ]]; then echo "JFR recording already running. Stop it before starting a new recording."; $$EXIT 1; fi;`
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
	_, err := c.DoRun(&commandExecutorImpl{cliConnection: cliConnection}, &uuidGeneratorImpl{}, utils.CfJavaPluginUtilImpl{}, args)
	if err != nil {
		if err.Error() != "unexpected EOF" {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}

// DoRun is an internal method that we use to wrap the cmd package with CommandExecutor for test purposes
func (c *JavaPlugin) DoRun(commandExecutor cmd.CommandExecutor, uuidGenerator uuid.UUIDGenerator, util utils.CfJavaPluginUtil, args []string) (string, error) {
	traceLogger := trace.NewLogger(os.Stdout, true, os.Getenv("CF_TRACE"), "")
	ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), traceLogger)

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
	// Used for creating the shell alias, which allows users to have similar commands in Docker
	// if empty uses SshCommand
	AliasCommand string
	// if true, omit from alias generation
	OmitAlias bool
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
		SshCommand: `if [ -f $$FILE_NAME ]; then echo >&2 'Heap dump $$FILE_NAME already exists'; $$EXIT 1; fi
JMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])
# SAP JVM: Wrap everything in an if statement in case jvmmon is available
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])
if [ -n "${JMAP_COMMAND}" ]; then
OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=$$FILE_NAME $$PIDOF_JAVA_APP ) || STATUS_CODE=$?
if [ ! -s $$FILE_NAME ]; then echo >&2 ${OUTPUT}; $$EXIT 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
elif [ -n "${JVMMON_COMMAND}" ]; then
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=$$FSPATH\ndump heap' > setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid $$PIDOF_JAVA_APP -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
sleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file
HEAP_DUMP_NAME=$(find $$FSPATH -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done
if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; $$EXIT 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
fi`,
		FileLabel:    "heap dump",
		FileNamePart: "heapdump",
		OmitAlias:    true,
	},
	{
		Name:          "thread-dump",
		Description:   "Generate a thread dump from a running Java application",
		RequiredTools: []string{"jstack", "jvmmon"},
		GenerateFiles: false,
		SshCommand:    "${JSTACK_COMMAND} $$PIDOF_JAVA_APP; ${JVMMON_COMMAND} -pid $$PIDOF_JAVA_APP -c \"print stacktrace\"",
		OmitAlias: 	true,
	},
	{
		Name:          "vm-info",
		Description:   "Print information about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.info | filter_jcmd_remote_message`,
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP VM.info`,
	},
	{
		Name:          "jcmd",
		Description:   "Run a JCMD command on a running Java application via --args, downloads and deletes all files that are created in the current folder, use '--no-download' to prevent this",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    `$JCMD_COMMAND $(pidof java) $$ARGS`,
		OmitAlias:     true,
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
		AliasCommand: CheckNoCurrentJFRRecordingCommand + `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.start settings=default.jfc filename=$$FILE_NAME name=JFR`,
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
		AliasCommand: CheckNoCurrentJFRRecordingCommand + `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.start settings=profile.jfc filename=$$FILE_NAME name=JFR`,
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
		AliasCommand: CheckNoCurrentJFRRecordingCommand + `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.start settings=gc.jfc filename=$$FILE_NAME name=JFR`,
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
		AliasCommand: CheckNoCurrentJFRRecordingCommand + `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.start settings=gc_details.jfc filename=$$FILE_NAME name=JFR`,
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
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.stop name=JFR`,
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
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.dump`,
	},
	{
		Name:          "jfr-status",
		Description:   "Check the running Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.check | filter_jcmd_remote_message`,
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP JFR.check`,
	},
	{
		Name:          "vm-version",
		Description:   "Print the version of the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.version | filter_jcmd_remote_message`,
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP VM.version`,
	},
	{
		Name:          "vm-vitals",
		Description:   "Print vital statistics about the Java Virtual Machine running a Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		SshCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.vitals | filter_jcmd_remote_message`,
		AliasCommand:  `$JCMD_COMMAND $$PIDOF_JAVA_APP VM.vitals`,
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
		OmitAlias:                        true,
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
		SshCommand:             `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e cpu -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
		AliasCommand:           `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e cpu -f $$FILE_NAME; echo "Use 'asprof-stop $$PIDOF_JAVA_APP' to copy the file to the local folder"`,
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
		SshCommand:             `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e wall -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
		AliasCommand:           `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e wall -f $$FILE_NAME; echo "Use 'asprof-stop $$PIDOF_JAVA_APP' to copy the file to the local folder"`,
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
		SshCommand:             `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e alloc -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
		AliasCommand:           `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e alloc -f $$FILE_NAME; echo "Use 'asprof-stop $$PIDOF_JAVA_APP' to copy the file to the local folder"`,
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
		SshCommand:             `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e lock -f $$FILE_NAME; echo "Use 'cf java asprof-stop $$APP_NAME' to copy the file to the local folder"`,
		AliasCommand:           `$ASPROF_COMMAND start $$PIDOF_JAVA_APP -e lock -f $$FILE_NAME; echo "Use 'asprof-stop $$PIDOF_JAVA_APP' to copy the file to the local folder"`,
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
		SshCommand:             `$ASPROF_COMMAND stop $$PIDOF_JAVA_APP`,
	},
	{
		Name:                   "asprof-status",
		Description:            "Get the status of async-profiler on a running Java application",
		RequiredTools:          []string{"asprof"},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          false,
		SshCommand:             `$ASPROF_COMMAND status $$PIDOF_JAVA_APP`,
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

func generateRequiredToolCommand(requiredTool string) []string {
	uppercase := strings.ToUpper(requiredTool)
	var ret = []string{fmt.Sprintf(`%s_COMMAND=$(which %s 2>/dev/null || command -v %s 2>/dev/null)
if [ -z "${%s_COMMAND}" ]; then
    if [ -n "$JAVA_HOME" ]; then
        %s_COMMAND=$(find "$JAVA_HOME/bin" -name %s -type f -executable 2>/dev/null | head -1)
    fi
fi`, uppercase, requiredTool, requiredTool, uppercase, uppercase, requiredTool)}

	if requiredTool == "jcmd" {
		ret = append(ret, fmt.Sprintf(`ASPROF_COMMAND=$(which asprof 2>/dev/null || command -v asprof 2>/dev/null)
if [ -n "${ASPROF_COMMAND}" ]; then 
    JCMD_COMMAND="${ASPROF_COMMAND} jcmd"
fi`))
	}
	return ret
}

func indentLines(input string, indent string) string {
	lines := strings.Split(input, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func generateAliasScript() {
	// idea:
	// create a script that replaces all variables

	fmt.Println(`#!/usr/bin/env sh

_jcmd() {
  ASPROF_COMMAND=$(which asprof 2>/dev/null || command -v asprof 2>/dev/null)
  if [ -n "${ASPROF_COMMAND}" ]; then 
      "${ASPROF_COMMAND}" jcmd $@
      return $!
  fi
  JCMD_COMMAND=$(which jcmd 2>/dev/null || command -v jcmd 2>/dev/null)
  if [ -z "${JCMD_COMMAND}" ]; then
      if [ -n "$JAVA_HOME" ]; then
          JCMD_COMMAND=$(find "$JAVA_HOME/bin" -name jcmd -type f -executable 2>/dev/null | head -1)
      fi
  fi
  "${JCMD_COMMAND}" jcmd $@
  return $!
}

java_pid() {
  # Function to find non-jcmd Java processes and return their PIDs
  
  # Define a function to list Java processes excluding jcmd-related ones

  list_java_processes() {
    # Use ps with comm= to find processes where the binary is named exactly "java"
    # This excludes processes where "java" is just part of the command arguments
    ps -e -o pid,comm= | grep "^[[:space:]]*[0-9][0-9]*[[:space:]]\+java$" | grep -v "jcmd\|JCmd"
  }
  
  # Get the list of valid Java processes
  local java_processes
  java_processes=$(list_java_processes)
  
  # Count the number of valid Java processes
  local process_count
  process_count=$(echo "$java_processes" | grep -c .)
  
  # Case 1: No argument provided - use the only Java process if only one exists
  if [ $# -eq 0 ]; then
    if [ "$process_count" -eq 0 ]; then
      echo "Error: No Java processes found." >&2
      return 1
    elif [ "$process_count" -eq 1 ]; then
      # Extract just the PID from the first line
      echo "$java_processes" | awk '{print $1}' | head -1
      return 0
    else
      echo "Error: Multiple Java processes found. Please specify a PID." >&2
      echo "Running Java processes:" >&2
      echo "$java_processes" >&2
      return 1
    fi
  fi
  
  # Case 2: PID argument provided - validate it's a Java (non-jcmd) process
  local pid="$1"
  
  # Check if argument is a number
  if ! [[ "$pid" =~ ^[0-9]+$ ]]; then
    echo "Error: '$pid' is not a valid process ID." >&2
    if [ "$process_count" -gt 0 ]; then
      echo "Running Java processes:" >&2
      echo "$java_processes" >&2
    fi
    return 1
  fi
  
  # Check if process exists
  if ! ps -p "$pid" > /dev/null; then
    echo "Error: Process $pid does not exist." >&2
    if [ "$process_count" -gt 0 ]; then
      echo "Running Java processes:" >&2
      echo "$java_processes" >&2
    fi
    return 1
  fi
  
  # Get command for the specified PID
  local cmd
  cmd=$(ps -p "$pid" -o command= 2>/dev/null)
  
  # Check if it's a Java process and not a jcmd process
  if echo "$cmd" | grep -q "java"; then
    if echo "$cmd" | grep -q "jcmd\|JCmd\|sun.tools.jcmd"; then
      echo "Error: Process $pid is a jcmd-related process, not a Java application." >&2
      if [ "$process_count" -gt 0 ]; then
        echo "Running Java processes:" >&2
        echo "$java_processes" >&2
      fi
      return 1
    else
      # It's a valid Java process
      echo "$pid"
      return 0
    fi
  else
    echo "Error: Process $pid is not a Java process." >&2
    if [ "$process_count" -gt 0 ]; then
      echo "Running Java processes:" >&2
      echo "$java_processes" >&2
    fi
    return 1
  fi
}

export -f java_pid
`)
	for _, command := range commands {
		if command.OmitAlias {
			continue
		}
		if command.AliasCommand == "" {
			command.AliasCommand = command.SshCommand
		}
		var prefixCommand = ""
		var aliasCommand = command.AliasCommand
		var replacements = map[string]string{
			"$$PIDOF_JAVA_APP": "$(java_pid)",
			"$$FILE_NAME":      command.FileNamePart + `-$(date +"%Y%m%d-%H%M%S")` + command.FileExtension,
			"$$FSPATH": 	"$(pwd)",
			"$JCMD_COMMAND": "jcmd",
			"$$EXIT": "return",
		}
		prohibited := []string{"$$APP_NAME", "$$ARGS"}
		for key, value := range replacements {
			aliasCommand = strings.ReplaceAll(aliasCommand, key, value)
		}
		for _, p := range prohibited {
			if strings.Contains(aliasCommand, p) {
				fmt.Println("Prohibited variable in alias command: " + p)
			}
		}
		for _, tool := range command.RequiredTools {
			if tool == "jcmd" {
				continue
			}
			prefixCommand = strings.Join(generateRequiredToolCommand(tool), "\n") + "\n" + prefixCommand
		}

		fmt.Printf(`
%s() {
  local pid
  pid=$(java_pid) || return 1
%s
%s
}
export -f %s
`, command.Name, indentLines(prefixCommand, "  "), aliasCommand, command.Name)
	}
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
		return "", errors.New("The environment variable CF_TRACE is set to true. This prevents download of the dump from succeeding")
	}

	commandFlags := flags.New()

	commandFlags.NewIntFlagWithDefault("app-instance-index", "i", "application `instance` to connect to", -1)
	commandFlags.NewBoolFlag("keep", "k", "whether to `keep` the heap-dump/JFR/... files on the container of the application instance after having downloaded it locally")
	commandFlags.NewBoolFlag("no-download", "nd", "do not download the heap-dump/JFR/... file to the local machine")
	commandFlags.NewBoolFlag("dry-run", "n", "triggers the `dry-run` mode to show only the cf-ssh command that would have been executed")
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

	applicationInstance := commandFlags.Int("app-instance-index")
	noDownload := commandFlags.IsSet("no-download")
	keepAfterDownload := commandFlags.IsSet("keep") || noDownload

	remoteDir := commandFlags.String("container-dir")
	localDir := commandFlags.String("local-dir")
	if localDir == "" {
		localDir = "."
	}

	arguments := commandFlags.Args()
	argumentLen := len(arguments)

	if argumentLen < 1 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	commandName := arguments[0]

	if commandName == "generate-alias-script" {
		if argumentLen > 1 {
			return "", &InvalidUsageError{message: "generate-alias-script has no options or arguments"}
		}
		generateAliasScript()
		return "", nil
	}

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
	if !command.GenerateFiles && !command.GenerateArbitraryFiles {
		for _, flag := range fileFlags {
			if commandFlags.IsSet(flag) {
				return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", flag, command.Name)}
			}
		}
	}
	if command.Name == "asprof" {
		trimmedMiscArgs := strings.TrimLeft(miscArgs, " ")
		if len(trimmedMiscArgs) > 6 && trimmedMiscArgs[:6] == "start " {
			noDownload = true
		} else {
			noDownload = trimmedMiscArgs == "start"
		}
	}
	if !command.HasMiscArgs() && commandFlags.IsSet("args") {
		return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "args", command.Name)}
	}
	if argumentLen == 1 {
		return "", &InvalidUsageError{message: "No application name provided"}
	} else if argumentLen > 2 {
		return "", &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	applicationName := arguments[1]

	cfSSHArguments := []string{"ssh", applicationName}
	if applicationInstance > 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(applicationInstance))
	}

	supported, err := util.CheckRequiredTools(applicationName)

	if err != nil || !supported {
		return "required tools checking failed", err
	}

	var remoteCommandTokens = []string{JavaDetectionCommand}

	for _, requiredTool := range command.RequiredTools {
		remoteCommandTokens = append(remoteCommandTokens, generateRequiredToolCommand(requiredTool)...)
	}
	fileName := ""
	fspath := remoteDir

	var replacements = map[string]string{
		"$$ARGS":           miscArgs,
		"$$APP_NAME":       applicationName,
		"$$PIDOF_JAVA_APP": "$(pidof java)",
		"$$EXIT":           "exit",
	}

	if command.GenerateFiles || command.NeedsFileName || command.GenerateArbitraryFiles {
		fspath, err = util.GetAvailablePath(applicationName, remoteDir)
		if err != nil {
			return "", err
		}
		if command.GenerateArbitraryFiles {
			fspath = fspath + "/" + command.GenerateArbitraryFilesFolderName
		}

		fileName = fspath + "/" + applicationName + "-" + command.FileNamePart + "-" + uuidGenerator.Generate() + command.FileExtension
		replacements["$$FILE_NAME"] = fileName
		replacements["$$FSPATH"] = fspath
		if command.GenerateArbitraryFiles {
			// prepend 'mkdir -p $$FSPATH' to the command to create the directory if it does not exist
			remoteCommandTokens = append([]string{"mkdir -p " + fspath}, remoteCommandTokens...)
			remoteCommandTokens = append(remoteCommandTokens, "cd "+fspath)
		}
	}

	var commandText = command.SshCommand
	for key, value := range replacements {
		commandText = strings.ReplaceAll(commandText, key, value)
	}
	remoteCommandTokens = append(remoteCommandTokens, commandText)

	cfSSHArguments = append(cfSSHArguments, "--command")
	remoteCommand := strings.Join(remoteCommandTokens, "; ")

	if commandFlags.IsSet("dry-run") {
		// When printing out the entire command line for separate execution, we wrap the remote command in single quotes
		// to prevent the shell processing it from running it in local
		cfSSHArguments = append(cfSSHArguments, "'"+remoteCommand+"'")
		return "cf " + strings.Join(cfSSHArguments, " "), nil
	}

	fullCommand := append(cfSSHArguments, remoteCommand)

	output, err := commandExecutor.Execute(fullCommand)

	if command.GenerateFiles && !noDownload {

		finalFile := ""
		var err error
		switch command.FileExtension {
		case ".hprof":
			finalFile, err = util.FindHeapDumpFile(cfSSHArguments, fileName, fspath)
		case ".jfr":
			finalFile, err = util.FindJFRFile(cfSSHArguments, fileName, fspath)
		default:
			return "", &InvalidUsageError{message: fmt.Sprintf("Unsupported file extension %q", command.FileExtension)}
		}
		if err == nil && finalFile != "" {
			fileName = finalFile
			fmt.Println("Successfully created " + command.FileLabel + " in application container at: " + fileName)
		} else {
			fmt.Println("Failed to find " + command.FileLabel + " in application container")
			return "", err
		}

		localFileFullPath := localDir + "/" + applicationName + "-" + command.FileNamePart + "-" + uuidGenerator.Generate() + command.FileExtension
		err = util.CopyOverCat(cfSSHArguments, fileName, localFileFullPath)
		if err == nil {
			fmt.Println(toSentenceCase(command.FileLabel) + " file saved to: " + localFileFullPath)
		} else {
			return "", err
		}

		if !keepAfterDownload {
			err = util.DeleteRemoteFile(cfSSHArguments, fileName)
			if err != nil {
				return "", err
			}
			fmt.Println(toSentenceCase(command.FileLabel) + " file deleted in application container")
		}
	}
	if command.GenerateArbitraryFiles && !noDownload {
		// download all files in the generic folder
		files, err := util.ListFiles(cfSSHArguments, fspath)
		if err != nil {
			return "", err
		}
		if len(files) != 0 {
			for _, file := range files {
				localFileFullPath := localDir + "/" + file
				err = util.CopyOverCat(cfSSHArguments, fspath+"/"+file, localFileFullPath)
				if err == nil {
					fmt.Printf("File %s saved to: %s\n", file, localFileFullPath)
				} else {
					return "", err
				}
			}

			if !keepAfterDownload {
				err = util.DeleteRemoteFile(cfSSHArguments, fspath)
				if err != nil {
					return "", err
				}
				fmt.Println("File folder deleted in application container")
			}
		}
	}
	// We keep this around to make the compiler happy, but commandExecutor.Execute will cause an os.Exit
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
	usageText += "\n\n  Use 'cf java generate-alias-script' for the creation of shell script with similar commands.\n"
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
