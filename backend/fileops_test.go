package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInFolderStaysInside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))
	os.Mkdir(filepath.Join(root, "sub"), 0o755)

	for _, p := range []string{"../x", filepath.Join(outside, "secret"), "link/secret", "link/new.txt", "/etc/passwd", ""} {
		if _, err := resolveInFolder(root, p); err == nil {
			t.Errorf("%q should be refused", p)
		}
	}
	for _, p := range []string{"a.txt", "sub/new.go", "./sub/../b.md", filepath.Join(root, "c.txt")} {
		if _, err := resolveInFolder(root, p); err != nil {
			t.Errorf("%q should be allowed: %v", p, err)
		}
	}
	if _, err := resolveInFolder(root, "nodir/x.go"); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("a missing parent folder should say so, got %v", err)
	}
}

func TestPlanEdit(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "c.yaml"), []byte("db:\n  max: 25\n  min: 25\n"), 0o644)
	args := func(m map[string]string) json.RawMessage { b, _ := json.Marshal(m); return b }

	e, err := planEdit(root, "edit_file", args(map[string]string{"path": "c.yaml", "old_text": "  max: 25", "new_text": "  max: 50"}))
	if err != nil || e.Content != "db:\n  max: 50\n  min: 25\n" {
		t.Fatalf("got %q, %v", e.Content, err)
	}
	if want := "--- c.yaml\n+++ c.yaml\n@@ -1,3 +1,3 @@\n db:\n-  max: 25\n+  max: 50\n   min: 25"; e.Diff != want {
		t.Errorf("diff:\n%s\nwant:\n%s", e.Diff, want)
	}

	for _, tc := range []struct {
		tool string
		args map[string]string
		want string
	}{
		{"edit_file", map[string]string{"path": "c.yaml", "old_text": "25", "new_text": "1"}, "appears 2 times"},
		{"edit_file", map[string]string{"path": "c.yaml", "old_text": "   2\t  max: 25", "new_text": "x"}, "not found"},
		{"edit_file", map[string]string{"path": "c.yaml", "old_text": "max", "new_text": "max"}, "no change"},
		{"edit_file", map[string]string{"path": "nope.yaml", "old_text": "a", "new_text": "b"}, "use write_file"},
		{"write_file", map[string]string{"path": "../x", "content": "y"}, "outside"},
	} {
		if _, err := planEdit(root, tc.tool, args(tc.args)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: want error containing %q, got %v", tc.args, tc.want, err)
		}
	}

	e, err = planEdit(root, "edit_file", args(map[string]string{"path": "c.yaml", "old_text": "", "new_text": "cache: 1"}))
	if err != nil || !strings.HasSuffix(e.Content, "  min: 25\ncache: 1\n") {
		t.Errorf("empty old_text should append, got %q, %v", e.Content, err)
	}
	e, err = planEdit(root, "write_file", args(map[string]string{"path": "new.txt", "content": "hi\n"}))
	if err != nil || e.Base != "" || !strings.HasPrefix(e.Diff, "--- /dev/null\n+++ new.txt\n@@ -1,0 +1,1 @@\n+hi") {
		t.Errorf("new file: %q, %v", e.Diff, err)
	}
}

func TestApplyEditRefusesAChangedFile(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	os.WriteFile(path, []byte("one\n"), 0o600)

	raw, _ := json.Marshal(map[string]string{"path": "a.txt", "old_text": "one", "new_text": "two"})
	e, err := planEdit(root, "edit_file", raw)
	if err != nil {
		t.Fatal(err)
	}
	editJSON, _ := json.Marshal(e)
	edit := string(editJSON)
	cmd := &Command{ID: "c1", Edit: &edit}

	os.WriteFile(path, []byte("one!\n"), 0o600)
	if out, status := s.applyEdit(cmd); status != "failed" || !strings.Contains(out, "changed since") {
		t.Errorf("a file changed after the proposal must not be overwritten: %s %q", status, out)
	}
	os.WriteFile(path, []byte("one\n"), 0o600)
	if out, status := s.applyEdit(cmd); status != "success" || !strings.HasPrefix(out, "[exit code: 0] wrote a.txt\n") {
		t.Fatalf("got %s %q", status, out)
	}
	if data, _ := os.ReadFile(path); string(data) != "two\n" {
		t.Errorf("file is %q", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
}

func TestReadFileResult(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("l1\nl2\nl3\n"), 0o644)
	os.WriteFile(filepath.Join(root, "bin"), []byte{0, 1, 2}, 0o644)
	raw := func(m map[string]any) json.RawMessage { b, _ := json.Marshal(m); return b }

	if got, want := readFileResult(root, raw(map[string]any{"path": "a.txt", "start_line": 2})), "[read a.txt: lines 2-3 of 3]\n   2\tl2\n   3\tl3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, p := range []string{"bin", "missing.txt", "../etc"} {
		if got := readFileResult(root, raw(map[string]any{"path": p})); !strings.HasPrefix(got, "[FAILED to read: ") {
			t.Errorf("%s: want a failure, got %q", p, got)
		}
	}
}
