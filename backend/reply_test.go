package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplyInsteadDeniesWithoutAModelTurn(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	createUser(db, "alice", "Alice", "a@x.io", "h")
	createConversation(db, "c1", "alice", "m")
	insertMessage(db, "c1", "user", "restart plasma")
	insertToolCallMessage(db, "c1", "", "", `[{"id":"call1","function":{"name":"run_shell","arguments":{"command":"killall plasmashell"}}}]`)
	createCommand(db, "cmd1", "c1", "call1", "killall plasmashell", "/tmp")

	u, _ := getUserByID(db, "alice")
	r := httptest.NewRequest(http.MethodPost, "/api/conversations/c1/commands/cmd1/deny?reply=1", nil)
	r.SetPathValue("id", "c1")
	r.SetPathValue("cmdId", "cmd1")
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
	w := httptest.NewRecorder()
	s.handleDenyCommand(w, r)

	// No Ollama is configured: a model turn would have written an error.
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("want 204 and no stream, got %d %q", w.Code, w.Body.String())
	}
	c, _ := getConversation(db, "c1", "alice")
	last := c.Messages[len(c.Messages)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "next message") {
		t.Fatalf("want the reply-instead tool result last, got %s %q", last.Role, last.Content)
	}
	if pending, _ := getPendingCommand(db, "c1"); pending != nil {
		t.Error("the command should no longer be pending")
	}
	timeline := buildTimeline(c.Messages, nil, nil)
	if card := timeline[len(timeline)-1]; card.CommandStatus != "denied" || card.CommandResult != "" {
		t.Errorf("a denied card should show only its status, got %q / %q", card.CommandStatus, card.CommandResult)
	}
}
