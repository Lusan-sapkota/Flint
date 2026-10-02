package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	if out, status := s.applyEdit(cmd); status != "success" || out != "[exit code: 0] wrote a.txt (1 line added, 1 removed)" {
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

func TestFileToolsKeepTheShieldsSecretFileRule(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(root, ".ssh", "id_ed25519"), []byte("KEY"), 0o600)
	os.Symlink(filepath.Join(root, ".ssh", "id_ed25519"), filepath.Join(root, "notes.txt"))
	raw := func(m map[string]string) json.RawMessage { b, _ := json.Marshal(m); return b }

	for _, p := range []string{".ssh/id_ed25519", "notes.txt", "./sub/../.ssh/id_ed25519"} {
		if got := readFileResult(root, raw(map[string]string{"path": p})); !strings.HasPrefix(got, "[BLOCKED by safety shield: accessing credential") || strings.Contains(got, "KEY") {
			t.Errorf("read %s: want the shield's block, got %q", p, got)
		}
	}
	for _, tc := range []struct{ tool, path string }{{"write_file", ".ssh/id_rsa"}, {"edit_file", ".ssh/id_ed25519"}, {"write_file", "notes.txt"}} {
		_, err := planEdit(root, tc.tool, raw(map[string]string{"path": tc.path, "content": "x", "old_text": "KEY", "new_text": "x"}))
		var blocked shieldError
		if !errors.As(err, &blocked) {
			t.Errorf("%s %s: want a shield error, got %v", tc.tool, tc.path, err)
		}
	}
}

func TestApprovedCloudReadReadsTheFile(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0o644)
	plan, _ := json.Marshal(plannedEdit{Read: json.RawMessage(`{"path":"a.txt"}`)})
	edit := string(plan)
	out, status := (&Server{db: db}).executeCommand(context.Background(), &Command{ID: "c1", Cwd: root, Edit: &edit})
	if status != "read" || out != "[read a.txt: lines 1-1 of 1]\n   1\thello" {
		t.Errorf("got %s %q", status, out)
	}
}

func TestCloudModelReadsWaitForApproval(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "a.txt"), []byte("SECRET-CONTENT\n"), 0o644)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "local"}, {"name": "big:cloud", "remote_host": "https://ollama.com:443"}}})
		case "/api/chat":
			if calls.Add(1)%2 == 1 {
				fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"a.txt"}}}]},"done":true}`)
			} else {
				fmt.Fprintln(w, `{"message":{"role":"assistant","content":"done"},"done":true}`)
			}
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()
	s := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: srv.URL, attachmentsDir: t.TempDir()}
	createUser(db, "alice", "Alice", "a@x.io", "h")
	u, _ := getUserByID(db, "alice")

	send := func(id, model string) string {
		calls.Store(0)
		createConversation(db, id, "alice", model)
		setAttachedFolder(db, id, folder)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"what is in a.txt?"}`))
		r.SetPathValue("id", id)
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		s.handlePostMessage(w, r)
		return w.Body.String()
	}

	if body := send("local1", "local"); !strings.Contains(body, "<<<READ>>>") || !strings.Contains(body, "SECRET-CONTENT") {
		t.Errorf("a local model should read without asking: %q", body)
	}
	body := send("cloud1", "big:cloud")
	if !strings.Contains(body, `<<<TOOL_CALL>>>`) || !strings.Contains(body, `"command":"read_file a.txt"`) || strings.Contains(body, "SECRET-CONTENT") {
		t.Errorf("a cloud model's read should wait for approval without sending the file: %q", body)
	}
	if cmd, _ := getPendingCommand(db, "cloud1"); cmd == nil || cmd.Edit == nil {
		t.Error("the cloud read should be a pending command")
	}
}
