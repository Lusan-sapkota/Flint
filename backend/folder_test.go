package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAnchorHeader_ListsEntriesAndSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"main.go", "README.md", ".gitignore"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}

	anchor := buildAnchorHeader(dir)

	if !strings.Contains(anchor, "main.go") || !strings.Contains(anchor, "README.md") {
		t.Fatalf("expected anchor to list visible files, got: %s", anchor)
	}
	if strings.Contains(anchor, ".gitignore") {
		t.Fatalf("expected anchor to skip hidden files, got: %s", anchor)
	}
	if !strings.Contains(anchor, "backend/") {
		t.Fatalf("expected anchor to mark directories with a trailing slash, got: %s", anchor)
	}
	if !strings.Contains(anchor, dir) {
		t.Fatalf("expected anchor to state the attached folder path, got: %s", anchor)
	}
}

func TestBuildAnchorHeader_EmptyFolderReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if anchor := buildAnchorHeader(dir); anchor != "" {
		t.Fatalf("expected empty anchor for an empty folder, got: %s", anchor)
	}
}
