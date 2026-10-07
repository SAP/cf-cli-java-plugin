/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

// Package main implements a CF CLI plugin for Java applications, providing commands
// for heap dumps, thread dumps, profiling, and other Java diagnostics.
package main

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/cli/cf/terminal"
	"code.cloudfoundry.org/cli/cf/trace"
	"code.cloudfoundry.org/cli/plugin"

	"cf.plugin.ref/requires/utils"

	"github.com/simonleung8/flags"
)

// Assert that JavaPlugin implements plugin.Plugin.
var _ plugin.Plugin = (*JavaPlugin)(nil)

// String constants extracted to satisfy goconst linter.
const (
	cmdSSH                = "ssh"
	cmdJava               = "java"
	flagKeep              = "keep"
	flagNoDownload        = "no-download"
	flagContainerDir      = "container-dir"
	flagLocalDir          = "local-dir"
	flagRedact            = "redact"
	flagRedactComplete    = "redact-complete"
	flagRedactKeepOnError = "redact-keep-on-error"
	flagCompress          = "compress"
	flagOpen              = "open"
	flagOpenURL           = "open-url"
	defaultOpenURL        = "https://parttimenerd.github.io/hprof-analyzer"
	osWindows             = "windows"
	cmdHeapDump           = "heap-dump"
	typeBool              = "bool"
	typeString            = "string"
	toolJcmd              = "jcmd"
	toolAsprof            = "asprof"
	extJFR                = ".jfr"
	labelJFR              = "JFR recording"
	partJFR               = "jfr"
	extHprof              = ".hprof"
	extHprofGz            = ".hprof.gz"
)

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

func isSSHConnectivityError(errorOutput string, err error) bool {
	if err == nil {
		return false
	}

	combined := strings.ToLower(err.Error() + " " + errorOutput)
	patterns := []string{
		"ssh not enabled",
		"connection refused",
		"connection reset",
		"permission denied",
		"authentication failed",
		"handshake failed",
		"one time auth code",
		"timeout",
		"deadline exceeded",
		"specified application instance does not exist",
		"of process web not found",
		"of process web not running",
	}

	for _, pattern := range patterns {
		if strings.Contains(combined, pattern) {
			return true
		}
	}

	return false
}

// wrapSSHError analyzes SSH error messages and provides user-friendly explanations.
func wrapSSHError(appName string, errorOutput string, err error) string {
	errStr := err.Error() + " " + errorOutput
	errStrLower := strings.ToLower(errStr)

	if strings.Contains(errStrLower, "specified application instance does not exist") ||
		strings.Contains(errStrLower, "of process web not found") ||
		strings.Contains(errStrLower, "of process web not running") {
		return fmt.Sprintf(
			"Cannot connect to app '%s' via SSH because the requested application instance is not available.\n"+
				"Verify the instance index with: cf app %s\n"+
				"Technical details: %v",
			appName,
			appName,
			err,
		)
	}

	if strings.Contains(errStrLower, "connection refused") ||
		strings.Contains(errStrLower, "connection reset") ||
		strings.Contains(errStrLower, "ssh not enabled") {
		return fmt.Sprintf(
			"Cannot connect to app '%s' via SSH.\n"+
				"Possible causes and solutions:\n"+
				"1. SSH may not be enabled on the application. Try:\n"+
				"   cf enable-ssh %s\n"+
				"   cf restart %s\n"+
				"2. Check your network connection and firewall settings.\n"+
				"3. Verify the application is running: cf app %s\n"+
				"Technical details: %v",
			appName,
			appName,
			appName,
			appName,
			err,
		)
	}

	if strings.Contains(errStrLower, "permission denied") ||
		strings.Contains(errStrLower, "authentication failed") ||
		strings.Contains(errStrLower, "one time auth code") ||
		strings.Contains(errStrLower, "handshake failed") {
		return fmt.Sprintf(
			"SSH authentication failed for app '%s'.\n"+
				"This may indicate:\n"+
				"1. You are not logged in to Cloud Foundry. Try: cf login\n"+
				"2. Your CF credentials have expired. Try: cf logout && cf login\n"+
				"3. You don't have permissions for this app.\n"+
				"Technical details: %v",
			appName,
			err,
		)
	}

	if strings.Contains(errStrLower, "timeout") ||
		strings.Contains(errStrLower, "deadline exceeded") {
		return fmt.Sprintf(
			"SSH connection to app '%s' timed out.\n"+
				"This may indicate:\n"+
				"1. Slow network or high latency\n"+
				"2. Cloud Foundry platform is experiencing issues\n"+
				"3. Firewall or proxy is blocking the connection\n"+
				"Try again, or contact your Cloud Foundry administrator if this persists.",
			appName,
		)
	}

	return fmt.Sprintf(
		"SSH command failed while connecting to app '%s'.\n"+
			"Details: %v\n"+
			"Output: %s\n"+
			"To debug, try manually: cf ssh %s -c 'echo ok'",
		appName,
		err,
		errorOutput,
		appName,
	)
}

// checkSSHConnectivity tests whether the app is reachable via SSH before running the main command.
func (c *JavaPlugin) checkSSHConnectivity(appName string, appInstanceIndex int) error {
	testArgs := []string{cmdSSH, appName}
	if appInstanceIndex >= 0 {
		testArgs = append(testArgs, "--app-instance-index", strconv.Itoa(appInstanceIndex))
	}
	testArgs = append(testArgs, "-c", "echo ok")

	c.logVerbosef("Checking SSH connectivity to app '%s'", appName)
	cmd := exec.Command("cf", testArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		c.logVerbosef("SSH connectivity check failed: %v", err)
		return fmt.Errorf("%s", wrapSSHError(appName, string(output), err))
	}

	c.logVerbosef("SSH connectivity check succeeded")
	return nil
}

// Options holds all command-line options for the Java plugin
type Options struct {
	AppInstanceIndex  int
	Keep              bool
	NoDownload        bool
	DryRun            bool
	Verbose           bool
	Full              bool
	ContainerDir      string
	LocalDir          string
	Args              string
	Redact            bool
	RedactComplete    bool
	RedactKeepOnError bool
	Compress          bool
	Open              bool
	OpenURL           string
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
		DefaultInt:  -1,
	},
	{
		Name:        flagKeep,
		ShortName:   "k",
		Usage:       "whether to `keep` the heap-dump/JFR/... files on the container of the application instance after having downloaded it locally",
		Description: "keep the heap dump in the container; by default the heap dump/JFR/... will be deleted from the container's filesystem after being downloaded",
		Type:        typeBool,
	},
	{
		Name:        flagNoDownload,
		ShortName:   "nd",
		Usage:       "do not download the heap-dump/JFR/... file to the local machine",
		Description: "don't download the heap dump/JFR/... file to local, only keep it in the container, implies '--keep'",
		Type:        typeBool,
	},
	{
		Name:        "dry-run",
		ShortName:   "n",
		Usage:       "triggers the `dry-run` mode to show only the cf-ssh command that would have been executed",
		Description: "just output to command line what would be executed",
		Type:        typeBool,
	},
	{
		Name:        "verbose",
		ShortName:   "",
		Usage:       "enable verbose output for the plugin (note: -v is reserved by CF CLI)",
		Description: "enable verbose output for the plugin (note: -v is reserved by CF CLI for its own trace mode and cannot be used as a shorthand here)",
		Type:        typeBool,
	},
	{
		Name:        flagContainerDir,
		ShortName:   "cd",
		Usage:       "specify the folder path where the dump/JFR/... file should be stored in the container",
		Description: "the directory path in the container that the heap dump/JFR/... file will be saved to",
		Type:        typeString,
	},
	{
		Name:        flagLocalDir,
		ShortName:   "ld",
		Usage:       "specify the folder where the dump/JFR/... file will be downloaded to, defaults to the current directory",
		Description: "the local directory path that the dump/JFR/... file will be saved to, defaults to the current directory",
		Type:        typeString,
	},
	{
		Name:        "full",
		ShortName:   "f",
		Usage:       "enable `full` mode for more comprehensive analysis (status and record-status commands)",
		Description: "enable full mode for more comprehensive JVM analysis (only for status and record-status)",
		Type:        typeBool,
	},
	{
		Name:        "args",
		ShortName:   "a",
		Usage:       "Miscellaneous arguments to pass to the command in the container, be aware to end it with a space if it is a simple option",
		Description: "Miscellaneous arguments to pass to the command (if supported) in the container, be aware to end it with a space if it is a simple option. For commands that create arbitrary files (jcmd, asprof), the environment variables @FSPATH, @ARGS, @APP_NAME, @FILE_NAME, and @STATIC_FILE_NAME are available in --args to reference the working directory path, arguments, application name, and generated file name respectively.",
		Type:        typeString,
	},
	{
		Name:        flagRedact,
		Usage:       "redact heap dump (lean mode: zero primitive arrays only) before saving locally",
		Description: "redact heap dump before saving locally (lean mode: zero primitive arrays only)",
		Type:        typeBool,
	},
	{
		Name:        flagRedactComplete,
		Usage:       "redact heap dump (complete mode: zero all primitive values) before saving locally",
		Description: "redact heap dump before saving locally (complete mode: zero all primitive values)",
		Type:        typeBool,
	},
	{
		Name:        flagRedactKeepOnError,
		Usage:       "keep partially-written redacted file if redaction fails (default: delete it)",
		Description: "keep partially-written redacted file if redaction fails (default: delete it)",
		Type:        typeBool,
	},
	{
		Name:        flagCompress,
		Usage:       "compress heap dump on container using jmap gz=1 (JDK 17+) to reduce transfer size; output is .hprof.gz",
		Description: "compress heap dump on the container before downloading (JDK 17+, reduces transfer size); output file will be .hprof.gz",
		Type:        typeBool,
	},
	{
		Name:        flagOpen,
		Usage:       "open the heap dump in the hprof-analyzer web app after downloading",
		Description: "open the heap dump in the hprof-analyzer web app after downloading",
		Type:        typeBool,
	},
	{
		Name:        flagOpenURL,
		Usage:       "base URL of the hprof-analyzer instance to open (implies --open)",
		Description: "base URL of the hprof-analyzer instance to open (implies --open)",
		Type:        typeString,
	},
}

func (c *JavaPlugin) createOptionsParser() flags.FlagContext {
	commandFlags := flags.New()

	// Create flags from centralized definitions
	for _, flagDef := range flagDefinitions {
		short := flagDef.ShortName
		switch flagDef.Type {
		case "int":
			commandFlags.NewIntFlagWithDefault(flagDef.Name, short, flagDef.Usage, flagDef.DefaultInt)
		case "bool":
			commandFlags.NewBoolFlag(flagDef.Name, short, flagDef.Usage)
		case "string":
			commandFlags.NewStringFlag(flagDef.Name, short, flagDef.Usage)
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

	appInstanceIndex := commandFlags.Int("app-instance-index")
	// simonleung8/flags registers flags with non-zero defaults in flagsets at init time,
	// so IsSet() returns true even when the flag was not explicitly provided.
	// Check against the known default (-1) to detect actual user-provided values.
	appInstanceIndexSet := commandFlags.IsSet("app-instance-index") && appInstanceIndex != -1
	keep := commandFlags.IsSet("keep")
	noDownload := commandFlags.IsSet("no-download")

	// Validate: contradictory flags
	if keep && noDownload {
		return nil, nil, &InvalidUsageError{
			message: "Error: flags '--keep' and '--no-download' are contradictory. Use '--no-download' to keep remote file without downloading, or '--keep' to download and keep a copy remote.",
		}
	}

	// Validate: instance index must be non-negative
	if appInstanceIndexSet && appInstanceIndex < 0 {
		return nil, nil, &InvalidUsageError{
			message: fmt.Sprintf("Error: app instance index must be non-negative, got %d", appInstanceIndex),
		}
	}

	// Validate: instance index should not be excessively large (sanity check)
	if appInstanceIndex > 9999 {
		return nil, nil, &InvalidUsageError{
			message: fmt.Sprintf("Error: app instance index is unreasonably large (%d). Cloud Foundry applications typically have fewer than 100 instances.", appInstanceIndex),
		}
	}

	options := &Options{
		AppInstanceIndex:  appInstanceIndex,
		Keep:              keep,
		NoDownload:        noDownload,
		DryRun:            commandFlags.IsSet("dry-run"),
		Verbose:           commandFlags.IsSet("verbose"),
		Full:              commandFlags.IsSet("full"),
		ContainerDir:      commandFlags.String("container-dir"),
		LocalDir:          commandFlags.String("local-dir"),
		Args:              commandFlags.String("args"),
		Redact:            commandFlags.IsSet(flagRedact),
		RedactComplete:    commandFlags.IsSet(flagRedactComplete),
		RedactKeepOnError: commandFlags.IsSet(flagRedactKeepOnError),
		Compress:          commandFlags.IsSet(flagCompress),
		Open:              commandFlags.IsSet(flagOpen) || commandFlags.IsSet(flagOpenURL),
		OpenURL: func() string {
			if u := commandFlags.String(flagOpenURL); u != "" {
				return u
			}
			return defaultOpenURL
		}(),
	}

	if options.Redact && options.RedactComplete {
		return nil, nil, &InvalidUsageError{
			message: "Error: flags '--redact' and '--redact-complete' are mutually exclusive",
		}
	}

	if options.Open && options.NoDownload {
		return nil, nil, &InvalidUsageError{
			message: "Error: flag '--open' requires a local file and cannot be used with '--no-download'",
		}
	}

	return options, commandFlags.Args(), nil
}

// generateOptionsMapFromFlags creates the options map for plugin metadata
func (c *JavaPlugin) generateOptionsMapFromFlags() map[string]string {
	options := make(map[string]string)

	// Generate options from the centralized flag definitions
	for _, flagDef := range flagDefinitions {
		var prefix string
		if flagDef.ShortName != "" {
			prefix = "-" + flagDef.ShortName
			if flagDef.Name == "app-instance-index" {
				prefix += " [index]"
			}
			prefix += ", "
		}

		// Use the Description field for detailed help text.
		// miscLineIndent aligns continuation lines: prefix + indent must equal the
		// widest prefix used ("-i [index], " = 12 chars, indent 19 → total 31).
		indent := 31 - len(prefix)
		if indent < 0 {
			indent = 0
		}
		options[flagDef.Name] = utils.WrapTextWithPrefix(flagDef.Description, prefix, 80, indent)
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

	// AttachSocketNCHelper defines a shell function `nc_jcmd PID CMDLINE...` that sends one
	// HotSpot attach-protocol request to the JVM's Unix-domain socket via nc.
	// This is the same protocol jcmd uses internally (JDK 9+) and works on JRE-only containers.
	// Socket location: /tmp/.java_pid<PID> (Linux) or ${TMPDIR%/}/.java_pid<PID> (macOS).
	// If the socket does not exist yet, it is triggered via the standard attach handshake
	// (write .attach_pid<PID> into the JVM's cwd and send SIGQUIT), then polled for 5 s.
	// Requires nc with -U support (netcat-openbsd or nmap-ncat).
	//
	// Protocol: the entire command line (command + args) goes in the first field, NUL-separated:
	//   printf '1\0jcmd\0GC.heap_dump /tmp/out.hprof\0\0\0'
	// Subsequent fields are unused by HotSpot; the JVM splits the first field on spaces.
	AttachSocketNCHelper = `nc_jcmd() {
  _pid=$1; shift; _cmdline="$*"
  _sock=$(if [ -S "/tmp/.java_pid${_pid}" ]; then echo "/tmp/.java_pid${_pid}"; \
          elif [ -n "$TMPDIR" ] && [ -S "${TMPDIR%/}/.java_pid${_pid}" ]; then echo "${TMPDIR%/}/.java_pid${_pid}"; fi)
  if [ -z "$_sock" ]; then
    _cwd=$(readlink /proc/${_pid}/cwd 2>/dev/null || echo /tmp)
    touch "${_cwd}/.attach_pid${_pid}" 2>/dev/null; kill -QUIT "${_pid}" 2>/dev/null
    for _i in 1 2 3 4 5 6 7 8 9 10; do sleep 0.5
      _sock=$(if [ -S "/tmp/.java_pid${_pid}" ]; then echo "/tmp/.java_pid${_pid}"; \
              elif [ -n "$TMPDIR" ] && [ -S "${TMPDIR%/}/.java_pid${_pid}" ]; then echo "${TMPDIR%/}/.java_pid${_pid}"; fi)
      [ -n "$_sock" ] && break
    done
  fi
  [ -z "$_sock" ] && { echo >&2 "nc_jcmd: attach socket not found for PID ${_pid}"; return 1; }
  _raw=$(printf '1\0jcmd\0%s\0\0\0' "$_cmdline" | nc -w 2 -U "$_sock" 2>/dev/null)
  _rc=$(echo "$_raw" | head -n 1)
  _body=$(echo "$_raw" | tail -n +2)
  if [ "$_rc" != "0" ]; then echo >&2 "nc_jcmd: command '$_cmdline' failed (rc=$_rc): $_body"; return 1; fi
  echo "$_body"
};
nc_available() { command -v nc >/dev/null 2>&1 && nc -h 2>&1 | grep -q '\-U'; };`
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
	// Note: -v is reserved by CF CLI (enables CF_TRACE). Only --verbose works.
	for _, arg := range args {
		if arg == "--verbose" {
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
			err := exec.Command("cf", "help", cmdJava).Run()
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
	// IsLocal indicates the command runs locally (not via SSH)
	IsLocal bool
	// AcceptsTrailingArgs indicates the command accepts positional arguments after the app name
	AcceptsTrailingArgs bool
	// SupportFullOption indicates the command supports the --full flag
	SupportFullOption bool
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
		Name:          cmdHeapDump,
		Description:   "Generate a heap dump from a running Java application",
		GenerateFiles: true,
		FileExtension: extHprof,
		/*
					If there is not enough space on the filesystem to write the dump, jmap will create a file
			with size 0, output something about not enough space left on the device, and exit with status code 0.
			Because YOLO.

			Also: if the heap dump file already exists, jmap will output something about the file already
			existing and exit with status code 0. At least it is consistent.

			OpenJDK: Wrap everything in an if statement in case jmap is available
		*/
		SSHCommand: AttachSocketNCHelper + `if [ -f @FILE_NAME ]; then echo >&2 'Heap dump @FILE_NAME already exists'; exit 1; fi
JMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])
# SAP JVM: Wrap everything in an if statement in case jvmmon is available
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])
_pid=$(pidof java)
if [ -n "${JMAP_COMMAND}" ]; then
GZ_ARG=""
if ${JMAP_COMMAND} -h 2>&1 | grep -q "gz="; then GZ_ARG=",gz=1"; fi
OUTPUT=$( ${JMAP_COMMAND} -dump:format=b${GZ_ARG},file=@FILE_NAME ${_pid} ) || STATUS_CODE=$?
if [ ! -s @FILE_NAME ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
elif [ -n "${JVMMON_COMMAND}" ]; then
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=@FSPATH\ndump heap' > setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid ${_pid} -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
sleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file
HEAP_DUMP_NAME=$(find @FSPATH -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done
if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
if [ -n "@COMPRESS_FLAG" ]; then gzip -1 "${HEAP_DUMP_NAME}" && HEAP_DUMP_NAME="${HEAP_DUMP_NAME}.gz"; fi
elif nc_available; then
nc_jcmd ${_pid} GC.heap_dump @FILE_NAME || exit 1
if [ ! -s @FILE_NAME ]; then echo >&2 "Heap dump file not created or empty"; exit 1; fi
else
  echo >&2 "jvmmon, jmap, or nc (netcat-openbsd / nmap-ncat) are required for generating a heap dump.
To use a JDK image, set JBP_CONFIG_OPEN_JDK_JRE in your manifest.yaml:
  env:
    JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: \"https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64\", version: 21.+ } }'
Or install netcat on the container (apt-get install netcat-openbsd)."
  exit 1
fi`,
		FileLabel:    "heap dump",
		FileNamePart: "heapdump",
	},
	{
		Name:          "thread-dump",
		Description:   "Generate a thread dump from a running Java application",
		GenerateFiles: false,
		SSHCommand: AttachSocketNCHelper + `
JSTACK_COMMAND=$(find -executable -name jstack | head -1);
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1)
_pid=$(pidof java)
if [ -n "${JSTACK_COMMAND}" ]; then ${JSTACK_COMMAND} ${_pid}; exit 0; fi
if [ -n "${JVMMON_COMMAND}" ]; then ${JVMMON_COMMAND} -pid ${_pid} -c "print stacktrace"; exit 0; fi
if nc_available; then nc_jcmd ${_pid} Thread.print; exit 0; fi
echo >&2 "jstack, jvmmon, or nc (netcat-openbsd / nmap-ncat) are required for generating a thread dump.
To use a JDK image, set JBP_CONFIG_OPEN_JDK_JRE in your manifest.yaml:
  env:
    JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: \"https://java-buildpack.cloudfoundry.org/openjdk-jdk/jammy/x86_64\", version: 21.+ } }'
Or install netcat on the container (apt-get install netcat-openbsd)."
exit 1`,
	},
	{
		Name:          "vm-info",
		Description:   "Print information about the Java Virtual Machine running a Java application",
		GenerateFiles: false,
		SSHCommand: AttachSocketNCHelper + FilterJCMDRemoteMessage + `
_pid=$(pidof java)
JCMD_COMMAND=$(find -executable -name jcmd | head -1)
if [ -n "${JCMD_COMMAND}" ]; then ${JCMD_COMMAND} ${_pid} VM.info | filter_jcmd_remote_message; exit 0; fi
if nc_available; then nc_jcmd ${_pid} VM.info; exit 0; fi
echo >&2 "jcmd not found. Install netcat (netcat-openbsd / nmap-ncat) for JRE-only containers."; exit 1`,
	},
	{
		Name:                             toolJcmd,
		Description:                      "Run a JCMD command on a running Java application via --args, downloads and deletes all files that are created in the current folder, use '--no-download' to prevent this. Environment variables available: @FSPATH (writable directory path, always set), @ARGS (command arguments), @APP_NAME (application name), @FILE_NAME (generated filename with UUID for file operations), and @STATIC_FILE_NAME (without UUID). Use single quotes around --args to prevent shell expansion.",
		RequiredTools:                    []string{},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: toolJcmd,
		SSHCommand: AttachSocketNCHelper + FilterJCMDRemoteMessage + `_pid=$(pidof java)
JCMD_COMMAND=$(find -executable -name jcmd | head -1)
if [ -n "${JCMD_COMMAND}" ]; then ${JCMD_COMMAND} ${_pid} @ARGS | filter_jcmd_remote_message; exit 0; fi
if nc_available; then nc_jcmd ${_pid} @ARGS; exit 0; fi
echo >&2 "jcmd not found. Install netcat (netcat-openbsd / nmap-ncat) for JRE-only containers."; exit 1`,
	},
	{
		Name:          "jfr-start",
		Description:   "Start a Java Flight Recorder default recording on a running Java application (stores in the container-dir)",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: extJFR,
		FileLabel:     labelJFR,
		FileNamePart:  partJFR,
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=default.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-start-profile",
		Description:   "Start a Java Flight Recorder profile recording on a running Java application (stores in the container-dir)",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: false,
		NeedsFileName: true,
		FileExtension: extJFR,
		FileLabel:     labelJFR,
		FileNamePart:  partJFR,
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=profile.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc",
		Description:            "Start a Java Flight Recorder GC recording on a running Java application (stores in the container-dir)",
		RequiredTools:          []string{toolJcmd},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileLabel:              labelJFR,
		FileNamePart:           partJFR,
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "jfr-start-gc-details",
		Description:            "Start a Java Flight Recorder detailed GC recording on a running Java application (stores in the container-dir)",
		RequiredTools:          []string{toolJcmd},
		GenerateFiles:          false,
		OnlyOnRecentSapMachine: true,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileLabel:              labelJFR,
		FileNamePart:           partJFR,
		SSHCommand: FilterJCMDRemoteMessage + CheckNoCurrentJFRRecordingCommand +
			`$JCMD_COMMAND $(pidof java) JFR.start settings=gc_details.jfc filename=@FILE_NAME name=JFR | filter_jcmd_remote_message;
		echo "Use 'cf java jfr-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:          "jfr-stop",
		Description:   "Stop a Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: true,
		FileExtension: extJFR,
		FileLabel:     labelJFR,
		FileNamePart:  partJFR,
		SSHCommand: FilterJCMDRemoteMessage + ` output=$($JCMD_COMMAND $(pidof java) JFR.stop name=JFR | filter_jcmd_remote_message);
		echo "$output"; echo ""; filename=$(echo "$output" | grep /.*.jfr --only-matching);
		if [ -z "$filename" ]; then echo "No active JFR recording found to stop"; exit 1; fi;
		if [ ! -f "$filename" ]; then echo "JFR recording $filename does not exist"; exit 1; fi;
		if [ ! -s "$filename" ]; then echo "JFR recording $filename is empty"; exit 1; fi;
		mv "$filename" @FILE_NAME;
		echo "JFR recording copied to @FILE_NAME"`,
	},
	{
		Name:          "jfr-dump",
		Description:   "Dump a Java Flight Recorder recording on a running Java application without stopping it",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: true,
		FileExtension: extJFR,
		FileLabel:     labelJFR,
		FileNamePart:  partJFR,
		SSHCommand: FilterJCMDRemoteMessage + ` output=$($JCMD_COMMAND $(pidof java) JFR.dump name=JFR | filter_jcmd_remote_message);
		echo "$output"; echo ""; filename=$(echo "$output" | grep /.*.jfr --only-matching);
		if [ -z "$filename" ]; then echo "No JFR recording found to dump"; exit 1; fi;
		if [ ! -f "$filename" ]; then echo "JFR recording $filename does not exist"; exit 1; fi;
		if [ ! -s "$filename" ]; then echo "JFR recording $filename is empty"; exit 1; fi;
		cp "$filename" @FILE_NAME;
		echo "JFR recording copied to @FILE_NAME";
		echo "Use 'cf java jfr-stop @APP_NAME' to stop the recording and copy the final JFR file to the local folder"`,
	},
	{
		Name:          "jfr-status",
		Description:   "Check the running Java Flight Recorder recording on a running Java application",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) JFR.check | filter_jcmd_remote_message`,
	},
	{
		Name:          "vm-version",
		Description:   "Print the version of the Java Virtual Machine running a Java application",
		GenerateFiles: false,
		SSHCommand: AttachSocketNCHelper + FilterJCMDRemoteMessage + `
_pid=$(pidof java)
JCMD_COMMAND=$(find -executable -name jcmd | head -1)
if [ -n "${JCMD_COMMAND}" ]; then ${JCMD_COMMAND} ${_pid} VM.version | filter_jcmd_remote_message; exit 0; fi
if nc_available; then nc_jcmd ${_pid} VM.version; exit 0; fi
echo >&2 "jcmd not found. Install netcat (netcat-openbsd / nmap-ncat) for JRE-only containers."; exit 1`,
	},
	{
		Name:          "vm-vitals",
		Description:   "Print vital statistics about the Java Virtual Machine running a Java application",
		RequiredTools: []string{toolJcmd},
		GenerateFiles: false,
		SSHCommand:    FilterJCMDRemoteMessage + `$JCMD_COMMAND $(pidof java) VM.vitals | filter_jcmd_remote_message`,
	},
	{
		Name:                             toolAsprof,
		Description:                      "Run async-profiler commands passed to asprof via --args, copies files in the current folder. Don't use in combination with asprof-* commands. Downloads and deletes all files that are created in the current folder, if not using 'start' asprof command, use '--no-download' to prevent this. Environment variables available: @FSPATH (writable directory path, always set), @ARGS (command arguments), @APP_NAME (application name), @FILE_NAME (generated filename for file operations), and @STATIC_FILE_NAME (without UUID). Use single quotes around --args to prevent shell expansion.",
		OnlyOnRecentSapMachine:           true,
		RequiredTools:                    []string{toolAsprof},
		GenerateFiles:                    false,
		GenerateArbitraryFiles:           true,
		GenerateArbitraryFilesFolderName: toolAsprof,
		SSHCommand:                       `$ASPROF_COMMAND $(pidof java) @ARGS`,
	},
	{
		Name:                   "asprof-start-cpu",
		Description:            "Start an async-profiler CPU-time profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{toolAsprof},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileNamePart:           toolAsprof,
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e cpu -f @FILE_NAME && echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-wall",
		Description:            "Start an async-profiler wall-clock profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{toolAsprof},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileNamePart:           toolAsprof,
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e wall -f @FILE_NAME && echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-alloc",
		Description:            "Start an async-profiler allocation profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{toolAsprof},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileNamePart:           toolAsprof,
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e alloc -f @FILE_NAME && echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-start-lock",
		Description:            "Start an async-profiler lock profile recording on a running Java application",
		OnlyOnRecentSapMachine: true,
		RequiredTools:          []string{toolAsprof},
		GenerateFiles:          false,
		NeedsFileName:          true,
		FileExtension:          extJFR,
		FileNamePart:           toolAsprof,
		SSHCommand:             `$ASPROF_COMMAND start $(pidof java) -e lock -f @FILE_NAME && echo "Use 'cf java asprof-stop @APP_NAME' to copy the file to the local folder"`,
	},
	{
		Name:                   "asprof-stop",
		Description:            "Stop an async-profiler profile recording on a running Java application",
		RequiredTools:          []string{toolAsprof},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          true,
		FileExtension:          extJFR,
		FileLabel:              "async-profiler recording",
		FileNamePart:           toolAsprof,
		SSHCommand:             `$ASPROF_COMMAND stop $(pidof java)`,
	},
	{
		Name:                   "asprof-status",
		Description:            "Get the status of async-profiler on a running Java application",
		RequiredTools:          []string{toolAsprof},
		OnlyOnRecentSapMachine: true,
		GenerateFiles:          false,
		SSHCommand:             `$ASPROF_COMMAND status $(pidof java)`,
	},
	{
		Name:              "status",
		Description:       "Quick status check of the remote JVM: deadlock detection, hot threads, dependency graph, and more. Requires Java 17+ locally. Use --full for comprehensive analysis. Pass additional options via --args (e.g., '--dumps 3'). See https://github.com/parttimenerd/jstall",
		IsLocal:           true,
		SupportFullOption: true,
		SSHCommand:        "status all @ARGS",
	},
	{
		Name:              "jstall",
		Description:       "Inspect the remote JVM via JStall (runs on your machine, connects via cf ssh). Requires Java 17+ locally. Subcommands typically require a target (e.g., 'all'). Pass jstall subcommands and options via --args. See https://github.com/parttimenerd/jstall",
		IsLocal:           true,
		SupportFullOption: true,
		SSHCommand:        "@ARGS",
	},
	{
		Name:                "record-status",
		Description:         "Record diagnostic data from the remote JVM via JStall and save to a local zip file. Requires Java 17+ locally. Output file can be specified as a trailing argument (default: APP_NAME-status.zip). Use --full for comprehensive recording. See https://github.com/parttimenerd/jstall",
		IsLocal:             true,
		AcceptsTrailingArgs: true,
		SupportFullOption:   true,
		SSHCommand:          "record all --output @ARGS",
	},
}

func (c *JavaPlugin) execute(cliConnection plugin.CliConnection, args []string) (string, error) {
	if len(args) == 0 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	switch args[0] {
	case "CLI-MESSAGE-UNINSTALL":
		// Nothing to uninstall, we keep no local state
		return "", nil
	case cmdJava:
		break
	default:
		return "", &InvalidUsageError{message: fmt.Sprintf("Unexpected command Name '%s' (expected : 'java')", args[0])}
	}

	options, arguments, parseErr := c.parseOptions(args[1:])
	if parseErr != nil {
		return "", &InvalidUsageError{message: fmt.Sprintf("Error while parsing command arguments: %v", parseErr)}
	}

	fileFlags := []string{flagContainerDir, flagLocalDir, flagKeep, flagNoDownload}

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
	} else {
		// Expand tilde to home directory
		if localDir == "~" || strings.HasPrefix(localDir, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				localDir = home + localDir[1:]
			}
		}
		// Reject path traversal sequences
		cleaned := filepath.Clean(localDir)
		if strings.Contains(cleaned, "..") {
			return "", &InvalidUsageError{message: "Error: --local-dir must not contain path traversal sequences (..)"}
		}
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
	lowerCommandName := strings.ToLower(commandName)
	for i, command := range commands {
		if command.Name == lowerCommandName {
			index = i
			break
		}
	}
	if index == -1 {
		// Handle 'help' as a subcommand
		if lowerCommandName == "help" {
			err := exec.Command("cf", "help", cmdJava).Run()
			if err != nil {
				return "", fmt.Errorf("failed to show help: %w", err)
			}
			return "", nil
		}
		avCommands := make([]string, 0, len(commands))
		for _, command := range commands {
			avCommands = append(avCommands, command.Name)
		}
		// Detect swapped order: cf java MY-APP heap-dump instead of cf java heap-dump MY-APP
		if argumentLen >= 2 && cliConnection != nil {
			secondArg := strings.ToLower(arguments[1])
			for _, cmd := range commands {
				if cmd.Name == secondArg {
					// Confirm the first arg looks like an app name
					if _, appErr := cliConnection.GetApp(commandName); appErr == nil {
						return "", &InvalidUsageError{message: fmt.Sprintf("Did you mean: cf java %s %s? (app and command appear to be swapped)", secondArg, commandName)}
					}
					break
				}
			}
		}
		matches := utils.FuzzySearch(lowerCommandName, avCommands, 3)
		return "", &InvalidUsageError{message: fmt.Sprintf("Unrecognized command %q, did you mean: %s?", commandName, utils.JoinWithOr(matches))}
	}

	command := commands[index]
	c.logVerbosef("Found command: %s - %s", command.Name, command.Description)

	// Warn about heap-dump-only flags used on other commands.
	if command.Name != cmdHeapDump {
		heapOnlyFlags := []struct {
			set  bool
			name string
		}{
			{options.Redact, flagRedact},
			{options.RedactComplete, flagRedactComplete},
			{options.Compress, flagCompress},
			{options.Open, flagOpen},
			{options.OpenURL != "", flagOpenURL},
		}
		for _, f := range heapOnlyFlags {
			if f.set {
				fmt.Fprintf(os.Stderr, "Warning: flag '--%s' is only supported by heap-dump and has no effect here.\n", f.name)
			}
		}
	}

	// Only block CF_TRACE for commands that download files (not for read-only commands like vm-version)
	if os.Getenv("CF_TRACE") == "true" && (command.GenerateFiles || command.GenerateArbitraryFiles) {
		return "", errors.New("the environment variable CF_TRACE is set to true. This prevents download of the dump from succeeding")
	}

	// Handle --help flag for jstall command
	if command.Name == "jstall" {
		for _, arg := range arguments {
			if arg == "--help" || arg == "-h" {
				// Delegate to jstall --help directly
				return c.executeJstall("", "--help", 0, false)
			}
		}
	}

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
	if command.Name == toolAsprof {
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
	// Reject whitespace-only --args
	if options.Args != "" && strings.TrimSpace(options.Args) == "" {
		return "", &InvalidUsageError{message: "Error: --args must not be empty or whitespace-only"}
	}
	// Validate that commands requiring @ARGS have arguments provided
	if command.HasMiscArgs() && options.Args == "" && (command.Name == toolJcmd || command.Name == toolAsprof) {
		c.logVerbosef("Command %s requires --args flag", command.Name)
		return "", &InvalidUsageError{message: fmt.Sprintf("The command %q requires the --args flag to be set. Use 'cf java %s --help' for usage information.", command.Name, command.Name)}
	}
	if options.Full && !command.SupportFullOption {
		c.logVerbosef("Command %s does not support --full flag", command.Name)
		return "", &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", "full", command.Name)}
	}
	if argumentLen == 1 {
		return "", &InvalidUsageError{message: "No application name provided"}
	} else if argumentLen > 2 && !command.AcceptsTrailingArgs {
		return "", &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	// Validate --local-dir exists (catches errors early, including during dry-run)
	if options.LocalDir != "" && (command.GenerateFiles || command.GenerateArbitraryFiles) {
		if _, statErr := os.Stat(localDir); os.IsNotExist(statErr) {
			return "", &InvalidUsageError{message: fmt.Sprintf("Error: --local-dir %q does not exist", localDir)}
		}
	}

	applicationName := arguments[1]
	c.logVerbosef("Application name: %s", applicationName)

	cfSSHArguments := []string{cmdSSH, applicationName}
	if options.AppInstanceIndex >= 0 {
		cfSSHArguments = append(cfSSHArguments, "--app-instance-index", strconv.Itoa(options.AppInstanceIndex))
	}
	if options.AppInstanceIndex < -1 {
		// indexes can't be negative (except -1 for default), so fail with an error
		return "", &InvalidUsageError{message: fmt.Sprintf("Invalid application instance index %d, must be >= 0", options.AppInstanceIndex)}
	}

	c.logVerbosef("CF SSH arguments: %v", cfSSHArguments)

	if !options.DryRun {
		supported, err := utils.CheckRequiredTools(applicationName)

		if err != nil || !supported {
			return "required tools checking failed", err
		}

		c.logVerbosef("Required tools check passed")
	}

	if command.IsLocal {
		c.logVerbosef("Executing local command: %s", command.Name)
		jstallArgs := options.Args
		switch {
		case command.SSHCommand == "@ARGS":
			// Generic jstall passthrough: default to 'status all' if no --args provided
			if options.Args == "" {
				jstallArgs = "status all"
			}
		case command.AcceptsTrailingArgs:
			// Commands like record-status: trailing positional arg is output file, --args are extra flags
			// J-09: Reject more than one trailing positional argument
			if argumentLen > 3 {
				return "", &InvalidUsageError{message: fmt.Sprintf("%s accepts at most one trailing argument (output file), got %d", command.Name, argumentLen-2)}
			}
			trailingArg := ""
			if argumentLen > 2 {
				trailingArg = arguments[2]
				// J-10: Reject empty trailing argument
				if strings.TrimSpace(trailingArg) == "" {
					return "", &InvalidUsageError{message: fmt.Sprintf("%s trailing argument must not be empty", command.Name)}
				}
			}
			if trailingArg == "" {
				trailingArg = applicationName + "-status.zip"
			}
			jstallArgs = strings.ReplaceAll(command.SSHCommand, "@ARGS", trailingArg)
			if options.Args != "" {
				jstallArgs += " " + options.Args
			}
		default:
			// Templated commands like status: replace @ARGS with --args value (may be empty)
			jstallArgs = strings.ReplaceAll(command.SSHCommand, "@ARGS", options.Args)
			// Trim trailing whitespace from empty @ARGS substitution
			jstallArgs = strings.TrimRight(jstallArgs, " ")
		}
		if options.Full {
			// J-12: Only add --full if not already present in args to avoid duplication
			if !strings.Contains(jstallArgs, "--full") {
				jstallArgs += " --full"
				jstallArgs = strings.TrimLeft(jstallArgs, " ")
			}
		}
		return c.executeJstall(applicationName, jstallArgs, options.AppInstanceIndex, options.DryRun)
	}

	if !options.DryRun {
		if err := c.checkSSHConnectivity(applicationName, options.AppInstanceIndex); err != nil {
			return "", err
		}
	}

	remoteCommandTokens := []string{JavaDetectionCommand}

	c.logVerbosef("Building remote command tokens")
	c.logVerbosef("Java detection command: %s", JavaDetectionCommand)

	for _, requiredTool := range command.RequiredTools {
		c.logVerbosef("Setting up required tool: %s", requiredTool)
		uppercase := strings.ToUpper(requiredTool)
		toolCommand := fmt.Sprintf(`%[1]s_TOOL_PATH=$(find -executable -name %[2]s | head -1 | tr -d [:space:]); if [ -z "$%[1]s_TOOL_PATH" ]; then echo "%[2]s not found"; exit 1; fi; %[1]s_COMMAND=$(realpath "$%[1]s_TOOL_PATH")`, uppercase, requiredTool)
		if requiredTool == toolJcmd {
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
	fileExt := command.FileExtension
	var err error
	if command.Name == cmdHeapDump && options.Compress {
		// Only set .hprof.gz for jvmmon path (explicit compress); jmap always writes .hprof on remote
		fileExt = extHprof
	}

	// Initialize fspath and fileName for commands that need them
	if command.GenerateFiles || command.NeedsFileName || command.GenerateArbitraryFiles {
		c.logVerbosef("Command requires file generation")
		if options.DryRun {
			// In dry-run mode, use user-supplied path or /tmp without SSH validation
			if remoteDir != "" {
				fspath = remoteDir
			} else {
				fspath = "/tmp"
			}
			c.logVerbosef("Dry-run mode: using path %s without validation", fspath)
		} else {
			fspath, err = utils.GetAvailablePath(applicationName, remoteDir)
			if err != nil {
				return "", fmt.Errorf("failed to get available path: %w", err)
			}
			if fspath == "" {
				return "", fmt.Errorf("no available path found for file generation")
			}
		}
		c.logVerbosef("Available path: %s", fspath)

		if command.GenerateArbitraryFiles {
			fspath = fspath + "/" + command.GenerateArbitraryFilesFolderName
			c.logVerbosef("Updated path for arbitrary files: %s", fspath)
		}

		namePart := ""
		if command.FileNamePart != "" {
			namePart = "-" + command.FileNamePart
		}
		fileName = fspath + "/" + applicationName + namePart + "-" + utils.GenerateUUID() + fileExt
		staticFileName = fspath + "/" + applicationName + namePart + fileExt
		c.logVerbosef("Generated filename: %s", fileName)
		c.logVerbosef("Generated static filename without UUID: %s", staticFileName)
	}

	commandText := command.SSHCommand
	// Expand @COMPRESS_FLAG for jvmmon path in heap-dump (jmap uses shell-level gz probe)
	if command.Name == cmdHeapDump {
		if options.Compress {
			commandText = strings.ReplaceAll(commandText, "@COMPRESS_FLAG", "1")
		} else {
			commandText = strings.ReplaceAll(commandText, "@COMPRESS_FLAG", "")
		}
	}
	// Perform variable replacements directly in Go code
	var err2 error
	commandText, err2 = c.replaceVariables(commandText, applicationName, fspath, fileName, staticFileName, options.Args)
	if err2 != nil {
		return "", fmt.Errorf("variable replacement failed: %w", err2)
	}

	// For arbitrary files commands, insert mkdir and cd before the main command
	if command.GenerateArbitraryFiles {
		remoteCommandTokens = append(remoteCommandTokens, "mkdir -p \""+fspath+"\"", "cd \""+fspath+"\"", commandText)
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
		escapedCommand := strings.ReplaceAll(remoteCommand, "'", "'\\''")
		cfSSHArguments = append(cfSSHArguments, "'"+escapedCommand+"'")
		if command.Name == cmdHeapDump && options.Open {
			ext := extHprof
			if options.Compress {
				ext = extHprofGz
			}
			fmt.Printf("Would open: %s\n", buildOpenURL(options.OpenURL, 0, "TOKEN"+ext))
		}
		return "cf " + strings.Join(cfSSHArguments, " "), nil
	}

	fullCommand := append([]string{}, cfSSHArguments...)
	fullCommand = append(fullCommand, remoteCommand)
	c.logVerbosef("Executing command: %v", fullCommand)
	cmd := exec.Command("cf", fullCommand...)
	outputBytes, err := cmd.CombinedOutput()
	output := strings.TrimRight(string(outputBytes), "\n")
	if err != nil {
		if isSSHConnectivityError(output, err) {
			return "", fmt.Errorf("%s", wrapSSHError(applicationName, output, err))
		}
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
		switch fileExt {
		case extHprof:
			c.logVerbosef("Finding heap dump file")
			finalFile, err = utils.FindHeapDumpFile(cfSSHArguments, fileName, fspath, applicationName+"-"+command.FileNamePart)
		case extHprofGz:
			c.logVerbosef("Finding compressed heap dump file")
			finalFile, err = utils.FindHeapDumpGzFile(cfSSHArguments, fileName, fspath, applicationName+"-"+command.FileNamePart)
		case ".jfr":
			c.logVerbosef("Finding JFR file")
			finalFile, err = utils.FindJFRFile(cfSSHArguments, fileName, fspath, applicationName+"-"+command.FileNamePart)
		default:
			return "", &InvalidUsageError{message: fmt.Sprintf("Unsupported file extension %q", fileExt)}
		}
		if err == nil && finalFile != "" {
			fileName = finalFile
			c.logVerbosef("Found file: %s", finalFile)
			fmt.Println("Successfully created " + command.FileLabel + " in application container at: " + fileName)
		} else if !noDownload {
			c.logVerbosef("Failed to find file, error: %v", err)
			fmt.Println("Failed to find " + command.FileLabel + " in application container")
			if err == nil {
				err = fmt.Errorf("failed to find %s in application container", command.FileLabel)
			}
			return "", err
		}

		if noDownload {
			fmt.Println("No download requested, skipping file download")
			return output, nil
		}

		// For heap-dump via jmap: probe whether the remote file is gzip-compressed.
		// jmap writes a .hprof filename but may fill it with gzip content when gz=1 is supported.
		localFileExt := fileExt
		remoteIsGz := false
		if command.Name == cmdHeapDump && fileExt == extHprof {
			remoteIsGz, _ = utils.ProbeRemoteFileGzip(cfSSHArguments, fileName)
			c.logVerbosef("Remote file is gzip-compressed: %t", remoteIsGz)
			switch {
			case remoteIsGz && options.Compress:
				// User asked for .hprof.gz locally → keep compressed
				localFileExt = extHprofGz
			case !remoteIsGz && options.Compress:
				fmt.Fprintf(os.Stderr, "Warning: remote jmap does not support gz compression (JDK 17+ required); downloading uncompressed\n")
			}
		}

		localFileFullPath := localDir + "/" + applicationName + "-" + command.FileNamePart + "-" + utils.GenerateUUID() + localFileExt
		c.logVerbosef("Downloading file to: %s", localFileFullPath)

		redactingHeapDump := command.Name == cmdHeapDump && (options.Redact || options.RedactComplete)
		if command.Name == cmdHeapDump && remoteIsGz && localFileExt == extHprof && !redactingHeapDump {
			fmt.Println("Note: remote jmap used gz compression; decompressing during transfer...")
		}

		if redactingHeapDump {
			mode := "lean"
			if options.RedactComplete {
				mode = "complete"
			}
			redactBin, rerr := ensureHprofRedact()
			if rerr != nil {
				return "", fmt.Errorf("hprof-redact unavailable: %w", rerr)
			}

			var reader io.ReadCloser
			var waitRemote func() error
			reader, waitRemote, err = utils.StreamOverCat(cfSSHArguments, fileName)
			if err != nil {
				return "", err
			}
			if remoteIsGz {
				// Remote jmap used gz=1; decompress before passing to hprof-redact
				// which expects uncompressed HPROF format.
				gz, gzErr := gzip.NewReader(reader)
				if gzErr != nil {
					_ = reader.Close()
					_ = waitRemote()
					return "", fmt.Errorf("gzip header error on remote heap dump: %w", gzErr)
				}
				reader = gz
			}

			finalPath, rerr := pipeHeapDumpThroughRedact(redactBin, reader, localFileFullPath, mode, options.RedactKeepOnError)
			closeErr := reader.Close()
			waitErr := waitRemote()
			if rerr != nil {
				return "", combineHeapDumpStreamErrors(rerr, closeErr, waitErr)
			}
			if combinedErr := combineHeapDumpStreamErrors(nil, closeErr, waitErr); combinedErr != nil {
				return "", combinedErr
			}

			c.logVerbosef("Redacted heap dump stream completed successfully")
			fmt.Println("Redacted heap dump saved to: " + finalPath)

			if command.Name == cmdHeapDump && options.Open {
				port, urlFile, done, serveErr := serveFileOnce(finalPath, 10*time.Minute)
				if serveErr != nil {
					return "", fmt.Errorf("could not start local file server: %w", serveErr)
				}
				openURL := buildOpenURL(options.OpenURL, port, urlFile)
				fmt.Printf("Opening heap dump in browser: %s\n", openURL)
				openBrowser(openURL)
				<-done
			}
		} else {
			if command.Name == cmdHeapDump && remoteIsGz && localFileExt == extHprof {
				// Transparent decompression: stream gz from remote, write plain .hprof locally
				err = utils.CopyOverCatGunzip(cfSSHArguments, fileName, localFileFullPath)
			} else {
				err = utils.CopyOverCat(cfSSHArguments, fileName, localFileFullPath)
			}

			if err == nil {
				c.logVerbosef("File download completed successfully")
				fmt.Println(utils.ToSentenceCase(command.FileLabel) + " file saved to: " + localFileFullPath)

				finalLocalPath := localFileFullPath
				if command.Name == cmdHeapDump && options.Open {
					port, urlFile, done, serveErr := serveFileOnce(finalLocalPath, 10*time.Minute)
					if serveErr != nil {
						return "", fmt.Errorf("could not start local file server: %w", serveErr)
					}
					openURL := buildOpenURL(options.OpenURL, port, urlFile)
					fmt.Printf("Opening heap dump in browser: %s\n", openURL)
					openBrowser(openURL)
					<-done
				}
			} else {
				c.logVerbosef("File download failed: %v", err)
				fmt.Fprintf(os.Stderr, "The %s was created successfully in the container at: %s\n", command.FileLabel, fileName)
				fmt.Fprintf(os.Stderr, "However, downloading to local failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "The remote file is still available. Retry with:\n")
				fmt.Fprintf(os.Stderr, "  cf ssh %s -c 'cat %s' > %s\n", applicationName, fileName, localFileFullPath)
				return "", fmt.Errorf("download failed (remote file intact): %w", err)
			}
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
		var annotations []string
		if command.IsLocal {
			annotations = append(annotations, "requires Java 17+ locally")
		}
		if command.OnlyOnRecentSapMachine {
			annotations = append(annotations, "recent SapMachine only")
		}
		var supportedFlags []string
		if command.HasMiscArgs() {
			supportedFlags = append(supportedFlags, "--args")
		}
		if command.SupportFullOption {
			supportedFlags = append(supportedFlags, "--full")
		}
		if len(supportedFlags) > 0 {
			annotations = append(annotations, "supports "+strings.Join(supportedFlags, ", "))
		}
		if len(annotations) > 0 {
			usageText += " (" + strings.Join(annotations, ", ") + ")"
		}
		// Wrap the description with proper indentation
		wrappedDescription := utils.WrapTextWithPrefix(command.Description, "        ", 80, 0)
		usageText += "\n" + wrappedDescription
	}
	return plugin.PluginMetadata{
		Name: cmdJava,
		Version: plugin.VersionType{
			Major: 4,
			Minor: 0,
			Build: 2,
		},
		MinCliVersion: plugin.VersionType{
			Major: 4,
			Minor: 0,
			Build: 2,
		},
		Commands: []plugin.Command{
			{
				Name:     cmdJava,
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
