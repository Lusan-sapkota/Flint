package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMissingPaths(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755)
	os.WriteFile(filepath.Join(root, "main.go"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "internal", "auth", "token.go"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "app.log"), nil, 0o644)

	reply := "The entry point is `main.go` (see main.go:12), and tokens live in auth/token.go and `internal/auth/token.go`. " +
		"Validation is in `utils/auth.py` and src/handlers/user.ts. Logs go to app.log, `./app.log` and logs/old.log. " +
		"It calls `os.Stat`, `console.log`, `process.env`, `s.db` and `mu.Lock`, runs `go test ./...` on `origin/main`, " +
		"see https://example.com/docs/setup.md and `/no/such/abs.go`.\n" +
		"```go\nimport \"missing/in/code.go\"\n```"

	got := missingPaths(root)(reply)
	want := []string{"utils/auth.py", "/no/such/abs.go", "src/handlers/user.ts", "logs/old.log"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	if got := missingPaths(root)("Nothing to check here, just `os.Stat` and v1.2."); got != nil {
		t.Errorf("want no candidates, got %q", got)
	}
}

func TestGroundingNoteUsesLastAnswer(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), nil, 0o644)
	msgs := []Message{
		{Role: "assistant", Content: "See `old/gone.py`."},
		{Role: "user", Content: "and?"},
		{Role: "assistant", Content: "It's in `main.go` and `src/app.ts`."},
		{Role: "tool", Content: "[exit code: 0]"},
	}
	if got := groundingNote(msgs, root); !strings.Contains(got, "`src/app.ts`") || strings.Contains(got, "gone.py") || strings.Contains(got, "main.go") {
		t.Errorf("want only the last answer's missing name, got %q", got)
	}
	msgs[2].Content = "It's in `main.go`."
	if got := groundingNote(msgs, root); got != "" {
		t.Errorf("want no note when the last answer is grounded, got %q", got)
	}
}

func TestFileTreeShallowFirstAndCut(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "deep"), 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "top.go"), nil, 0o644)
	os.WriteFile(filepath.Join(root, ".env"), nil, 0o644)
	os.WriteFile(filepath.Join(root, ".git", "HEAD"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "a", "mid.go"), nil, 0o644)
	for i := range maxTreeChars / 10 {
		os.WriteFile(filepath.Join(root, "a", "deep", fmt.Sprintf("f%07d.go", i)), nil, 0o644)
	}

	got := fileTree(root)
	if want := "./: top.go\na/: mid.go\n(240 more files in deeper folders not listed)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGroundingNoteSuggestsRealPath(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "backend"), 0o755)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "backend", "shield.go"), nil, 0o644)
	msgs := []Message{{Role: "assistant", Content: "See `agents.md` and `src/shield.go`."}}
	got := groundingNote(msgs, root)
	for _, want := range []string{"`agents.md` (exists as `AGENTS.md`)", "`src/shield.go` (exists as `backend/shield.go`)"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in %q", want, got)
		}
	}
}
