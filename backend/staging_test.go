package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCloudEditsAreStagedThenReviewed(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := t.TempDir()
	src := "a = 1\nx\nx\nx\nx\nx\nx\nx\nb = 2\n"
	os.WriteFile(filepath.Join(folder, "f.py"), []byte(src), 0o644)

	// Two edits to the same file, the second planned against the first, then a command, then a reply.
	steps := []string{
		`{"name":"edit_file","arguments":{"path":"f.py","old_text":"a = 1","new_text":"a = 10"}}`,
		`{"name":"edit_file","arguments":{"path":"f.py","old_text":"a = 10\nx","new_text":"a = 10\nx"}}`,
		`{"name":"edit_file","arguments":{"path":"f.py","old_text":"b = 2","new_text":"b = 20"}}`,
		`{"name":"run_shell","arguments":{"command":"python3 f.py"}}`,
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "big:cloud", "remote_host": "https://ollama.com:443"}}})
		case "/api/chat":
			if n := int(calls.Add(1)); n <= len(steps) {
				fmt.Fprintf(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":%s}]},"done":true}`+"\n", steps[n-1])
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
	createConversation(db, "c1", "alice", "big:cloud")
	setAttachedFolder(db, "c1", folder)
	req := func(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.SetPathValue("id", "c1")
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}

	// The second edit is a no-op on the staged text, so it is refused against the staged version, not the disk.
	body := req(s.handlePostMessage, `{"content":"change a and b"}`).Body.String()
	if strings.Count(body, "<<<STAGED>>>") != 2 || !strings.Contains(body, "makes no change") || !strings.Contains(body, "[Not run: this turn's edits are staged") {
		t.Fatalf("want two staged edits, one refused, the command not run: %q", body)
	}
	if got, _ := os.ReadFile(filepath.Join(folder, "f.py")); string(got) != src {
		t.Fatalf("nothing should be written before review: %q", got)
	}
	if w := req(s.handlePostMessage, `{"content":"next"}`); w.Code != http.StatusConflict {
		t.Errorf("a new message before review should be refused, got %d", w.Code)
	}

	files, _ := buildReview(db, "c1")
	if len(files) != 1 || len(files[0].hunks) != 2 {
		t.Fatalf("want one file with two changes: %+v", files)
	}
	if w := req(s.handleDecideReview, `{"write":true,"keep":{"f.py":[1]}}`); w.Code != http.StatusOK {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(folder, "f.py")); string(got) != strings.Replace(src, "b = 2", "b = 20", 1) {
		t.Errorf("only the kept change should be written: %q", got)
	}
	convo, _ := getConversation(db, "c1", "alice")
	results := 0
	for _, m := range convo.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "reviewed your staged edits") && strings.Contains(m.Content, "Not written: change 1") {
			results++
		}
	}
	if results != 2 {
		t.Errorf("both staged results should be rewritten with the outcome, got %d", results)
	}
	if left, _ := stagedEdits(db, "c1"); len(left) != 0 {
		t.Errorf("no edit should stay staged: %d", len(left))
	}
}
