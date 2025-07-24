/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

// Package main implements a Cloud Foundry CLI plugin for Java application profiling and debugging.
// It provides commands for heap dumps, JFR recordings, and async-profiler integration.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cf-cli-java-plugin/utils"

	"code.cloudfoundry.org/cli/cf/terminal"
	"code.cloudfoundry.org/cli/cf/trace"
	"code.cloudfoundry.org/cli/plugin"
)

// Assert that JavaPlugin implements plugin.Plugin interface.
var _ plugin.Plugin = (*JavaPlugin)(nil)

// Global verbose flag for logging.
var verbose bool

// LogVerbosef prints verbose messages if verbose mode is enabled.
func LogVerbosef(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[VERBOSE] "+format+"\n", args...)
	}
}

// CommandLineOptions holds all parsed command line options.
type CommandLineOptions struct {
	AppInstanceIndex int
	Keep             bool
	NoDownload       bool
	DryRun           bool
	Verbose          bool
	ContainerDir     string
	LocalDir         string
	Args             string
	CommandName      string
	ApplicationName  string
}

// InvalidUsageError indicates that the arguments passed to the command are invalid.
type InvalidUsageError struct {
	message string
}

func (e InvalidUsageError) Error() string {
	return e.message
}

// JavaPlugin is a cf cli plugin that supports taking heap and thread dumps on demand.
type JavaPlugin struct {
	verbose       bool
	options       *CommandLineOptions
	cliConnection plugin.CliConnection
}

// NewJavaPlugin creates a new instance of JavaPlugin.
func NewJavaPlugin() *JavaPlugin {
	return &JavaPlugin{}
}

// IsVerbose returns whether verbose logging is enabled.
func (c *JavaPlugin) IsVerbose() bool {
	return c.verbose || (c.options != nil && c.options.Verbose)
}

// Command represents a plugin command with its configuration.
// Each command defines how to interact with Java applications running in Cloud Foundry.
type Command struct {
	// Basic command information
	Name        string // The command name as used by users
	Description string // Human-readable description of what the command does

	// Platform compatibility
	OnlyOnRecentSapMachine bool // Whether this command requires recent SapMachine JVM

	// Tool requirements
	RequiredTools []string // Tools required, checked and $TOOL_COMMAND set in the remote command

	// File generation settings
	GenerateFiles bool   // Whether this command creates a single output file
	NeedsFileName bool   // Whether the command needs a filename parameter
	FileExtension string // Extension for generated files (e.g., ".hprof", ".jfr")
	FileLabel     string // Human-readable label for the file type
	FileNamePart  string // Part of the filename that identifies the command
	FilePattern   string // Pattern for finding generated files

	// Arbitrary file generation (for commands that create multiple files)
	GenerateArbitraryFiles           bool   // Whether the command creates multiple files
	GenerateArbitraryFilesFolderName string // Subfolder name for arbitrary files

	// Command execution
	SSHCommand string // The shell command to execute remotely (use @ prefix for variable replacement)
}

// HasMiscArgs checks whether the SSHCommand contains @ARGS.
func (c *Command) HasMiscArgs() bool {
	return strings.Contains(c.SSHCommand, "@ARGS")
}

const (
	// Plugin information
	PluginName = "java"

	// File extensions
	HeapDumpExtension = ".hprof"
	JFRExtension      = ".jfr"

	// Command execution constants
	JavaDetectionCommand = "if ! pgrep -x \"java\" > /dev/null; then " +
		"echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; " +
		"exit 1; fi"

	CheckNoCurrentJFRRecordingCommand = `OUTPUT=$($JCMD_COMMAND $(pidof java) JFR.check 2>&1); ` +
		`if [[ ! "$OUTPUT" == *"No available recording"* ]]; then ` +
		`echo "JFR recording already running. Stop it before starting a new recording."; ` +
		`exit 1; fi;`

	FilterJCMDRemoteMessage = `filter_jcmd_remote_message() {
  if command -v grep >/dev/null 2>&1; then
    grep -v -e "Connected to remote JVM" -e "JVM response code = 0"
  else
    cat  # fallback: just pass through the input unchanged
  fi
};`

	// Error messages
	UnexpectedEOFError      = "Unexpected EOF"
	CommandExecutionError   = "Command execution terminated unexpectedly"
	NoOutputError           = "Command execution failed with no output"
	FileGenerationError     = "File generation failed"
	RequiredToolsCheckError = "Required tools checking failed"
)

// Version information
var (
	PluginVersion = utils.Version{Major: 4, Minor: 0, Build: 0}
	MinCLIVersion = utils.Version{Major: 6, Minor: 7, Build: 0}
)

// flagDefinitions contains all the flag definitions used by both parsing and metadata
var flagDefinitions = []utils.FlagDefinition{
	{
		Name:        "app-instance-index",
		ShortName:   "i",
		Description: "[index], select to which instance of the app to connect",
		Type:        "int",
		Default:     0,
		TakesValue:  true,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).AppInstanceIndex = value.(int)
		},
	},
	{
		Name:      "keep",
		ShortName: "k",
		Description: "keep the heap dump in the container; by default the heap dump/JFR/... " +
			"will be deleted from the container's filesystem after been downloaded",
		Type:       "bool",
		Default:    false,
		TakesValue: false,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).Keep = value.(bool)
		},
	},
	{
		Name:        "no-download",
		ShortName:   "nd",
		Description: "don't download the heap dump/JFR/... file to local, only keep it in the container, implies '--keep'",
		Type:        "bool",
		Default:     false,
		TakesValue:  false,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).NoDownload = value.(bool)
		},
	},
	{
		Name:        "dry-run",
		ShortName:   "n",
		Description: "just output to command line what would be executed",
		Type:        "bool",
		Default:     false,
		TakesValue:  false,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).DryRun = value.(bool)
		},
	},
	{
		Name:        "verbose",
		ShortName:   "v",
		Description: "enable verbose output for the plugin",
		Type:        "bool",
		Default:     false,
		TakesValue:  false,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).Verbose = value.(bool)
		},
	},
	{
		Name:        "container-dir",
		ShortName:   "cd",
		Description: "the directory path in the container that the heap dump/JFR/... file will be saved to",
		Type:        "string",
		Default:     "",
		TakesValue:  true,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).ContainerDir = value.(string)
		},
	},
	{
		Name:      "local-dir",
		ShortName: "ld",
		Description: "the local directory path that the dump/JFR/... file will be saved to, " +
			"defaults to the current directory",
		Type:       "string",
		Default:    ".",
		TakesValue: true,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).LocalDir = value.(string)
		},
	},
	{
		Name:      "args",
		ShortName: "a",
		Description: "Miscellaneous arguments to pass to the command (if supported) in the container, " +
			"be aware to end it with a space if it is a simple option",
		Type:       "string",
		Default:    "",
		TakesValue: true,
		Setter: func(target, value interface{}) {
			target.(*CommandLineOptions).Args = value.(string)
		},
	},
}

// commands contains all available plugin commands
var commands = []Command{
	{
		Name:          "heap-dump",
		Description:   "Generate a heap dump from a running Java application",
		GenerateFiles: true,
		FileExtension: HeapDumpExtension,
		/*
					If there is not enough space on the filesystem to write the dump, jmap will create a file
			with size 0, output something about not enough space left on the device, and exit with status code 0.
			Because YOLO.

			Also: if the heap dump file already exists, jmap will output something about the file already
			existing and exit with status code 0. At least it is consistent.

			OpenJDK: Wrap everything in an if statement in case jmap is available
		*/
		SSHCommand: `JMAP_COMMAND=$(find -executable -name jmap | head -1);
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1);
if [ -z "${JMAP_COMMAND}" ] && [ -z "${JVMMON_COMMAND}" ]; then
  echo >&2 "jvmmon or jmap are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
		---
		applications:
		- name: <APP_NAME>
		  memory: 1G
		  path: <PATH_TO_BUILD_ARTIFACT>
		  buildpack: https://github.com/cloudfoundry/java-buildpack
		  env:
		    JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: \"https://java-buildpack.cloudfoundry.org/openjdk-jdk/bionic/x86_64\", version: 17.+ } }'
		
	"
  exit 1
fi
if [ -n "${JMAP_COMMAND}" ]; then
  ${JMAP_COMMAND} -dump:live,format=b,file=@FILE_NAME $(pidof java);
  HEAP_DUMP_NAME=$(find @FSPATH -name 'java_pid*.hprof' -printf '%T@ %p\0' | 
    sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
  SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); 
  while [ ${SIZE} != ${OLD_SIZE} ]; do 
    OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); 
  done
  if [ ! -s "${HEAP_DUMP_NAME}" ]; then 
    echo "Heap dump file is empty or does not exist"; exit 1; 
  fi
  mv "${HEAP_DUMP_NAME}" @FILE_NAME;
  exit 0;
fi
if [ -n "${JVMMON_COMMAND}" ]; then
  ${JVMMON_COMMAND} -pid $(pidof java) -c "create_dump -f @FILE_NAME";
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
		if [ -z "${JMAP_COMMAND}" ] && [ -z "${JVMMON_COMMAND}" ]; then
		echo >&2 "jvmmon or jmap are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
				---
				applications:
				- name: <APP_NAME>
				  memory: 1G
				  path: <PATH_TO_BUILD_ARTIFACT>
				  buildpack: https://github.com/cloudfoundry/java-buildpack
		env:
			JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: \"https://java-buildpack.cloudfoundry.org/openjdk-jdk/bionic/x86_64\", version: 17.+ } }'			"
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
		Description:                      "Run a JCMD command on a running Java application via --args, downloads and deletes all files that are created in the current folder, use '--no-download' to prevent this",
		RequiredTools:                    []string{"jcmd"},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: "jcmd",
		SSHCommand:                       `$JCMD_COMMAND $(pidof java) @ARGS`,
	},
	{
		Name:          "jfr-start",
		Description:   "Start a Java Flight Recorder default recording on a running Java application (stores in the the container-dir)",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: JFRExtension,
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=default.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-start-profile",
		Description:   "Start a Java Flight Recorder profile recording on a running Java application (stores in the the container-dir))",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: JFRExtension,
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=profile.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
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
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc_details.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-stop",
		Description:   "Stop a Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: JFRExtension,
		FileLabel:     "JFR recording",
		FileNamePart:  "jfr",
		SSHCommand: FilterJCMDRemoteMessage + ` output=$($JCMD_COMMAND $(pidof java) JFR.stop name=JFR | filter_jcmd_remote_message);
		echo "$output"; echo ""; filename=$(echo "$output" | grep /.*.jfr --only-matching);
		if [ -z "$filename" ]; then echo "No JFR recording created"; exit 1; fi;
		if [ ! -f "$filename" ]; then echo "JFR recording $filename does not exist"; exit 1; fi;
		if [ ! -s "$filename" ]; then echo "JFR recording $filename is empty"; exit 1; fi;
		mvn "$filename" @FILE_NAME;
		echo "JFR recording copied to @FILE_NAME"`,
	},
	{
		Name:          "jfr-dump",
		Description:   "Dump a Java Flight Recorder recording on a running Java application without stopping it",
		RequiredTools: []string{"jcmd"},
		GenerateFiles: true,
		FileExtension: JFRExtension,
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
		Description:                      "Run async-profiler commands passed to asprof via --args, copies files in the current folder. Don't use in combination with asprof-* commands. Downloads and deletes all files that are created in the current folder, if not using 'start' asprof command, use '--no-download' to prevent this.",
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

// Run must be implemented by any plugin because it is part of the
// plugin interface defined by the core CLI.
//
// Run(....) is the entry point when the core CLI is invoking a command defined
// by a plugin. The first parameter, plugin.CliConnection, is a struct that can
// be used to invoke cli commands. The second parameter, args, is a slice of
// strings. args[0] will be the Name of the command, and will be followed by
// any additional arguments a cli user typed in.
//
// Any error handling should be handled with the plugin itself (this means printing
// user facing errors). The CLI will exit 0 if the plugin exits 0 and will exit
// 1 should the plugin exit nonzero.
func (c *JavaPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	// Check if verbose flag is in args for early logging
	for _, arg := range args {
		if arg == "-v" || arg == "--verbose" {
			c.verbose = true
			verbose = true // Set global verbose flag
			break
		}
	}

	LogVerbosef("Run called with args: %v", args)

	_, err := c.DoRun(cliConnection, args)
	if err != nil {
		LogVerbosef("Error occurred: %v", err)
		os.Exit(1)
	}
	LogVerbosef("Run completed successfully")
}

// DoRun is an internal method that we use to wrap the cmd package with CliConnection for test purposes
func (c *JavaPlugin) DoRun(cliConnection plugin.CliConnection, args []string) (string, error) {
	traceLogger := trace.NewLogger(os.Stdout, true, os.Getenv("CF_TRACE"), "")
	ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), traceLogger)

	LogVerbosef("DoRun called with args: %v", args)

	// Store the connection in the plugin
	c.cliConnection = cliConnection

	output, err := c.execute(args)
	if err != nil {
		return c.handleExecutionError(err, cliConnection, ui, output)
	}

	if output != "" {
		ui.Say(output)
	}

	return output, err
}

// handleExecutionError processes errors from command execution
func (c *JavaPlugin) handleExecutionError(err error, cliConnection plugin.CliConnection, ui terminal.UI, output string) (string, error) {
	if err.Error() == "unexpected EOF" {
		return output, err
	}

	ui.Failed(err.Error())

	var invalidUsageErr *InvalidUsageError
	if errors.As(err, &invalidUsageErr) {
		fmt.Println()
		fmt.Println()
		if _, helpErr := cliConnection.CliCommand("help", "java"); helpErr != nil {
			ui.Failed("Failed to show help")
		}
	}

	return output, err
}

// parseCommandLineOptions parses command line arguments using the generic parser from utils
func parseCommandLineOptions(args []string) (*CommandLineOptions, error) {
	// Factory function to create new CommandLineOptions instance
	targetFactory := func() interface{} {
		return &CommandLineOptions{}
	}

	// Validation function for parsed options
	validateFunc := func(target interface{}) error {
		opts := target.(*CommandLineOptions)

		// Set global verbose flag
		verbose = opts.Verbose

		// Extract positional arguments
		var positionalArgs []string
		for i := 1; i < len(args); i++ {
			arg := args[i]
			if !strings.HasPrefix(arg, "-") {
				positionalArgs = append(positionalArgs, arg)
			}
		}

		// Get remaining arguments (command and app name) from positional args
		if len(positionalArgs) < 1 {
			return &utils.InvalidUsageError{Message: "No command provided"}
		}
		if len(positionalArgs) < 2 {
			return &utils.InvalidUsageError{Message: "No application name provided"}
		}
		if len(positionalArgs) > 2 {
			return &utils.InvalidUsageError{Message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(positionalArgs[2:], ", "))}
		}

		opts.CommandName = positionalArgs[0]
		opts.ApplicationName = positionalArgs[1]

		// Validate application instance index
		if opts.AppInstanceIndex < 0 {
			return &utils.InvalidUsageError{Message: fmt.Sprintf("Invalid application instance index %d, must be >= 0", opts.AppInstanceIndex)}
		}

		// Normalize local directory
		if opts.LocalDir == "" {
			opts.LocalDir = "."
		}

		// Strip trailing slashes from container directory
		opts.ContainerDir = strings.TrimRight(opts.ContainerDir, "/")

		return nil
	}

	// Use the generic parser
	result, err := utils.ParseCommandLineOptions(args, targetFactory, flagDefinitions, PluginName, validateFunc)
	if err != nil {
		// Convert utils.InvalidUsageError to local InvalidUsageError
		var utilsErr *utils.InvalidUsageError
		if errors.As(err, &utilsErr) {
			return nil, &InvalidUsageError{message: utilsErr.Message}
		}
		return nil, err
	}

	return result.(*CommandLineOptions), nil
}

func (c *JavaPlugin) execute(args []string) (string, error) {
	// Parse command line options and store them in the plugin
	opts, err := parseCommandLineOptions(args)
	if err != nil {
		return "", err
	}
	c.options = opts

	// Handle special case for uninstall
	if len(args) > 0 && args[0] == "CLI-MESSAGE-UNINSTALL" {
		return "", nil
	}

	LogVerbosef("Starting command execution")
	LogVerbosef("Command arguments: %v", args)
	LogVerbosef("Parsed options: %+v", c.options)

	// Find the command
	command, err := c.findCommand()
	if err != nil {
		return "", err
	}

	LogVerbosef("Found command: %s - %s", command.Name, command.Description)

	// Validate flags for the command
	err = c.validateCommandFlags(command)
	if err != nil {
		return "", err
	}

	// Handle special cases for specific commands
	c.applyCommandSpecificOptions(command)

	// Build and execute the SSH command
	return c.executeSSHCommand(command)
}

// ==================== COMMAND RESOLUTION AND VALIDATION ====================

// findCommand locates a command by name and returns it or an error
func (c *JavaPlugin) findCommand() (*Command, error) {
	for i, command := range commands {
		if command.Name == c.options.CommandName {
			return &commands[i], nil
		}
	}

	// Command not found, provide suggestions
	avCommands := make([]string, 0, len(commands))
	for _, command := range commands {
		avCommands = append(avCommands, command.Name)
	}
	matches := utils.FuzzySearch(c.options.CommandName, avCommands, 3)
	return nil, &InvalidUsageError{message: fmt.Sprintf("Unrecognized command %q, did you mean: %s?", c.options.CommandName, utils.JoinWithOr(matches))}
}

// validateCommandFlags checks if the provided flags are valid for the given command
func (c *JavaPlugin) validateCommandFlags(command *Command) error {
	if !command.GenerateFiles && !command.GenerateArbitraryFiles {
		LogVerbosef("Command does not generate files, checking for invalid file flags")

		// Check each file flag to see if it was set inappropriately
		if c.options.ContainerDir != "" {
			return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "container-dir", command.Name)}
		}
		if c.options.LocalDir != "." {
			return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "local-dir", command.Name)}
		}
		if c.options.Keep {
			return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "keep", command.Name)}
		}
		if c.options.NoDownload {
			return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "no-download", command.Name)}
		}
	}

	if !command.HasMiscArgs() && c.options.Args != "" {
		LogVerbosef("Command %s does not support --args flag", command.Name)
		return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "args", command.Name)}
	}

	return nil
}

// applyCommandSpecificOptions handles special logic for specific commands
func (c *JavaPlugin) applyCommandSpecificOptions(command *Command) {
	if command.Name == "asprof" {
		trimmedMiscArgs := strings.TrimLeft(c.options.Args, " ")
		if len(trimmedMiscArgs) > 6 && trimmedMiscArgs[:6] == "start " {
			c.options.NoDownload = true
			LogVerbosef("asprof start command detected, setting noDownload to true")
		} else {
			c.options.NoDownload = trimmedMiscArgs == "start"
			if c.options.NoDownload {
				LogVerbosef("asprof start command detected, setting noDownload to true")
			}
		}
	}
}

func (c *JavaPlugin) buildSSHArguments() []string {
	cfSSHArguments := []string{"ssh", c.options.ApplicationName}
	if c.options.AppInstanceIndex > 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(c.options.AppInstanceIndex))
	}
	return cfSSHArguments
}

// executeSSHCommand builds and executes the SSH command with all the logic
func (c *JavaPlugin) executeSSHCommand(command *Command) (string, error) {
	// Log execution context
	LogVerbosef("Application name: %s", c.options.ApplicationName)
	LogVerbosef("Application instance: %d", c.options.AppInstanceIndex)
	LogVerbosef("No download: %t", c.options.NoDownload)
	LogVerbosef("Keep after download: %t", c.options.Keep || c.options.NoDownload)
	LogVerbosef("Remote directory: %s", c.options.ContainerDir)
	LogVerbosef("Local directory: %s", c.options.LocalDir)

	if err := c.validatePrerequisites(); err != nil {
		return "", err
	}

	cfSSHArguments := c.buildSSHArguments()
	remoteCommand, fileName, err := c.buildRemoteCommand(command)
	if err != nil {
		return "", err
	}

	if c.options.DryRun {
		return c.handleDryRun(cfSSHArguments, remoteCommand), nil
	}

	return c.executeRemoteCommand(command, cfSSHArguments, remoteCommand, fileName)
}

// validatePrerequisites checks if required tools are available
func (c *JavaPlugin) validatePrerequisites() error {
	if c.options == nil {
		return fmt.Errorf("command line options not initialized")
	}

	if c.options.ApplicationName == "" {
		return fmt.Errorf("application name is required")
	}

	if c.options.AppInstanceIndex < 0 {
		return fmt.Errorf("application instance index must be non-negative, got %d",
			c.options.AppInstanceIndex)
	}

	supported, err := utils.CheckRequiredTools(c.options.ApplicationName)
	if err != nil {
		return fmt.Errorf("%s: %w", RequiredToolsCheckError, err)
	}
	if !supported {
		return fmt.Errorf("%s: required tools not available", RequiredToolsCheckError)
	}

	LogVerbosef("Required tools check passed")
	return nil
}

// handleDryRun formats the dry-run command output
func (c *JavaPlugin) handleDryRun(cfSSHArguments []string, remoteCommand string) string {
	LogVerbosef("Dry-run mode enabled, returning command without execution")
	cfSSHArguments = append(cfSSHArguments, "--command", "'"+remoteCommand+"'")
	return "cf " + strings.Join(cfSSHArguments, " ")
}

// buildRemoteCommand constructs the complete remote command to execute
func (c *JavaPlugin) buildRemoteCommand(command *Command) (string, string, error) {
	remoteCommandTokens := []string{JavaDetectionCommand}
	LogVerbosef("Building remote command tokens")
	LogVerbosef("Java detection command: %s", JavaDetectionCommand)

	// Add required tools
	for _, requiredTool := range command.RequiredTools {
		LogVerbosef("Setting up required tool: %s", requiredTool)
		uppercase := strings.ToUpper(requiredTool)
		toolCommand := fmt.Sprintf(`%[1]s_TOOL_PATH=$(find -executable -name %[2]s | head -1 | tr -d [:space:]); if [ -z "$%[1]s_TOOL_PATH" ]; then echo "%[2]s not found"; exit 1; fi; %[1]s_COMMAND=$(realpath "$%[1]s_TOOL_PATH")`, uppercase, requiredTool)
		if requiredTool == "jcmd" {
			remoteCommandTokens = append(remoteCommandTokens, toolCommand, "ASPROF_COMMAND=$(realpath $(find -executable -name asprof | head -1 | tr -d [:space:])); if [ -n \"${ASPROF_COMMAND}\" ]; then JCMD_COMMAND=\"${ASPROF_COMMAND} jcmd\"; fi")
			LogVerbosef("Added jcmd with asprof fallback")
		} else {
			remoteCommandTokens = append(remoteCommandTokens, toolCommand)
			LogVerbosef("Added tool command for %s", requiredTool)
		}
	}

	// Initialize file paths
	fileName, fspath, err := c.initializeFilePaths(command)
	if err != nil {
		return "", "", err
	}

	// Process command text with variable replacements
	commandText, err := c.processCommandText(command, fspath, fileName)
	if err != nil {
		return "", "", err
	}

	// Add command to token list
	if command.GenerateArbitraryFiles {
		remoteCommandTokens = append(remoteCommandTokens, "mkdir -p "+fspath, "cd "+fspath, commandText)
		LogVerbosef("Added directory creation and navigation before command execution")
	} else {
		remoteCommandTokens = append(remoteCommandTokens, commandText)
	}

	LogVerbosef("Command text after replacements: %s", commandText)
	LogVerbosef("Full remote command tokens: %v", remoteCommandTokens)

	remoteCommand := strings.Join(remoteCommandTokens, "; ")
	LogVerbosef("Final remote command: %s", remoteCommand)

	return remoteCommand, fileName, nil
}

// initializeFilePaths sets up file paths for commands that generate files
func (c *JavaPlugin) initializeFilePaths(command *Command) (string, string, error) {
	fileName := ""
	fspath := c.options.ContainerDir

	if command.GenerateFiles || command.NeedsFileName || command.GenerateArbitraryFiles {
		LogVerbosef("Command requires file generation")
		var err error
		fspath, err = utils.GetAvailablePath(c.options.ApplicationName, c.options.ContainerDir)
		if err != nil {
			return "", "", fmt.Errorf("failed to get available path: %w", err)
		}
		if fspath == "" {
			return "", "", fmt.Errorf("no available path found for file generation")
		}
		LogVerbosef("Available path: %s", fspath)

		if command.GenerateArbitraryFiles {
			fspath = fspath + "/" + command.GenerateArbitraryFilesFolderName
			LogVerbosef("Updated path for arbitrary files: %s", fspath)
		}

		fileName = fspath + "/" + c.options.ApplicationName + "-" + command.FileNamePart + "-" + utils.GenerateUUID() + command.FileExtension
		LogVerbosef("Generated filename: %s", fileName)
	}

	return fileName, fspath, nil
}

// processCommandText handles variable replacement in the SSH command
func (c *JavaPlugin) processCommandText(command *Command, fspath, fileName string) (string, error) {
	staticFileName := fspath + "/" + c.options.ApplicationName + command.FileNamePart + command.FileExtension
	LogVerbosef("Generated static filename without UUID: %s", staticFileName)

	// Inline variable replacement logic
	variables := map[string]string{
		"APP_NAME":         c.options.ApplicationName,
		"FSPATH":           fspath,
		"FILE_NAME":        fileName,
		"STATIC_FILE_NAME": staticFileName,
		"ARGS":             c.options.Args,
	}

	replacer := utils.NewVariableReplacer(
		variables,
		utils.WithPrefix("@"),
		utils.WithSpecialVariables("@ARGS"), // @ARGS can contain other variables
	)

	if err := replacer.ValidateVariables(); err != nil {
		return "", fmt.Errorf("variable replacement failed: %w", err)
	}

	commandText := replacer.ReplaceInText(command.SSHCommand)
	return commandText, nil
}

// executeRemoteCommand executes the SSH command and handles file operations
func (c *JavaPlugin) executeRemoteCommand(command *Command, cfSSHArguments []string, remoteCommand, fileName string) (string, error) {
	cfSSHArguments = append(cfSSHArguments, "--command", remoteCommand)
	LogVerbosef("Executing command: %v", cfSSHArguments)

	output, err := c.cliConnection.CliCommand(cfSSHArguments...)
	if err != nil {
		return "", c.handleCommandExecutionError(err, output)
	}

	// Handle file operations if the command generates files
	if command.GenerateFiles {
		return c.handleGeneratedFiles(command, cfSSHArguments, fileName, output)
	}

	if command.GenerateArbitraryFiles && !c.options.NoDownload {
		return c.handleArbitraryFiles(command, cfSSHArguments, fileName, output)
	}

	LogVerbosef("Command execution completed successfully")
	return strings.Join(output, "\n"), nil
}

// handleCommandExecutionError provides consistent error handling for command execution
func (c *JavaPlugin) handleCommandExecutionError(err error, output []string) error {
	if err.Error() == UnexpectedEOFError {
		return errors.New(CommandExecutionError)
	}
	if len(output) == 0 {
		return fmt.Errorf("%s: %w", NoOutputError, err)
	}
	return fmt.Errorf("command execution failed: %w\nOutput: %s", err, strings.Join(output, "\n"))
}

func (c *JavaPlugin) handleGeneratedFiles(command *Command, cfSSHArguments []string, fileName string, output []string) (string, error) {
	LogVerbosef("Processing file generation and download")

	finalFile, err := c.findGeneratedFile(command, cfSSHArguments, fileName)
	if err != nil {
		return "", err
	}

	if finalFile != "" {
		fileName = finalFile
		LogVerbosef("Found file: %s", finalFile)
		fmt.Println("Successfully created " + command.FileLabel + " in application container at: " + fileName)
	} else if !c.options.NoDownload {
		fmt.Println("Failed to find " + command.FileLabel + " in application container")
		return "", fmt.Errorf("file not found")
	}

	if c.options.NoDownload {
		fmt.Println("No download requested, skipping file download")
		return strings.Join(output, "\n"), nil
	}

	return c.downloadAndCleanupFile(command, cfSSHArguments, fileName, output)
}

// findGeneratedFile locates the generated file based on the command type
func (c *JavaPlugin) findGeneratedFile(command *Command, cfSSHArguments []string, fileName string) (string, error) {
	switch command.FileExtension {
	case HeapDumpExtension:
		LogVerbosef("Finding heap dump file")
		return utils.FindHeapDumpFile(cfSSHArguments, fileName)
	case JFRExtension:
		LogVerbosef("Finding JFR file")
		return utils.FindJFRFile(cfSSHArguments, fileName)
	default:
		return "", &InvalidUsageError{
			message: fmt.Sprintf("unsupported file extension %q for command %s",
				command.FileExtension, command.Name),
		}
	}
}

// downloadAndCleanupFile handles the download and cleanup process
func (c *JavaPlugin) downloadAndCleanupFile(command *Command, cfSSHArguments []string, fileName string, output []string) (string, error) {
	// Download the file
	localFileFullPath := c.options.LocalDir + "/" + c.options.ApplicationName + "-" + command.FileNamePart + "-" + utils.GenerateUUID() + command.FileExtension
	LogVerbosef("Downloading file to: %s", localFileFullPath)

	if err := utils.CopyOverCat(cfSSHArguments, fileName, localFileFullPath); err != nil {
		LogVerbosef("File download failed: %v", err)
		return "", err
	}

	LogVerbosef("File download completed successfully")
	fmt.Println(utils.ToSentenceCase(command.FileLabel) + " file saved to: " + localFileFullPath)

	// Clean up remote file if requested
	if !c.options.Keep {
		if err := c.cleanupRemoteFile(command, cfSSHArguments, fileName); err != nil {
			return "", err
		}
	} else {
		LogVerbosef("Keeping remote file as requested")
	}

	return strings.Join(output, "\n"), nil
}

// cleanupRemoteFile removes the remote file after download
func (c *JavaPlugin) cleanupRemoteFile(command *Command, cfSSHArguments []string, fileName string) error {
	LogVerbosef("Deleting remote file")
	if err := utils.DeleteRemoteFile(cfSSHArguments, fileName); err != nil {
		LogVerbosef("Failed to delete remote file: %v", err)
		return err
	}
	LogVerbosef("Remote file deleted successfully")
	fmt.Println(utils.ToSentenceCase(command.FileLabel) + " file deleted in application container")
	return nil
}

// handleArbitraryFiles processes download for commands that generate arbitrary files
func (c *JavaPlugin) handleArbitraryFiles(
	_ *Command,
	cfSSHArguments []string,
	fileName string,
	output []string,
) (string, error) {
	fspath := utils.ExtractFspath(fileName)
	LogVerbosef("Processing arbitrary files download: %s", fspath)
	LogVerbosef("cfSSHArguments: %v", cfSSHArguments)

	// List and download all files in the folder
	files, err := utils.ListFiles(cfSSHArguments, fspath)
	for i, file := range files {
		LogVerbosef("File %d: %s", i+1, file)
	}
	if err != nil {
		LogVerbosef("Failed to list files: %v", err)
		return "", err
	}

	LogVerbosef("Found %d files to download", len(files))
	if len(files) == 0 {
		LogVerbosef("No files found to download")
		return strings.Join(output, "\n"), nil
	}

	if err := c.downloadFiles(files, cfSSHArguments, fspath); err != nil {
		return "", err
	}

	if err := c.cleanupRemoteFiles(cfSSHArguments, fspath); err != nil {
		return "", err
	}

	return strings.Join(output, "\n"), nil
}

// downloadFiles downloads all files from the remote location
func (c *JavaPlugin) downloadFiles(files []string, cfSSHArguments []string, fspath string) error {
	for _, file := range files {
		LogVerbosef("Downloading file: %s", file)
		localFileFullPath := c.options.LocalDir + "/" + file
		if err := utils.CopyOverCat(cfSSHArguments, fspath+"/"+file, localFileFullPath); err != nil {
			LogVerbosef("Failed to download file %s: %v", file, err)
			return err
		}
		LogVerbosef("File %s downloaded successfully", file)
		fmt.Printf("File %s saved to: %s\n", file, localFileFullPath)
	}
	return nil
}

// cleanupRemoteFiles removes remote files if cleanup is requested
func (c *JavaPlugin) cleanupRemoteFiles(cfSSHArguments []string, fspath string) error {
	if c.options.Keep || c.options.NoDownload {
		LogVerbosef("Keeping remote files as requested")
		return nil
	}

	LogVerbosef("Deleting remote file folder")
	if err := utils.DeleteRemoteFile(cfSSHArguments, fspath); err != nil {
		LogVerbosef("Failed to delete remote folder: %v", err)
		return err
	}
	LogVerbosef("Remote folder deleted successfully")
	fmt.Println("File folder deleted in application container")
	return nil
}

// GetMetadata must be implemented as part of the plugin interface
// defined by the core CLI.
//
// GetMetadata() returns a PluginMetadata struct. The first field, Name,
// determines the Name of the plugin which should generally be without spaces.
// If there are spaces in the Name a user will need to properly quote the Name
// during uninstall otherwise the Name will be treated as separate arguments.
// The second value is a slice of Command structs. Our slice only contains one
// Command Struct, but could contain any number of them. The first field Name
// defines the command `cf java` once installed into the CLI. The
// second field, HelpText, is used by the core CLI to display help information
// to the user in the core commands `cf help`, `cf`, or `cf -h`.
func (c *JavaPlugin) GetMetadata() plugin.PluginMetadata {
	usageText := "cf " + PluginName + " COMMAND APP_NAME [options]"
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
		usageText += "\n" + utils.WrapTextWithPrefix(command.Description, "        ", 80, 0)
	}
	return plugin.PluginMetadata{
		Name: PluginName,
		Version: plugin.VersionType{
			Major: PluginVersion.Major,
			Minor: PluginVersion.Minor,
			Build: PluginVersion.Build,
		},
		MinCliVersion: plugin.VersionType{
			Major: MinCLIVersion.Major,
			Minor: MinCLIVersion.Minor,
			Build: MinCLIVersion.Build,
		},
		Commands: []plugin.Command{
			{
				Name:     PluginName,
				HelpText: "Obtain a heap-dump, thread-dump or profile from a running, SSH-enabled Java application.",

				// UsageDetails is optional
				// It is used to show help of usage of each command
				UsageDetails: plugin.Usage{
					Usage:   usageText,
					Options: utils.GenerateOptionsMap(flagDefinitions),
				},
			},
		},
	}
}

// Unlike most Go programs, the main() function will not be used to run all of the
// commands provided in your plugin. Main will be used to initialize the plugin
// process, as well as any dependencies you might require for your plugin.
func main() {
	// Any initialization for your plugin can be handled here
	//
	// Note: to run the plugin.Start method, we pass in a pointer to the struct
	// implementing the interface defined at "code.cloudfoundry.org/cli/plugin/plugin.go"
	//
	// Note: The plugin's main() method is invoked at install time to collect
	// metadata. The plugin will exit 0 and the Run([]string) method will not be
	// invoked.
	plugin.Start(NewJavaPlugin())
	// Plugin code should be written in the Run([]string) method,
	// ensuring the plugin environment is bootstrapped.
}
