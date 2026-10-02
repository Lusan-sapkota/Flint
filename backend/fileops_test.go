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

func TestUserAdjustedEdit(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	root := t.TempDir()
	path := filepath.Join(root, "c.yaml")
	os.WriteFile(path, []byte("db:\n  max: 25\n  min: 25\n"), 0o644)
	raw, _ := json.Marshal(map[string]string{"path": "c.yaml", "old_text": "  max: 25", "new_text": "  max: 50"})
	e, err := planEdit(root, "edit_file", raw)
	if err != nil || e.Editable != "  max: 50" {
		t.Fatalf("editable %q, %v", e.Editable, err)
	}

	same := e
	if adjustEdit(&same, "  max: 50"); same.Adjusted != "" || same.Content != e.Content {
		t.Error("an unchanged replacement isn't an adjustment")
	}
	if err := adjustEdit(&e, "  max: 60\n  extra: 1"); err != nil {
		t.Fatal(err)
	}
	if e.Content != "db:\n  max: 60\n  extra: 1\n  min: 25\n" || !strings.Contains(e.Diff, "+  extra: 1") {
		t.Fatalf("content %q diff %q", e.Content, e.Diff)
	}
	editJSON, _ := json.Marshal(e)
	edit := string(editJSON)
	out, status := s.applyEdit(&Command{ID: "c1", Edit: &edit})
	if status != "success" || !strings.Contains(out, "The user changed your proposed text") || !strings.HasSuffix(out, "  max: 60\n  extra: 1") {
		t.Errorf("got %s %q", status, out)
	}
	if data, _ := os.ReadFile(path); string(data) != "db:\n  max: 60\n  extra: 1\n  min: 25\n" {
		t.Errorf("file is %q", data)
	}

	raw, _ = json.Marshal(map[string]string{"path": "new.txt", "content": "hi\n"})
	n, _ := planEdit(root, "write_file", raw)
	if err := adjustEdit(&n, "hello\nthere"); err != nil || n.Content != "hello\nthere\n" {
		t.Errorf("new file: %q, %v", n.Content, err)
	}

	stale, _ := planEdit(root, "edit_file", json.RawMessage(`{"path":"c.yaml","old_text":"  min: 25","new_text":"  min: 5"}`))
	os.WriteFile(path, []byte("changed\n"), 0o644)
	if err := adjustEdit(&stale, "  min: 1"); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("a changed file must refuse the adjustment, got %v", err)
	}
}

func TestMissedOldTextPointsAtTheLine(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.csv"), []byte("sku,price\nA-100,19.99\nB-220,4.50\nC-310,4.50\n"), 0o644)
	try := func(old string) string {
		raw, _ := json.Marshal(map[string]string{"path": "p.csv", "old_text": old, "new_text": "x"})
		_, err := planEdit(root, "edit_file", raw)
		return err.Error()
	}
	if got := try("4.50\nB-220"); !strings.Contains(got, `Line 3 contains every part of it: "B-220,4.50". Use that whole line as old_text, and as new_text the whole line with your change made.`) {
		t.Errorf("want a pointer to line 3, got %q", got)
	}
	for _, old := range []string{"4.50 ", "Z-999,1"} {
		if got := try(old); strings.Contains(got, "contains every part") {
			t.Errorf("%q matches several lines or none: no pointer expected, got %q", old, got)
		}
	}
}

func TestMultiEditAndHunks(t *testing.T) {
	root := t.TempDir()
	src := "from utils import parse_sku\n\nA = 1\nB = 2\nC = 3\nD = 4\nE = 5\nF = 6\nG = 7\n\ndef f(o):\n    return parse_sku(o)\n"
	os.WriteFile(filepath.Join(root, "main.py"), []byte(src), 0o644)
	args, _ := json.Marshal(map[string]any{"path": "main.py", "edits": []map[string]string{
		{"old_text": "import parse_sku", "new_text": "import parse_code"},
		{"old_text": "return parse_sku(o)", "new_text": "return parse_code(o)"},
	}})
	e, err := planEdit(root, "edit_file", args)
	if err != nil || e.Content != strings.ReplaceAll(src, "parse_sku", "parse_code") {
		t.Fatalf("got %q, %v", e.Content, err)
	}
	if len(e.Hunks) != 2 || strings.Count(e.Diff, "\n@@ ") != 2 {
		t.Fatalf("want two hunks, got %d:\n%s", len(e.Hunks), e.Diff)
	}
	if !strings.Contains(e.Diff, "@@ -9,4 +9,4 @@\n G = 7\n \n def f(o):\n-    return parse_sku(o)\n+    return parse_code(o)") {
		t.Errorf("second hunk header or body wrong:\n%s", e.Diff)
	}
	kept := strings.Join(applyHunks(splitLines(src), e.Hunks, []bool{false, true}), "\n") + "\n"
	if kept != strings.Replace(src, "return parse_sku", "return parse_code", 1) {
		t.Errorf("only the second hunk should be written, got %q", kept)
	}

	// A later edit sees the earlier one; an edit that misses names its place in the list.
	args, _ = json.Marshal(map[string]any{"path": "main.py", "edits": []map[string]string{
		{"old_text": "A = 1", "new_text": "A = 10"}, {"old_text": "A = 1\n", "new_text": "x"},
	}})
	if _, err := planEdit(root, "edit_file", args); err == nil || !strings.Contains(err.Error(), "edits[1].old_text was not found") {
		t.Errorf("want edits[1] not found, got %v", err)
	}
}

func TestKeepHunks(t *testing.T) {
	root := t.TempDir()
	src := "a = 1\nx\nx\nx\nx\nx\nx\nx\nb = 2\n"
	os.WriteFile(filepath.Join(root, "f.py"), []byte(src), 0o644)
	args, _ := json.Marshal(map[string]any{"path": "f.py", "edits": []map[string]string{{"old_text": "a = 1", "new_text": "a = 10"}, {"old_text": "b = 2", "new_text": "b = 20"}}})
	e, err := planEdit(root, "edit_file", args)
	if err != nil || len(e.Hunks) != 2 {
		t.Fatalf("want two hunks: %v %v", e.Hunks, err)
	}
	if err := keepHunks(&e, []int{1}); err != nil {
		t.Fatal(err)
	}
	if e.Content != strings.Replace(src, "b = 2", "b = 20", 1) || !strings.Contains(e.Partial, "change 1 (old lines 1-1)") || len(e.Hunks) != 1 {
		t.Errorf("got %q, %q", e.Content, e.Partial)
	}
	if err := keepHunks(&e, []int{5}); err != nil {
		t.Errorf("keeping everything there is should be a no-op, got %v", err)
	}
}

func TestLeftoverNote(t *testing.T) {
	after := "from utils import parse_code\n\ndef f(o):\n    return parse_sku(o)\n"
	if got := leftoverNote("main.py", "from utils import parse_sku", "from utils import parse_code", after); !strings.Contains(got, "parse_sku (line 4)") {
		t.Errorf("got %q", got)
	}
	if got := leftoverNote("c.yaml", "  max: 25", "  max: 50", "db:\n  max: 50\n"); got != "" {
		t.Errorf("a value change leaves nothing behind, got %q", got)
	}
}
