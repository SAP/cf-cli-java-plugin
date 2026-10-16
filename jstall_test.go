package main

import (
	"slices"
	"testing"
)

func TestBuildJstallArgs_unicodeAfterSubcommand(t *testing.T) {
	args, err := buildJstallArgs("/tmp/jstall.jar", "myapp", "status myapp", -1)
	if err != nil {
		t.Fatal(err)
	}
	// --unicode off must appear after the subcommand (status) so femtocli routes it correctly
	unicodeIdx := slices.Index(args, "--unicode")
	statusIdx := slices.Index(args, "status")
	if unicodeIdx == -1 {
		t.Fatal("--unicode not found in args")
	}
	if statusIdx == -1 {
		t.Fatal("status subcommand not found in args")
	}
	if unicodeIdx < statusIdx {
		t.Errorf("--unicode (idx %d) must come after subcommand 'status' (idx %d), args: %v", unicodeIdx, statusIdx, args)
	}
	if args[unicodeIdx+1] != "off" {
		t.Errorf("expected --unicode off, got --unicode %s", args[unicodeIdx+1])
	}
}
