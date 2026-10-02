package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMaxInputFitsAfterCompaction(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	createUser(db, "alice", "Alice", "a@x.io", "h")
	createConversation(db, "c1", "alice", "m")
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		insertMessage(db, "c1", role, strings.Repeat("x", 400))
	}
	u, _ := getUserByID(db, "alice")
	c, _ := getConversation(db, "c1", "alice")
	count := tokenCounter(c.TokenRatio)

	numCtx := 4096
	_, _, _, budget := s.turnSetup(u, c, numCtx)
	limit := s.maxInputTokens(u, c, numCtx)
	if history := count.messages(buildOptimizedHistory(c.Messages, nil, "", 1<<30, count)); limit <= budget-history {
		t.Fatalf("older history should not count against the limit: limit %d, budget %d, history %d", limit, budget, history)
	}

	insertMessage(db, "c1", "user", strings.Repeat("y", int(float64(limit-perMessageTokens)*charsPerToken)))
	c, _ = getConversation(db, "c1", "alice")
	if got := count.messages(buildOptimizedHistory(c.Messages, nil, "", budget, count)); got > budget {
		t.Errorf("a message at the limit should fit: %d tokens sent, budget %d", got, budget)
	}
	if after := s.maxInputTokens(u, c, numCtx); after >= limit {
		t.Errorf("a big protected message should shrink the next limit: %d -> %d", limit, after)
	}
}
