package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCommandPreconditions_MissingBinary(t *testing.T) {
	ok, reason := checkCommandPreconditions("this_binary_definitely_does_not_exist_xyz --version", ".")
	if ok {
		t.Fatal("expected a nonexistent binary to fail preconditions")
	}
	if reason == "" {
		t.Fatal("expected a reason")
	}
}

func TestCheckCommandPreconditions_MissingFile(t *testing.T) {
	dir := t.TempDir()
	ok, reason := checkCommandPreconditions("cat this_file_does_not_exist.txt", dir)
	if ok {
		t.Fatal("expected cat on a missing file to fail preconditions")
	}
	if reason == "" {
		t.Fatal("expected a reason")
	}
}

func TestCheckCommandPreconditions_ExistingFilePasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := checkCommandPreconditions("cat exists.txt", dir)
	if !ok {
		t.Fatalf("expected cat on an existing file to pass preconditions, got reason: %s", reason)
	}
}

func TestCheckCommandPreconditions_BuiltinsAndOrdinaryCommandsPass(t *testing.T) {
	dir := t.TempDir()
	cases := []string{
		"echo hello",
		"pwd",
		"ls -la",
		"go test ./...",
		"git status",
		"wc -l",
	}
	for _, cmd := range cases {
		if ok, reason := checkCommandPreconditions(cmd, dir); !ok {
			t.Errorf("expected %q to pass preconditions, got blocked: %s", cmd, reason)
		}
	}
}

func TestCheckCommandPreconditions_CompoundCommandsSkipPathCheck(t *testing.T) {
	dir := t.TempDir()
	ok, reason := checkCommandPreconditions("cat missing.txt | wc -l", dir)
	if !ok {
		t.Fatalf("expected a compound command to skip the ambiguous path check, got blocked: %s", reason)
	}
}
