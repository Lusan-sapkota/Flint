//go:build live

// Live checks against a real Ollama and a copy of a real Flint database.
// Not part of `go test ./...`; run with the live tag, see docs/testing.md:
//
//	LIVE_DB=/tmp/copy.db LIVE_MODEL=qwen2.5-3b-instruct go test -tags live -run TestLive -v .

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// liveConversation opens LIVE_DB and returns its first conversation, run
// with LIVE_MODEL, plus a Server talking to the local Ollama.
func liveConversation(t *testing.T) (*Server, *User, *ConversationWithMessages) {
	t.Helper()
	db, err := openDB(os.Getenv("LIVE_DB"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var cid, uid string
	if err := db.QueryRow(`SELECT id, user_id FROM conversations LIMIT 1`).Scan(&cid, &uid); err != nil {
		t.Fatalf("LIVE_DB needs at least one conversation: %v", err)
	}
	convo, err := getConversation(db, cid, uid)
	if err != nil {
		t.Fatal(err)
	}
	user, err := getUserByID(db, uid)
	if err != nil {
		t.Fatal(err)
	}
	if m := os.Getenv("LIVE_MODEL"); m != "" {
		convo.Model = m
	}
	return &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: getenv("OLLAMA_BASE_URL", "http://localhost:11434")}, user, convo
}

// TestLiveSummarizeChunk prints the level-0 summary the model writes for
// the conversation's oldest summarizable chunk. SHOW=1 also prints the
// transcript it was given.
func TestLiveSummarizeChunk(t *testing.T) {
	srv, user, convo := liveConversation(t)
	numCtx := numCtxFor(convo.Conversation)
	chunk, ok := nextChunk(convo.Messages, nil, tokenCounter(convo.TokenRatio), numCtx/4)
	if !ok {
		t.Skip("conversation is too short to have a summarizable chunk")
	}
	target := summaryTokens(numCtx)
	out, err := srv.condense(context.Background(), user, convo, fmt.Sprintf(summarizePrompt, target/20), "Transcript:\n"+formatTranscript(chunk), target)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("messages %d-%d\n\nuser notes (verbatim):\n%s\nmodel notes:\n%s\n", chunk[0].ID, chunk[len(chunk)-1].ID, userNotes(chunk), out)
	if os.Getenv("SHOW") != "" {
		fmt.Println("===== transcript\n" + formatTranscript(chunk))
	}
}

// TestLiveDumpHistory prints what would be sent to Ollama for the next
// turn: role, tool-call count and the start of each message.
func TestLiveDumpHistory(t *testing.T) {
	_, _, convo := liveConversation(t)
	numCtx := numCtxFor(convo.Conversation)
	h := buildOptimizedHistory(convo.Messages, convo.Summaries, "", numCtx-responseReserve, tokenCounter(convo.TokenRatio))
	fmt.Printf("token ratio %.2f, %d summaries, %d messages -> %d sent\n", convo.TokenRatio, len(convo.Summaries), len(convo.Messages), len(h))
	for _, m := range h {
		c := m.Content
		if len(c) > 110 {
			c = c[:110]
		}
		fmt.Printf("%-9s tc=%d %q\n", m.Role, len(m.ToolCalls), c)
	}
}
