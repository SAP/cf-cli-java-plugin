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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed dist/hprof-redact-linux-amd64
var hprofRedactLinuxAmd64 []byte

//go:embed dist/hprof-redact-linux-arm64
var hprofRedactLinuxArm64 []byte

//go:embed dist/hprof-redact-darwin-arm64
var hprofRedactDarwinArm64 []byte

//go:embed dist/hprof-redact-windows-amd64.exe
var hprofRedactWindowsAmd64 []byte

//go:embed dist/hprof-redact-windows-arm64.exe
var hprofRedactWindowsArm64 []byte

// hprofRedactBytes returns the embedded hprof-redact binary for the current platform,
// or (nil, false) if this platform is not supported.
func hprofRedactBytes() ([]byte, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return hprofRedactLinuxAmd64, true
	case "linux/arm64":
		return hprofRedactLinuxArm64, true
	case "darwin/arm64":
		return hprofRedactDarwinArm64, true
	case osWindows + "/amd64":
		return hprofRedactWindowsAmd64, true
	case osWindows + "/arm64":
		return hprofRedactWindowsArm64, true
	default:
		return nil, false
	}
}

// ensureHprofRedact extracts the embedded hprof-redact binary to the plugin cache
// directory (same location as jstall) and returns its path.
func ensureHprofRedact() (string, error) {
	data, ok := hprofRedactBytes()
	if !ok {
		return "", fmt.Errorf("hprof-redact is not available for %s/%s; install manually: https://github.com/parttimenerd/hprof-analyzer/releases", runtime.GOOS, runtime.GOARCH)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("hprof-redact for %s/%s was not available at build time; install manually: https://github.com/parttimenerd/hprof-analyzer/releases", runtime.GOOS, runtime.GOARCH)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	pluginCacheDir := filepath.Join(cacheDir, "cf-java-plugin")
	if err := os.MkdirAll(pluginCacheDir, 0o755); err != nil { //nolint:gosec // 0755 is correct for a cache dir
		return "", err
	}

	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:8])
	binPath := filepath.Join(pluginCacheDir, fmt.Sprintf("hprof-redact-%s", hash))
	if runtime.GOOS == osWindows {
		binPath += ".exe"
	}

	// Re-use if already extracted
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	if err := os.WriteFile(binPath, data, 0o755); err != nil { //nolint:gosec // 0755: binary must be executable
		return "", fmt.Errorf("failed to extract hprof-redact: %w", err)
	}
	return binPath, nil
}

// pipeHeapDumpThroughRedact streams heap dump bytes through hprof-redact using
// stdin (`hprof-redact -`) and writes only the requested output path.
//
// mode must be "lean" or "complete". outputBasePath must end in .hprof or .hprof.gz.
func pipeHeapDumpThroughRedact(redactBin string, input io.Reader, outputBasePath, mode string, keepOnError bool) (string, error) {
	if !strings.HasSuffix(outputBasePath, extHprof) && !strings.HasSuffix(outputBasePath, extHprofGz) {
		return "", fmt.Errorf("unsupported heap dump path %q: expected %s or %s suffix", outputBasePath, extHprof, extHprofGz)
	}
	outputPath := outputBasePath

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil { //nolint:gosec // local output dir for plugin-managed file
		return "", fmt.Errorf("cannot create local directory %s: %w", filepath.Dir(outputPath), err)
	}

	var args []string
	if mode == "complete" {
		args = append(args, "--complete")
	}
	args = append(args, "-", outputPath)

	cmd := exec.Command(redactBin, args...) //nolint:gosec // redactBin comes from ensureHprofRedact, not user input
	cmd.Stdin = input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		if !keepOnError {
			if rmErr := os.Remove(outputPath); rmErr != nil && !os.IsNotExist(rmErr) {
				fmt.Fprintf(os.Stderr, "warning: could not remove partial redacted file %s: %v\n", outputPath, rmErr)
			}
		}
		return "", fmt.Errorf("hprof-redact failed: %w", err)
	}

	return outputPath, nil
}

func combineHeapDumpStreamErrors(redactErr, closeErr, waitErr error) error {
	parts := make([]string, 0, 3)
	joined := make([]error, 0, 3)

	if redactErr != nil {
		parts = append(parts, "redaction failed")
		joined = append(joined, redactErr)
	}
	if closeErr != nil {
		parts = append(parts, "closing redaction input stream failed")
		joined = append(joined, closeErr)
	}
	if waitErr != nil {
		parts = append(parts, "remote heap dump stream failed")
		joined = append(joined, waitErr)
	}
	if len(joined) == 0 {
		return nil
	}

	return fmt.Errorf("%s: %w", strings.Join(parts, "; "), errors.Join(joined...))
}
