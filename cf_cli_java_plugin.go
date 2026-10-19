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
	cmdVmVitals           = "vm-vitals"
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
	OpenURLExplicit   bool // true only when --open-url was explicitly passed
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
		OpenURLExplicit: commandFlags.IsSet(flagOpenURL),
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

func (c *JavaPlugin) buildJstallArgs(command *Command, applicationName string, arguments []string, options *Options) (string, error) {
	argumentLen := len(arguments)
	jstallArgs := options.Args

	switch {
	case command.SSHCommand == "@ARGS":
		// Generic jstall passthrough: default to 'status all' if no --args provided.
		if options.Args == "" {
			jstallArgs = "status all"
		}
	case command.AcceptsTrailingArgs:
		// Commands like record-status: the trailing positional argument is the output file,
		// while --args contains any extra JStall flags.
		if argumentLen > 3 {
			return "", &InvalidUsageError{message: fmt.Sprintf("%s accepts at most one trailing argument (output file), got %d", command.Name, argumentLen-2)}
		}

		trailingArg := ""
		if argumentLen > 2 {
			trailingArg = arguments[2]
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
		// Templated commands like status: replace @ARGS with the provided value.
		jstallArgs = strings.ReplaceAll(command.SSHCommand, "@ARGS", options.Args)
		jstallArgs = strings.TrimRight(jstallArgs, " ")
	}

	if options.Full && !strings.Contains(jstallArgs, "--full") {
		// Only add --full when it is not already present to avoid duplication.
		jstallArgs += " --full"
		jstallArgs = strings.TrimLeft(jstallArgs, " ")
	}

	return jstallArgs, nil
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
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=@FSPATH\ndump heap' > @FSPATH/setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid ${_pid} -cmd "@FSPATH/setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
rm -f @FSPATH/setHeapDumpOnDemandPath.sh
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
		Name:          cmdVmVitals,
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

// commandContext bundles everything that is resolved once per `cf java` invocation
// and is then shared by the remote-execution, download and cleanup phases.
type commandContext struct {
	command         *Command
	options         *Options
	applicationName string
	// cfSSHArguments is the `cf ssh` prefix *without* a trailing --command. Use
	// sshArgs() to obtain a ready-to-use argument slice.
	cfSSHArguments []string
	remoteDir      string
	localDir       string

	// noDownload may be stricter than options.NoDownload: `asprof start ...` only
	// arms the profiler, so there is no file to fetch yet.
	noDownload bool
	// keepAfterDownload reflects the effective noDownload: nothing fetched means
	// nothing may be deleted in the container.
	keepAfterDownload bool

	// Remote filesystem layout, populated by prepareRemotePaths.
	fspath         string
	fileExt        string
	fileName       string
	staticFileName string
}

// sshArgs returns a fresh `cf ssh` argument slice terminated by --command, ready for
// a remote command to be appended. Every call allocates, because the utils helpers
// append to the slice they are given; sharing one backing array between them would
// let overlapping invocations overwrite each other's remote command.
func (ctx *commandContext) sshArgs() []string {
	args := make([]string, 0, len(ctx.cfSSHArguments)+2)
	args = append(args, ctx.cfSSHArguments...)
	return append(args, "--command")
}

// handleCLILifecycle deals with the lifecycle messages the CF CLI sends and rejects
// invocations that are not `cf java ...`. handled=true means execute must return
// immediately with the returned error.
func handleCLILifecycle(args []string) (handled bool, err error) {
	if len(args) == 0 {
		return true, &InvalidUsageError{message: "No command provided"}
	}
	switch args[0] {
	case "CLI-MESSAGE-UNINSTALL":
		// Nothing to uninstall, we keep no local state
		return true, nil
	case cmdJava:
		return false, nil
	default:
		return true, &InvalidUsageError{message: fmt.Sprintf("Unexpected command Name '%s' (expected : 'java')", args[0])}
	}
}

// resolveLocalDir defaults to the current directory, expands a leading `~` and
// rejects path traversal sequences.
func resolveLocalDir(localDir string) (string, error) {
	if localDir == "" {
		return ".", nil
	}
	if localDir == "~" || strings.HasPrefix(localDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			localDir = home + localDir[1:]
		}
	}
	for _, part := range strings.Split(filepath.Clean(localDir), string(filepath.Separator)) {
		if part == ".." {
			return "", &InvalidUsageError{message: "Error: --local-dir must not contain path traversal sequences (..)"}
		}
	}
	return localDir, nil
}

func showPluginHelp() error {
	if err := exec.Command("cf", "help", cmdJava).Run(); err != nil {
		return fmt.Errorf("failed to show help: %w", err)
	}
	return nil
}

// findCommand resolves a subcommand by name, or returns a helpful usage error.
func findCommand(cliConnection plugin.CliConnection, arguments []string) (*Command, error) {
	name := strings.ToLower(arguments[0])
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i], nil
		}
	}
	return nil, unknownCommandError(cliConnection, arguments)
}

func unknownCommandError(cliConnection plugin.CliConnection, arguments []string) error {
	commandName := arguments[0]

	// Detect swapped order: cf java MY-APP heap-dump instead of cf java heap-dump MY-APP
	if len(arguments) >= 2 && cliConnection != nil {
		secondArg := strings.ToLower(arguments[1])
		for _, cmd := range commands {
			if cmd.Name == secondArg {
				// Confirm the first arg looks like an app name via CF API.
				if _, appErr := cliConnection.GetApp(commandName); appErr == nil {
					return &InvalidUsageError{message: fmt.Sprintf("Did you mean: cf java %s %s? (app and command appear to be swapped)", secondArg, commandName)}
				}
				break
			}
		}
	}

	avCommands := make([]string, 0, len(commands))
	for _, cmd := range commands {
		avCommands = append(avCommands, cmd.Name)
	}
	matches := utils.FuzzySearch(strings.ToLower(commandName), avCommands, 3)
	return &InvalidUsageError{message: fmt.Sprintf("Unrecognized command %q, did you mean: %s?", commandName, utils.JoinWithOr(matches))}
}

func unsupportedFlagError(flag, commandName string) error {
	return &InvalidUsageError{message: fmt.Sprintf("The flag %q is not supported for %s", flag, commandName)}
}

// warnAboutHeapDumpOnlyFlags warns about heap-dump-only flags used on other commands.
func warnAboutHeapDumpOnlyFlags(command *Command, options *Options) {
	if command.Name == cmdHeapDump {
		return
	}
	heapOnlyFlags := []struct {
		set  bool
		name string
	}{
		{options.Redact, flagRedact},
		{options.RedactComplete, flagRedactComplete},
		{options.Compress, flagCompress},
		{options.Open, flagOpen},
		{options.OpenURLExplicit, flagOpenURL},
	}
	for _, f := range heapOnlyFlags {
		if f.set {
			fmt.Fprintf(os.Stderr, "Warning: flag '--%s' is only supported by heap-dump and has no effect here.\n", f.name)
		}
	}
}

func hasHelpFlag(arguments []string) bool {
	for _, arg := range arguments {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

// validateInvocation rejects unsupported flag/argument combinations. The order of
// the checks is significant: it decides which error a user sees when several apply.
func validateInvocation(command *Command, options *Options, arguments []string, localDir string) error {
	if !command.GenerateFiles && !command.GenerateArbitraryFiles {
		switch {
		case options.ContainerDir != "":
			return unsupportedFlagError(flagContainerDir, command.Name)
		case options.LocalDir != "":
			return unsupportedFlagError(flagLocalDir, command.Name)
		case options.Keep:
			return unsupportedFlagError(flagKeep, command.Name)
		case options.NoDownload:
			return unsupportedFlagError(flagNoDownload, command.Name)
		}
	}

	if !command.HasMiscArgs() && options.Args != "" {
		return unsupportedFlagError("args", command.Name)
	}
	if options.Args != "" && strings.TrimSpace(options.Args) == "" {
		return &InvalidUsageError{message: "Error: --args must not be empty or whitespace-only"}
	}
	if command.HasMiscArgs() && options.Args == "" && (command.Name == toolJcmd || command.Name == toolAsprof) {
		return &InvalidUsageError{message: fmt.Sprintf("The command %q requires the --args flag to be set. Use 'cf java %s --help' for usage information.", command.Name, command.Name)}
	}
	if options.Full && !command.SupportFullOption {
		return unsupportedFlagError("full", command.Name)
	}

	switch {
	case len(arguments) == 1:
		return &InvalidUsageError{message: "No application name provided"}
	case len(arguments) > 2 && !command.AcceptsTrailingArgs:
		return &InvalidUsageError{message: fmt.Sprintf("Too many arguments provided: %v", strings.Join(arguments[2:], ", "))}
	}

	// Validate --local-dir exists (catches errors early, including during dry-run)
	if options.LocalDir != "" && (command.GenerateFiles || command.GenerateArbitraryFiles) {
		if _, statErr := os.Stat(localDir); os.IsNotExist(statErr) {
			return &InvalidUsageError{message: fmt.Sprintf("Error: --local-dir %q does not exist", localDir)}
		}
	}

	// Indexes can't be negative (except -1 for "not set"), so fail with an error.
	if options.AppInstanceIndex < -1 {
		return &InvalidUsageError{message: fmt.Sprintf("Invalid application instance index %d, must be >= 0", options.AppInstanceIndex)}
	}
	return nil
}

// resolveDownloadBehavior decides whether the download and cleanup phases run.
func resolveDownloadBehavior(command *Command, options *Options) (noDownload, keepAfterDownload bool) {
	noDownload = options.NoDownload

	// `asprof start ...` only arms the profiler; the recording is written later by
	// `asprof stop`, so there is nothing to fetch yet.
	if command.Name == toolAsprof && isAsprofStartArgs(options.Args) {
		noDownload = true
	}

	// If nothing is downloaded, the container copy is the only copy: never delete it.
	keepAfterDownload = options.Keep || noDownload
	return noDownload, keepAfterDownload
}

// isAsprofStartArgs reports whether --args invokes async-profiler's `start`
// subcommand, i.e. "start" on its own or "start <options...>".
func isAsprofStartArgs(args string) bool {
	trimmed := strings.TrimSpace(args)
	return trimmed == "start" || strings.HasPrefix(trimmed, "start ")
}

func buildSSHArguments(applicationName string, appInstanceIndex int) []string {
	args := []string{cmdSSH, applicationName}
	if appInstanceIndex >= 0 {
		args = append(args, "--app-instance-index", strconv.Itoa(appInstanceIndex))
	}
	return args
}

// prepareRemotePaths resolves the writable directory in the container and the file
// names the command will produce there.
func (c *JavaPlugin) prepareRemotePaths(ctx *commandContext) error {
	ctx.fspath = ctx.remoteDir
	ctx.fileExt = ctx.command.FileExtension
	if ctx.command.Name == cmdHeapDump && ctx.options.Compress {
		// jmap always writes a .hprof file on the container; only jvmmon may produce a .hprof.gz file
		ctx.fileExt = extHprof
	}

	// Fail before touching the container: the extension is static command metadata,
	// so an unsupported one is a plugin bug and must not depend on --no-download.
	if ctx.command.GenerateFiles && !isLocatableFileExtension(ctx.fileExt) {
		return &InvalidUsageError{message: fmt.Sprintf("Unsupported file extension %q", ctx.fileExt)}
	}

	if !ctx.command.GenerateFiles && !ctx.command.NeedsFileName && !ctx.command.GenerateArbitraryFiles {
		return nil
	}

	if ctx.options.DryRun {
		// Dry-run cannot discover volume mounts without contacting CF; use the
		// supplied container directory or an offline placeholder.
		if ctx.fspath == "" {
			ctx.fspath = "/tmp"
		}
	} else {
		path, err := utils.GetAvailablePath(ctx.applicationName, ctx.remoteDir)
		if err != nil {
			return fmt.Errorf("failed to get available path: %w", err)
		}
		if path == "" {
			return fmt.Errorf("no available path found for file generation")
		}
		ctx.fspath = path
	}

	if ctx.command.GenerateArbitraryFiles {
		ctx.fspath += "/" + ctx.command.GenerateArbitraryFilesFolderName
	}

	namePart := ""
	if ctx.command.FileNamePart != "" {
		namePart = "-" + ctx.command.FileNamePart
	}
	base := ctx.fspath + "/" + ctx.applicationName + namePart
	ctx.fileName = base + "-" + utils.GenerateUUID() + ctx.fileExt
	ctx.staticFileName = base + ctx.fileExt
	c.logVerbosef("Remote path: %s, file: %s", ctx.fspath, ctx.fileName)
	return nil
}

// toolResolutionTokens emits the shell snippets that locate the required JDK tools
// inside the container and export them as $<TOOL>_COMMAND.
func toolResolutionTokens(requiredTools []string) []string {
	var tokens []string
	for _, tool := range requiredTools {
		upper := strings.ToUpper(tool)
		tokens = append(tokens, fmt.Sprintf(`%[1]s_TOOL_PATH=$(find -executable -name %[2]s | head -1 | tr -d [:space:]); if [ -z "$%[1]s_TOOL_PATH" ]; then echo "%[2]s not found"; exit 1; fi; %[1]s_COMMAND=$(realpath "$%[1]s_TOOL_PATH")`, upper, tool))
		if tool == toolJcmd {
			// Prefer `asprof jcmd` over plain `jcmd` when async-profiler is present.
			tokens = append(tokens, `ASPROF_COMMAND=$(realpath $(find -executable -name asprof | head -1 | tr -d [:space:])); if [ -n "${ASPROF_COMMAND}" ]; then JCMD_COMMAND="${ASPROF_COMMAND} jcmd"; fi`)
		}
	}
	return tokens
}

// buildRemoteCommand assembles the single shell command that runs in the container.
func (c *JavaPlugin) buildRemoteCommand(ctx *commandContext) (string, error) {
	tokens := []string{JavaDetectionCommand}
	tokens = append(tokens, toolResolutionTokens(ctx.command.RequiredTools)...)

	commandText := ctx.command.SSHCommand
	// Expand @COMPRESS_FLAG for jvmmon path in heap-dump (jmap uses shell-level gz probe)
	if ctx.command.Name == cmdHeapDump {
		compressFlag := ""
		if ctx.options.Compress {
			compressFlag = "1"
		}
		commandText = strings.ReplaceAll(commandText, "@COMPRESS_FLAG", compressFlag)
	}

	commandText, err := c.replaceVariables(commandText, ctx.applicationName, ctx.fspath, ctx.fileName, ctx.staticFileName, ctx.options.Args)
	if err != nil {
		return "", fmt.Errorf("variable replacement failed: %w", err)
	}

	if ctx.command.GenerateArbitraryFiles {
		// Arbitrary-file commands run inside their own subfolder so the plugin knows
		// exactly which files to collect afterwards.
		tokens = append(tokens, `mkdir -p "`+ctx.fspath+`"`, `cd "`+ctx.fspath+`"`, commandText)
	} else {
		tokens = append(tokens, commandText)
	}

	remoteCommand := strings.Join(tokens, "; ")
	c.logVerbosef("Final remote command: %s", remoteCommand)
	return remoteCommand, nil
}

// dryRunCommandLine renders the `cf ssh` invocation that would have been executed.
func dryRunCommandLine(ctx *commandContext, remoteCommand string) string {
	// The remote command is wrapped in single quotes so that copy-pasting it does not
	// make the local shell execute it.
	escaped := strings.ReplaceAll(remoteCommand, "'", `'\''`)
	args := append(ctx.sshArgs(), "'"+escaped+"'")
	return "cf " + strings.Join(args, " ")
}

// runRemoteCommand executes the command over `cf ssh` and maps failures onto
// user-facing error messages.
func (c *JavaPlugin) runRemoteCommand(ctx *commandContext, remoteCommand string) (string, error) {
	fullCommand := append(ctx.sshArgs(), remoteCommand)
	c.logVerbosef("Executing command: %v", fullCommand)

	outputBytes, err := exec.Command("cf", fullCommand...).CombinedOutput()
	output := strings.TrimRight(string(outputBytes), "\n")
	if err == nil {
		return output, nil
	}

	switch {
	case isSSHConnectivityError(output, err):
		return "", fmt.Errorf("%s", wrapSSHError(ctx.applicationName, output, err))
	case err.Error() == "unexpected EOF":
		return "", fmt.Errorf("Command failed")
	case len(output) == 0:
		return "", fmt.Errorf("Command execution failed: %w", err)
	default:
		return "", fmt.Errorf("Command execution failed: %w\nOutput: %s", err, output)
	}
}

// isLocatableFileExtension reports whether collectGeneratedFile knows how to find a
// file with this extension in the container.
func isLocatableFileExtension(ext string) bool {
	switch ext {
	case extHprof, extHprofGz, extJFR:
		return true
	default:
		return false
	}
}

// collectGeneratedFile locates the file the command produced in the container and
// updates ctx.fileName with the real name. prepareRemotePaths has already rejected
// extensions this function cannot handle.
func (c *JavaPlugin) collectGeneratedFile(ctx *commandContext) error {
	var (
		finalFile string
		err       error
	)
	namePrefix := ctx.applicationName + "-" + ctx.command.FileNamePart

	switch ctx.fileExt {
	case extHprofGz:
		finalFile, err = utils.FindHeapDumpGzFile(ctx.sshArgs(), ctx.fileName, ctx.fspath, namePrefix)
	case extJFR:
		finalFile, err = utils.FindJFRFile(ctx.sshArgs(), ctx.fileName, ctx.fspath, namePrefix)
	default:
		finalFile, err = utils.FindHeapDumpFile(ctx.sshArgs(), ctx.fileName, ctx.fspath, namePrefix)
	}

	if err == nil && finalFile != "" {
		ctx.fileName = finalFile
		fmt.Println("Successfully created " + ctx.command.FileLabel + " in application container at: " + ctx.fileName)
		return nil
	}
	if ctx.noDownload {
		// Nothing is going to be fetched, so a missing file is not fatal here.
		return nil
	}

	fmt.Println("Failed to find " + ctx.command.FileLabel + " in application container")
	if err == nil {
		err = fmt.Errorf("failed to find %s in application container", ctx.command.FileLabel)
	}
	return err
}

// downloadGeneratedFile transfers the generated file to the local machine, applying
// gzip handling and redaction where requested.
func (c *JavaPlugin) downloadGeneratedFile(ctx *commandContext) error {
	isHeapDump := ctx.command.Name == cmdHeapDump
	localFileExt := ctx.fileExt
	remoteIsGz := false

	// For heap-dump via jmap: probe whether the remote file is gzip-compressed.
	// jmap writes a .hprof filename but may fill it with gzip content when gz=1 is supported.
	if isHeapDump && ctx.fileExt == extHprof {
		remoteIsGz, _ = utils.ProbeRemoteFileGzip(ctx.sshArgs(), ctx.fileName)
		c.logVerbosef("Remote file is gzip-compressed: %t", remoteIsGz)
		switch {
		case remoteIsGz && ctx.options.Compress:
			// User asked for .hprof.gz locally -> keep compressed
			localFileExt = extHprofGz
		case !remoteIsGz && ctx.options.Compress:
			fmt.Fprintf(os.Stderr, "Warning: remote jmap does not support gz compression (JDK 17+ required); downloading uncompressed\n")
		}
	}

	localPath := ctx.localDir + "/" + ctx.applicationName + "-" + ctx.command.FileNamePart + "-" + utils.GenerateUUID() + localFileExt
	c.logVerbosef("Downloading file to: %s", localPath)

	redacting := isHeapDump && (ctx.options.Redact || ctx.options.RedactComplete)
	needsGunzip := isHeapDump && remoteIsGz && localFileExt == extHprof

	if needsGunzip && !redacting {
		fmt.Println("Note: remote jmap used gz compression; decompressing during transfer...")
	}
	if redacting {
		return c.downloadRedactedHeapDump(ctx, localPath, remoteIsGz)
	}
	return c.downloadPlainFile(ctx, localPath, needsGunzip)
}

// downloadRedactedHeapDump streams the heap dump through hprof-redact instead of
// writing the raw dump to disk.
func (c *JavaPlugin) downloadRedactedHeapDump(ctx *commandContext, localPath string, remoteIsGz bool) error {
	mode := "lean"
	if ctx.options.RedactComplete {
		mode = "complete"
	}
	redactBin, err := ensureHprofRedact()
	if err != nil {
		return fmt.Errorf("hprof-redact unavailable: %w", err)
	}

	reader, waitRemote, err := utils.StreamOverCat(ctx.sshArgs(), ctx.fileName)
	if err != nil {
		return err
	}
	stream := reader
	if remoteIsGz {
		// Remote jmap used gz=1; decompress before passing to hprof-redact
		// which expects uncompressed HPROF format.
		gz, gzErr := gzip.NewReader(stream)
		if gzErr != nil {
			_ = stream.Close()
			_ = waitRemote()
			return fmt.Errorf("gzip header error on remote heap dump: %w", gzErr)
		}
		stream = gz
	}

	finalPath, redactErr := pipeHeapDumpThroughRedact(redactBin, stream, localPath, mode, ctx.options.RedactKeepOnError)
	closeErr := stream.Close()
	waitErr := waitRemote()
	if redactErr != nil {
		return combineHeapDumpStreamErrors(redactErr, closeErr, waitErr)
	}
	if combinedErr := combineHeapDumpStreamErrors(nil, closeErr, waitErr); combinedErr != nil {
		return combinedErr
	}

	c.logVerbosef("Redacted heap dump stream completed successfully")
	fmt.Println("Redacted heap dump saved to: " + finalPath)
	return c.openHeapDumpIfRequested(ctx, finalPath)
}

// downloadPlainFile copies the remote file verbatim, optionally gunzipping in flight.
func (c *JavaPlugin) downloadPlainFile(ctx *commandContext, localPath string, gunzip bool) error {
	var err error
	if gunzip {
		// Transparent decompression: stream gz from remote, write plain .hprof locally
		err = utils.CopyOverCatGunzip(ctx.sshArgs(), ctx.fileName, localPath)
	} else {
		err = utils.CopyOverCat(ctx.sshArgs(), ctx.fileName, localPath)
	}
	if err != nil {
		c.logVerbosef("File download failed: %v", err)
		fmt.Fprintf(os.Stderr, "The %s was created successfully in the container at: %s\n", ctx.command.FileLabel, ctx.fileName)
		fmt.Fprintf(os.Stderr, "However, downloading to local failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "The remote file is still available. Retry with:\n")
		fmt.Fprintf(os.Stderr, "  cf ssh %s -c 'cat %s' > %s\n", ctx.applicationName, ctx.fileName, localPath)
		return fmt.Errorf("download failed (remote file intact): %w", err)
	}

	fmt.Println(utils.ToSentenceCase(ctx.command.FileLabel) + " file saved to: " + localPath)
	return c.openHeapDumpIfRequested(ctx, localPath)
}

// openHeapDumpIfRequested serves the dump once over localhost and opens the analyzer.
func (c *JavaPlugin) openHeapDumpIfRequested(ctx *commandContext, path string) error {
	if ctx.command.Name != cmdHeapDump || !ctx.options.Open {
		return nil
	}
	port, urlFile, done, err := serveFileOnce(path, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("could not start local file server: %w", err)
	}
	openURL := buildOpenURL(ctx.options.OpenURL, port, urlFile)
	fmt.Printf("Opening heap dump in browser: %s\n", openURL)
	openBrowser(openURL)
	<-done
	return nil
}

// downloadArbitraryFiles fetches every file the command left in its working folder.
func (c *JavaPlugin) downloadArbitraryFiles(ctx *commandContext) error {
	files, err := utils.ListFiles(ctx.sshArgs(), ctx.fspath)
	if err != nil {
		return err
	}
	c.logVerbosef("Found %d files to download in %s", len(files), ctx.fspath)
	if len(files) == 0 {
		return nil
	}

	for _, file := range files {
		localPath := ctx.localDir + "/" + file
		if err := utils.CopyOverCat(ctx.sshArgs(), ctx.fspath+"/"+file, localPath); err != nil {
			c.logVerbosef("Failed to download file %s: %v", file, err)
			return err
		}
		fmt.Printf("File %s saved to: %s\n", file, localPath)
	}

	return c.deleteRemoteArtifact(ctx, ctx.fspath, "File folder deleted in application container")
}

// deleteRemoteArtifact removes the file or folder from the container unless the user
// asked to keep it.
func (c *JavaPlugin) deleteRemoteArtifact(ctx *commandContext, path, successMessage string) error {
	if ctx.keepAfterDownload {
		c.logVerbosef("Keeping remote artifact %s as requested", path)
		return nil
	}
	if err := utils.DeleteRemoteFile(ctx.sshArgs(), path); err != nil {
		c.logVerbosef("Failed to delete remote artifact %s: %v", path, err)
		return err
	}
	fmt.Println(successMessage)
	return nil
}

// execute runs one `cf java` invocation as a pipeline:
// lifecycle -> parse -> resolve command -> validate -> dispatch (local or SSH) ->
// run -> download -> clean up.
func (c *JavaPlugin) execute(cliConnection plugin.CliConnection, args []string) (string, error) {
	if handled, err := handleCLILifecycle(args); handled {
		return "", err
	}

	options, arguments, parseErr := c.parseOptions(args[1:])
	if parseErr != nil {
		return "", &InvalidUsageError{message: fmt.Sprintf("Error while parsing command arguments: %v", parseErr)}
	}
	c.logVerbosef("Starting command execution with args: %v", args)

	remoteDir := strings.TrimRight(options.ContainerDir, "/")
	localDir, err := resolveLocalDir(options.LocalDir)
	if err != nil {
		return "", err
	}
	c.logVerbosef("Remote directory: %s, local directory: %s", remoteDir, localDir)

	if len(arguments) < 1 {
		return "", &InvalidUsageError{message: "No command provided"}
	}

	// 'help' is not a real subcommand, it just forwards to `cf help java`.
	if strings.ToLower(arguments[0]) == "help" {
		return "", showPluginHelp()
	}

	command, err := findCommand(cliConnection, arguments)
	if err != nil {
		return "", err
	}
	c.logVerbosef("Found command: %s - %s", command.Name, command.Description)

	warnAboutHeapDumpOnlyFlags(command, options)

	// Only block CF_TRACE for commands that download files (not for read-only commands like vm-version)
	if os.Getenv("CF_TRACE") == "true" && (command.GenerateFiles || command.GenerateArbitraryFiles) {
		return "", errors.New("the environment variable CF_TRACE is set to true. This prevents download of the dump from succeeding")
	}

	// `cf java jstall APP --help` is forwarded straight to jstall's own help.
	if command.Name == "jstall" && hasHelpFlag(arguments) {
		return c.executeJstall("", "--help", 0, false)
	}

	if err := validateInvocation(command, options, arguments, localDir); err != nil {
		return "", err
	}

	applicationName := arguments[1]
	noDownload, keepAfterDownload := resolveDownloadBehavior(command, options)
	ctx := &commandContext{
		command:           command,
		options:           options,
		applicationName:   applicationName,
		cfSSHArguments:    buildSSHArguments(applicationName, options.AppInstanceIndex),
		remoteDir:         remoteDir,
		localDir:          localDir,
		noDownload:        noDownload,
		keepAfterDownload: keepAfterDownload,
	}

	if !options.DryRun {
		supported, err := utils.CheckRequiredTools(applicationName)
		if err != nil || !supported {
			return "required tools checking failed", err
		}
		c.logVerbosef("Required tools check passed")
	}

	// Local commands (JStall) run on this machine and manage their own SSH session.
	if command.IsLocal {
		c.logVerbosef("Executing local command: %s", command.Name)
		jstallArgs, err := c.buildJstallArgs(command, applicationName, arguments, options)
		if err != nil {
			return "", err
		}
		return c.executeJstall(applicationName, jstallArgs, options.AppInstanceIndex, options.DryRun)
	}

	// Everything below runs inside the application container via `cf ssh`.
	if !options.DryRun {
		if err := c.checkSSHConnectivity(applicationName, options.AppInstanceIndex); err != nil {
			return "", err
		}
	}

	if err := c.prepareRemotePaths(ctx); err != nil {
		return "", err
	}

	remoteCommand, err := c.buildRemoteCommand(ctx)
	if err != nil {
		return "", err
	}

	if options.DryRun {
		c.logVerbosef("Dry-run mode enabled, returning command without execution")
		if command.Name == cmdHeapDump && options.Open {
			ext := extHprof
			if options.Compress {
				ext = extHprofGz
			}
			fmt.Printf("Would open: %s\n", buildOpenURL(options.OpenURL, 0, "TOKEN"+ext))
		}
		return dryRunCommandLine(ctx, remoteCommand), nil
	}

	output, err := c.runRemoteCommand(ctx, remoteCommand)
	if err != nil {
		return "", err
	}

	if command.GenerateFiles {
		if err := c.collectGeneratedFile(ctx); err != nil {
			return "", err
		}
		if ctx.noDownload {
			fmt.Println("No download requested, skipping file download")
			return output, nil
		}
		if err := c.downloadGeneratedFile(ctx); err != nil {
			return "", err
		}
		if err := c.deleteRemoteArtifact(ctx, ctx.fileName, utils.ToSentenceCase(command.FileLabel)+" file deleted in application container"); err != nil {
			return "", err
		}
	}

	if command.GenerateArbitraryFiles && !ctx.noDownload {
		if err := c.downloadArbitraryFiles(ctx); err != nil {
			return "", err
		}
	}

	c.logVerbosef("Command execution completed successfully")
	return output, nil
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
