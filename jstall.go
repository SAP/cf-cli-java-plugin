/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/google/shlex"
)

//go:embed dist/jstall-minimal.jar
var jstallJarBytes []byte

func javaExecutable() string {
	if runtime.GOOS == osWindows {
		return "java.exe"
	}
	return cmdJava
}

func getJavaMajorVersion(javaPath string) (int, error) {
	cmd := exec.Command(javaPath, "-version") //nolint:gosec // G702: javaPath comes from findJava17Plus, resolved from JAVA_HOME or PATH
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	outputStr := string(output)
	start := strings.Index(outputStr, "\"")
	if start == -1 {
		return 0, fmt.Errorf("cannot parse java version output")
	}
	end := strings.Index(outputStr[start+1:], "\"")
	if end == -1 {
		return 0, fmt.Errorf("cannot parse java version output")
	}
	versionStr := outputStr[start+1 : start+1+end]
	parts := strings.Split(versionStr, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	// Old format: 1.X.Y (Java 8 and below)
	if major == 1 && len(parts) > 1 {
		major, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
	}
	return major, nil
}

func platformJavaCandidates() []string {
	exe := javaExecutable()
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		matches, _ := filepath.Glob("/Library/Java/JavaVirtualMachines/*/Contents/Home/bin/" + exe)
		candidates = append(candidates, matches...)
		matches, _ = filepath.Glob("/opt/homebrew/Cellar/openjdk/*/bin/" + exe)
		candidates = append(candidates, matches...)
		matches, _ = filepath.Glob("/opt/homebrew/Cellar/openjdk@*/*/bin/" + exe)
		candidates = append(candidates, matches...)
	case "linux":
		matches, _ := filepath.Glob("/usr/lib/jvm/*/bin/" + exe)
		candidates = append(candidates, matches...)
	case osWindows:
		for _, envVar := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
			base := os.Getenv(envVar)
			if base == "" {
				continue
			}
			for _, vendor := range []string{"Java", "SapMachine", "Eclipse Adoptium", "Microsoft", "Amazon Corretto", "Zulu"} {
				matches, _ := filepath.Glob(filepath.Join(base, vendor, "*", "bin", exe))
				candidates = append(candidates, matches...)
			}
		}
	}
	return candidates
}

func findJava17Plus() (string, error) {
	var candidates []string
	exe := javaExecutable()
	if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
		candidates = append(candidates, filepath.Join(javaHome, "bin", exe))
	}
	if path, err := exec.LookPath(exe); err == nil {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, platformJavaCandidates()...)

	seen := make(map[string]bool)
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			resolved = candidate
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		if version, err := getJavaMajorVersion(candidate); err == nil && version >= 17 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Java 17+ installation found. Install a JDK 17+ and ensure it is on your PATH or set JAVA_HOME")
}

func jstallJarHash() string {
	h := sha256.Sum256(jstallJarBytes)
	return hex.EncodeToString(h[:])
}

func ensureJstallJar() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	pluginCacheDir := filepath.Join(cacheDir, "cf-java-plugin")
	if err := os.MkdirAll(pluginCacheDir, 0o755); err != nil { //nolint:gosec // 0755 is correct for a cache dir
		return "", err
	}
	jarPath := filepath.Join(pluginCacheDir, "jstall-minimal.jar")
	hashPath := jarPath + ".sha256"

	// Check if cached JAR matches the embedded version by SHA-256 hash
	expectedHash := jstallJarHash()
	if cachedHash, err := os.ReadFile(hashPath); err == nil && string(cachedHash) == expectedHash { //nolint:gosec // path is derived from UserCacheDir, not user input
		if _, err := os.Stat(jarPath); err == nil {
			return jarPath, nil
		}
	}

	// Extract embedded JAR and write hash
	if err := os.WriteFile(jarPath, jstallJarBytes, 0o644); err != nil { //nolint:gosec // 0644 is correct; JAR must be readable to execute
		return "", err
	}
	if err := os.WriteFile(hashPath, []byte(expectedHash), 0o644); err != nil { //nolint:gosec // 0644 is correct for a hash file
		// Non-fatal: JAR is already written, just can't cache the hash
		_ = err
	}
	return jarPath, nil
}

func quoteArgForDisplay(arg string) string {
	if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\") {
		return strconv.Quote(arg)
	}
	return arg
}

func formatCommandForDisplay(command string, args []string) string {
	displayArgs := make([]string, len(args))
	for i, arg := range args {
		displayArgs[i] = quoteArgForDisplay(arg)
	}
	return command + " " + strings.Join(displayArgs, " ")
}

// jstallWindowsQuotingFix is a JVM system property that fixes `cf java status`/`jstall` on Windows.
// jstall sends its remote shell snippets to the app container by running
// `ProcessBuilder("cf", "ssh", APP, "-c", payload)`. The payload contains embedded double quotes
// (e.g. `if [ -n "$JAVA_HOME" ] ...; jcmd "123" "Thread.print"`). With the JDK default
// (jdk.lang.Process.allowAmbiguousCommands=true -> VERIFICATION_WIN32), Java wraps such an argument
// in quotes WITHOUT escaping the embedded quotes, so cf.exe's Windows argv parser shreds it into
// many broken tokens (quotes lost, payload split at spaces) and the command fails.
// Setting allowAmbiguousCommands=false switches Java to VERIFICATION_WIN32_SAFE, which escapes
// embedded quotes with backslashes, and the payload arrives at cf.exe exactly as jstall built it.
// The property only affects process creation on Windows; it is inert on other platforms.
const jstallWindowsQuotingFix = "-Djdk.lang.Process.allowAmbiguousCommands=false"

// buildJstallArgs assembles the arguments for the jstall JVM invocation (without the java binary).
func buildJstallArgs(jarPath, appName, jstallArgs string, appInstanceIndex int) ([]string, error) {
	args := []string{jstallWindowsQuotingFix, "-jar", jarPath}

	// Use --cf which jstall translates to "cf ssh <app> -c" internally via ProcessBuilder
	// (no sh -c wrapper since v0.7.2). For instance index, fall back to --ssh
	// since --cf doesn't support it. See jstallWindowsQuotingFix for why remote
	// execution additionally needs the system property set above on Windows.
	if appInstanceIndex != -1 {
		sshCmd := "cf ssh " + appName + " --app-instance-index " + strconv.Itoa(appInstanceIndex) + " -c"
		args = append(args, "--ssh", sshCmd)
	} else {
		args = append(args, "--cf", appName)
	}

	if jstallArgs != "" {
		splitArgs, err := shlex.Split(jstallArgs)
		if err != nil {
			return nil, fmt.Errorf("invalid jstall arguments: %w", err)
		}
		args = append(args, splitArgs...)
	}
	return args, nil
}

func (c *JavaPlugin) executeJstall(appName string, jstallArgs string, appInstanceIndex int, dryRun bool) (string, error) {
	javaPath, err := findJava17Plus()
	if err != nil {
		return "", err
	}
	c.logVerbosef("Found Java 17+: %s", javaPath)

	jarPath, err := ensureJstallJar()
	if err != nil {
		return "", fmt.Errorf("failed to extract jstall JAR: %w", err)
	}
	c.logVerbosef("JStall JAR at: %s", jarPath)

	args, err := buildJstallArgs(jarPath, appName, jstallArgs, appInstanceIndex)
	if err != nil {
		return "", err
	}

	displayCmd := formatCommandForDisplay(javaPath, args)
	c.logVerbosef("JStall command: %s", displayCmd)

	if dryRun {
		return displayCmd, nil
	}

	// Pre-validate SSH connectivity to avoid confusing "No JVMs found" errors
	if appName != "" {
		testArgs := []string{cmdSSH, appName}
		if appInstanceIndex != -1 {
			testArgs = append(testArgs, "--app-instance-index", strconv.Itoa(appInstanceIndex))
		}
		testArgs = append(testArgs, "-c", "echo ok")
		testCmd := exec.Command("cf", testArgs...)
		testOutput, testErr := testCmd.CombinedOutput()
		if testErr != nil {
			outputStr := strings.TrimSpace(string(testOutput))
			if outputStr != "" {
				return "", fmt.Errorf("cannot connect to application via SSH: %s", outputStr)
			}
			return "", fmt.Errorf("cannot connect to application via SSH: %w", testErr)
		}
	}

	cmd := exec.Command(javaPath, args...) //nolint:gosec // G702: javaPath comes from findJava17Plus, resolved from JAVA_HOME or PATH
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 10 {
			// Exit code 10 means jstall ran successfully but found a warning condition
			// (e.g. outdated JVM). Output was already printed; treat as success.
			return "", nil
		}
		return "", fmt.Errorf("jstall execution failed: %w", err)
	}
	return "", nil
}
