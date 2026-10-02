package main

import (
	"os"
	"path/filepath"
	"slices"
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
