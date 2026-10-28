/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testAppName = "myapp"

func TestExecute_SwappedAppAndCommand(t *testing.T) {
	p := &JavaPlugin{}
	// User typed: cf java myapp heap-dump  (app and command swapped)
	_, err := p.execute([]string{cmdJava, testAppName, cmdHeapDump})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cf java "+cmdHeapDump+" "+testAppName) {
		t.Errorf("expected corrected command in error message, got: %s", msg)
	}
}

func TestExecute_UnrecognizedCommand_NoSwapHint_WhenSecondArgNotACommand(t *testing.T) {
	p := &JavaPlugin{}
	// First arg is not a valid command, second arg is also not a valid command -> fuzzy match only
	_, err := p.execute([]string{cmdJava, "bogus-command", "also-not-a-command"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "swapped") {
		t.Errorf("should not suggest swap when second arg is not a known command: %s", err.Error())
	}
}

func TestExecute_SwappedAppAndCommand_SuggestsSwapBasedOnPattern(t *testing.T) {
	// cf java sapmachine17 vm-vitals — second arg is a known command, suggest swap regardless of CF app lookup
	p := &JavaPlugin{}
	_, err := p.execute([]string{cmdJava, "sapmachine17", cmdVmVitals})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cf java "+cmdVmVitals+" sapmachine17") {
		t.Errorf("expected swap suggestion in error message, got: %s", msg)
	}
}

func TestExecute_NoSwapHint_WhenSecondArgNotACommand(t *testing.T) {
	// cf java sapmachiasdfadfadfne17 not-a-command — second arg is not a command, no swap hint
	p := &JavaPlugin{}
	_, err := p.execute([]string{cmdJava, "sapmachiasdfadfadfne17", "not-a-command"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "swapped") {
		t.Errorf("should not suggest swap when second arg is not a known command: %s", msg)
	}
}

func TestParseOptions_Open(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("expected Open=true")
	}
	if opts.OpenURL != defaultOpenURL {
		t.Errorf("expected OpenURL=%q, got %q", defaultOpenURL, opts.OpenURL)
	}
}

func TestParseOptions_OpenURL_ImpliesOpen(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpenURL, "http://localhost:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("--open-url should imply Open=true")
	}
	if opts.OpenURL != "http://localhost:8080" {
		t.Errorf("unexpected OpenURL: %q", opts.OpenURL)
	}
}

func TestParseOptions_Open_NoDownload_Error(t *testing.T) {
	p := &JavaPlugin{}
	_, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen, "--" + flagNoDownload})
	if err == nil {
		t.Fatal("expected error for --open + --no-download, got nil")
	}
}

func TestParseOptions_Open_DryRun_NoError(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen, "--dry-run"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("expected Open=true")
	}
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
}

func TestResolveLocalDir_AllowsDotsInFilename(t *testing.T) {
	got, err := resolveLocalDir("./sdfsdf..file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "./sdfsdf..file.txt" {
		t.Fatalf("unexpected local dir: %q", got)
	}
}

func TestResolveLocalDir_RejectsTraversal(t *testing.T) {
	_, err := resolveLocalDir("../tmp")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestPrepareRemotePaths_DryRunDoesNotCallCF(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("test uses a POSIX shell to detect CF CLI calls")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	cf := filepath.Join(dir, "cf")
	if err := os.WriteFile(cf, []byte("#!/bin/sh\nprintf 'called' >> \"$CF_CALL_LOG\"\nexit 1\n"), 0o700); err != nil { //nolint:gosec // executable test-only CF stub lives in t.TempDir
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CF_CALL_LOG", marker)

	ctx := &commandContext{
		command:         &Command{Name: cmdHeapDump, GenerateFiles: true, FileExtension: extHprof},
		options:         &Options{DryRun: true},
		applicationName: testAppName,
	}
	if err := (&JavaPlugin{}).prepareRemotePaths(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.fspath != "/tmp" {
		t.Fatalf("expected offline dry-run fallback /tmp, got %q", ctx.fspath)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dry-run called cf (stat error: %v)", err)
	}
}

// --- buildJstallArgs (plugin-level) tests ---

// findTestCommand looks up a command by name from the real commands slice.
// Tests use this instead of duplicating Command struct literals so they
// break immediately when a command definition changes in the source.
func findTestCommand(t *testing.T, name string) *Command {
	t.Helper()
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i]
		}
	}
	t.Fatalf("command %q not found in commands slice — was it renamed?", name)
	return nil
}

func TestBuildJstallArgs_StatusDefaultsToAll(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "status")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"status", testAppName}, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "status all" {
		t.Errorf("expected %q, got %q", "status all", got)
	}
}

func TestBuildJstallArgs_StatusWithArgs(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "status")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"status", testAppName}, &Options{Args: "--dumps 3"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "status all --dumps 3" {
		t.Errorf("expected %q, got %q", "status all --dumps 3", got)
	}
}

func TestBuildJstallArgs_StatusFull(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "status")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"status", testAppName}, &Options{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "status all --full" {
		t.Errorf("expected %q, got %q", "status all --full", got)
	}
}

func TestBuildJstallArgs_StatusFullNoDuplicate(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "status")
	// User explicitly passes --full via --args; the plugin must not add it again
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"status", testAppName}, &Options{Full: true, Args: "--full"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "--full") != 1 {
		t.Errorf("expected exactly one --full, got %q", got)
	}
}

func TestBuildJstallArgs_FullNotSuppressedByPrefix(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "status")
	// A hypothetical --fullscreen must not prevent --full from being appended
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"status", testAppName}, &Options{Full: true, Args: "--fullscreen"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "--fullscreen") {
		t.Errorf("expected --fullscreen in args, got %q", got)
	}
	if !strings.HasSuffix(got, "--full") {
		t.Errorf("expected --full appended at end, got %q", got)
	}
}

func TestBuildJstallArgs_JstallDefaultsToStatusAll(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "jstall")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"jstall", testAppName}, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "status all" {
		t.Errorf("expected %q, got %q", "status all", got)
	}
}

func TestBuildJstallArgs_JstallPassthrough(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "jstall")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"jstall", testAppName}, &Options{Args: "deadlock all"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "deadlock all" {
		t.Errorf("expected %q, got %q", "deadlock all", got)
	}
}

func TestBuildJstallArgs_RecordStatusDefaultFile(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "record-status")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"record-status", testAppName}, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	expected := "record all --output " + testAppName + "-status.zip"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestBuildJstallArgs_RecordStatusCustomFile(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "record-status")
	got, err := p.buildJstallArgs(cmd, testAppName, []string{"record-status", testAppName, "diag.zip"}, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "record all --output diag.zip" {
		t.Errorf("expected %q, got %q", "record all --output diag.zip", got)
	}
}

func TestBuildJstallArgs_RecordStatusTooManyTrailingArgs(t *testing.T) {
	p := &JavaPlugin{}
	cmd := findTestCommand(t, "record-status")
	_, err := p.buildJstallArgs(cmd, testAppName, []string{"record-status", testAppName, "a.zip", "extra"}, &Options{})
	if err == nil {
		t.Fatal("expected error for too many trailing args, got nil")
	}
}

// --- containsFlag tests ---

func TestContainsFlag(t *testing.T) {
	tests := []struct {
		s    string
		flag string
		want bool
	}{
		{"--full", "--full", true},
		{"status all --full", "--full", true},
		{"status --fullscreen all", "--full", false},
		{"--full-output", "--full", false},
		{"", "--full", false},
		{"status all", "--full", false},
		{"--full --verbose", "--full", true},
	}
	for _, tc := range tests {
		got := containsFlag(tc.s, tc.flag)
		if got != tc.want {
			t.Errorf("containsFlag(%q, %q) = %v, want %v", tc.s, tc.flag, got, tc.want)
		}
	}
}

// --- README / code drift detection ---

func TestHelpSubcommand_PrintsDescriptionAndExits(t *testing.T) {
	p := &JavaPlugin{}
	out, err := p.execute([]string{cmdJava, "help", cmdHeapDump})
	if err != nil {
		t.Fatalf("expected no error for help subcommand, got: %v", err)
	}
	if !strings.Contains(out, cmdHeapDump) {
		t.Errorf("expected output to contain command name %q, got: %s", cmdHeapDump, out)
	}
	cmd := findTestCommand(t, cmdHeapDump)
	if !strings.Contains(out, cmd.Description) {
		t.Errorf("expected output to contain description, got: %s", out)
	}
}

func TestHelpSubcommand_HeapDumpShowsHeapDumpFlags(t *testing.T) {
	p := &JavaPlugin{}
	out, err := p.execute([]string{cmdJava, "help", cmdHeapDump})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, flag := range []string{"--redact", "--compress", "--keep", "--local-dir"} {
		if !strings.Contains(out, flag) {
			t.Errorf("expected help heap-dump to list %s, got: %s", flag, out)
		}
	}
}

func TestHelpSubcommand_StatusDoesNotShowHeapDumpFlags(t *testing.T) {
	p := &JavaPlugin{}
	out, err := p.execute([]string{cmdJava, "help", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, flag := range []string{"--redact", "--compress", "--keep", "--local-dir"} {
		if strings.Contains(out, flag) {
			t.Errorf("expected help status NOT to list %s, got: %s", flag, out)
		}
	}
}

func TestReadmeCommandTable_ListsAllCommands(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Skipf("README.md not readable (ok in isolated test env): %v", err)
	}
	content := string(readme)
	for _, cmd := range commands {
		// Every command must appear as `| \x60name\x60 |` in the table
		marker := "| `" + cmd.Name + "` |"
		if !strings.Contains(content, marker) {
			t.Errorf("command %q missing from README command table (expected %s)", cmd.Name, marker)
		}
	}
}
