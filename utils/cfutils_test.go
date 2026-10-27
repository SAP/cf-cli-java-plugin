package utils

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestShellSingleQuote_NoSpecialChars(t *testing.T) {
	got := ShellSingleQuote("hello")
	if got != "'hello'" {
		t.Errorf("expected 'hello', got %s", got)
	}
}

func TestShellSingleQuote_Empty(t *testing.T) {
	got := ShellSingleQuote("")
	if got != "''" {
		t.Errorf("expected '', got %s", got)
	}
}

func TestShellSingleQuote_WithSingleQuote(t *testing.T) {
	got := ShellSingleQuote("don't")
	// The result must be a valid shell expression that evaluates to don't
	if got != "'don'\"'\"'t'" {
		t.Errorf("unexpected quoting for don't: %s", got)
	}
}

func TestShellSingleQuote_RoundTripThroughShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test requires POSIX shell")
	}
	inputs := []string{
		"hello",
		"",
		"don't",
		"it's a test",
		"O'Brien",
		"'fully quoted'",
		"a'b'c'd",
		"path/with spaces",
		"$HOME",
		"$(whoami)",
		`back\slash`,
		"double\"quote",
	}
	for _, input := range inputs {
		quoted := ShellSingleQuote(input)
		// Use printf %s to avoid echo's trailing-newline ambiguity
		cmd := exec.Command("sh", "-c", "printf %s "+quoted)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("ShellSingleQuote(%q) produced invalid shell: %s (error: %v)", input, quoted, err)
			continue
		}
		if string(output) != input {
			t.Errorf("ShellSingleQuote(%q) round-trip failed: quoted=%s, got=%q", input, quoted, string(output))
		}
	}
}
