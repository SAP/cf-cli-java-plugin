package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeFakeBin writes a script that exits with the given code and returns its path.
// On Windows it writes a .bat file; on Unix a shell script.
func makeFakeBin(t *testing.T, exitCode int) string {
	t.Helper()
	tmp := t.TempDir()
	if runtime.GOOS == osWindows {
		bin := filepath.Join(tmp, "fake-redact.bat")
		script := fmt.Sprintf("@echo off\r\nexit /b %d\r\n", exitCode)
		if err := os.WriteFile(bin, []byte(script), 0o600); err != nil {
			t.Fatalf("write fake bin: %v", err)
		}
		return bin
	}
	bin := filepath.Join(tmp, "fake-redact")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatalf("write fake bin: %v", err)
	}
	return bin
}

// makeCopyBin writes a script that copies stdin to $2 (Unix) or %2 (Windows).
func makeCopyBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if runtime.GOOS == osWindows {
		bin := filepath.Join(tmp, "fake-redact.bat")
		// Use PowerShell to copy stdin in binary mode; `more` corrupts non-text bytes.
		script := "@echo off\r\npowershell -Command \"$in=[System.Console]::OpenStandardInput();$out=[System.IO.File]::OpenWrite('%2');$in.CopyTo($out);$out.Close()\"\r\nexit /b 0\r\n"
		if err := os.WriteFile(bin, []byte(script), 0o600); err != nil {
			t.Fatalf("write copy bin: %v", err)
		}
		return bin
	}
	bin := filepath.Join(tmp, "fake-redact")
	script := "#!/bin/sh\ncat - > \"$2\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatalf("write copy bin: %v", err)
	}
	return bin
}

// makeWriteAndFailBin writes a script that writes PARTIAL to $2/%2 then exits 1.
func makeWriteAndFailBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if runtime.GOOS == osWindows {
		bin := filepath.Join(tmp, "fake-redact.bat")
		script := "@echo off\r\necho PARTIAL> \"%2\"\r\nexit /b 1\r\n"
		if err := os.WriteFile(bin, []byte(script), 0o600); err != nil {
			t.Fatalf("write write-and-fail bin: %v", err)
		}
		return bin
	}
	bin := filepath.Join(tmp, "fake-redact")
	script := "#!/bin/sh\necho PARTIAL > \"$2\"\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatalf("write write-and-fail bin: %v", err)
	}
	return bin
}

func TestPipeHeapDumpThroughRedact_ErrorDeletesPartial(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "dump.hprof")

	failBin := makeFakeBin(t, 1)
	_, err := pipeHeapDumpThroughRedact(failBin, bytes.NewBufferString("FAKE"), outputPath, "lean", false)
	if err == nil {
		t.Fatal("expected error from failing redact binary, got nil")
	}

	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Error("partial redacted file should have been deleted on error, but still exists")
	}
}

func TestPipeHeapDumpThroughRedact_KeepOnError(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "dump.hprof")

	bin := makeWriteAndFailBin(t)
	_, err := pipeHeapDumpThroughRedact(bin, bytes.NewBufferString("FAKE"), outputPath, "lean", true)
	if err == nil {
		t.Fatal("expected error from failing redact binary, got nil")
	}

	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Error("partial file should have been kept with keepOnError=true, but is gone")
	}
}

func TestPipeHeapDumpThroughRedact_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "dump.hprof")

	bin := makeCopyBin(t)
	out, err := pipeHeapDumpThroughRedact(bin, bytes.NewBufferString("HEAP"), outputPath, "lean", false)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if out != outputPath {
		t.Errorf("output path: want %q, got %q", outputPath, out)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Errorf("output file missing: %v", statErr)
	}
	data, readErr := os.ReadFile(out) //nolint:gosec // test reads file path produced by helper under test
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	// Windows `more` appends \r\n; strip for comparison
	content := strings.TrimRight(string(data), "\r\n")
	if content != "HEAP" {
		t.Fatalf("unexpected output content: %q", string(data))
	}
}

func TestPipeHeapDumpThroughRedact_PassesCompressedInputUnchanged(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "dump.hprof")

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write([]byte("HEAP")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	bin := makeCopyBin(t)
	out, err := pipeHeapDumpThroughRedact(bin, bytes.NewReader(compressed.Bytes()), outputPath, "lean", false)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	data, err := os.ReadFile(out) //nolint:gosec // test reads file path produced by helper under test
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(data, compressed.Bytes()) {
		t.Fatal("compressed input was modified before reaching hprof-redact")
	}
}

func TestPipeHeapDumpThroughRedact_RejectsUnexpectedExtension(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "dump.bin")

	bin := makeFakeBin(t, 0)
	_, err := pipeHeapDumpThroughRedact(bin, bytes.NewBufferString("HEAP"), base, "lean", false)
	if err == nil {
		t.Fatal("expected unsupported extension error, got nil")
	}
}

func TestCombineHeapDumpStreamErrors(t *testing.T) {
	redactErr := errors.New("redact boom")
	closeErr := errors.New("close boom")
	waitErr := errors.New("wait boom")

	err := combineHeapDumpStreamErrors(redactErr, closeErr, waitErr)
	if err == nil {
		t.Fatal("expected combined error, got nil")
	}

	for _, want := range []string{
		"redaction failed",
		"closing redaction input stream failed",
		"remote heap dump stream failed",
		"redact boom",
		"close boom",
		"wait boom",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected combined error to contain %q, got %q", want, err.Error())
		}
	}

	if !errors.Is(err, redactErr) || !errors.Is(err, closeErr) || !errors.Is(err, waitErr) {
		t.Fatal("expected combined error to match all component errors via errors.Is")
	}
}

func TestCombineHeapDumpStreamErrors_NilWhenNoErrors(t *testing.T) {
	if err := combineHeapDumpStreamErrors(nil, nil, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
