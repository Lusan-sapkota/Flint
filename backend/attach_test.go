package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangingFolderRewritesTheManifestInPlace(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}

	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for _, d := range []string{first, second} {
		os.Mkdir(d, 0o755)
		os.WriteFile(filepath.Join(d, filepath.Base(d)+".txt"), []byte("contents of "+filepath.Base(d)), 0o644)
	}
	createUser(db, "alice", "alice", "a@x.io", "h")
	createUser(db, "bob", "bob", "b@x.io", "h")
	createConversation(db, "c1", "alice", "m")

	attach := func(userID, folder string) int {
		u, _ := getUserByID(db, userID)
		r := httptest.NewRequest(http.MethodPost, "/api/conversations/c1/attach", strings.NewReader(`{"folder":"`+folder+`"}`))
		r.SetPathValue("id", "c1")
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		s.handleAttachFolder(w, r)
		return w.Code
	}
	manifests := func() []Message {
		c, _ := getConversation(db, "c1", "alice")
		var out []Message
		for _, m := range c.Messages {
			if m.Role == "system" && strings.HasPrefix(m.Content, manifestPrefix) {
				out = append(out, m)
			}
		}
		return out
	}

	if code := attach("alice", first); code != http.StatusOK {
		t.Fatalf("first attach: %d", code)
	}
	insertMessage(db, "c1", "user", "what's in here?")
	before := manifests()

	if code := attach("bob", second); code != http.StatusNotFound {
		t.Fatalf("another account: got %d, want 404", code)
	}
	if code := attach("alice", second); code != http.StatusOK {
		t.Fatalf("change: %d", code)
	}
	after := manifests()
	if len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatalf("want the one manifest rewritten in place, got %d manifests (ids %v -> %v)", len(after), before[0].ID, after)
	}
	if !strings.Contains(after[0].Content, "contents of second") || strings.Contains(after[0].Content, "contents of first") {
		t.Fatalf("manifest should hold only the new folder: %q", after[0].Content)
	}
	if c, _ := getConversation(db, "c1", "alice"); *c.AttachedFolder != second {
		t.Errorf("attached folder = %s, want %s", *c.AttachedFolder, second)
	}

	createCommand(db, "cmd1", "c1", "t1", "ls", second)
	if code := attach("alice", first); code != http.StatusConflict {
		t.Fatalf("with a pending command: got %d, want 409", code)
	}
}
