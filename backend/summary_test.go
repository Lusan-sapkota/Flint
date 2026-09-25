package main

import (
	"strings"
	"testing"
)

func TestNextChunk_StopsBeforeCurrentTurnAndKeepsToolPairs(t *testing.T) {
	big := strings.Repeat("y", 400)
	messages := []Message{{ID: 1, Role: "user", Content: "goal"}}
	id := int64(2)
	add := func(m Message) {
		m.ID = id
		id++
		messages = append(messages, m)
	}
	for i := 0; i < 6; i++ {
		add(Message{Role: "user", Content: big})
		add(makeToolCallMessage(t, "ls"))
		add(Message{Role: "tool", Content: big, ToolCallID: strPtr("call1")})
	}
	add(Message{Role: "user", Content: "current turn"})

	chunk, ok := nextChunk(messages, nil, 1, 300)
	if !ok {
		t.Fatal("expected a chunk")
	}
	if chunk[0].ID == 1 {
		t.Fatal("the first user message is protected and must never be summarized")
	}
	if last := chunk[len(chunk)-1]; last.Role == "assistant" && last.ToolCalls != nil {
		t.Fatal("a chunk must not end between a tool call and its result")
	}

	all, ok := nextChunk(messages, nil, 1, 1<<30)
	if ok || all != nil {
		t.Fatal("no chunk should be returned until there is enough to summarize")
	}
	summaries := []Summary{{FirstMessageID: 2, LastMessageID: messages[len(messages)-protectedWindow-1].ID}}
	if _, ok := nextChunk(messages, summaries, 1, 1); ok {
		t.Fatal("nothing inside the recent window should be summarized")
	}
}

func TestMergeCandidates(t *testing.T) {
	var s []Summary
	for i := 0; i < summaryFanout; i++ {
		s = append(s, Summary{ID: int64(i), Level: 0})
	}
	if mergeCandidates(s) != nil {
		t.Fatal("a level at the fanout limit should not merge yet")
	}
	s = append(s, Summary{ID: 99, Level: 0})
	got := mergeCandidates(s)
	if len(got) != summaryFanout || got[0].ID != 0 {
		t.Fatalf("expected the oldest %d summaries to merge, got %+v", summaryFanout, got)
	}
}

func TestUserNotesAreVerbatimAndReachHistory(t *testing.T) {
	chunk := []Message{
		{Role: "user", Content: "staging runs on   port 9123\nmanager is Tom"},
		{Role: "assistant", Content: "noted"},
		{Role: "user", Content: strings.Repeat("z", userNoteChars+50)},
	}
	notes := userNotes(chunk)
	if !strings.Contains(notes, "- staging runs on port 9123 manager is Tom\n") || strings.Contains(notes, "noted") {
		t.Fatalf("user messages should be copied verbatim, assistant ones left out: %q", notes)
	}
	if !strings.Contains(notes, strings.Repeat("z", userNoteChars)+"...") {
		t.Fatal("long user messages should be clipped")
	}
	got := formatSummaries([]Summary{{Content: "- found X", UserNotes: notes}})
	if !strings.Contains(got, "port 9123") || !strings.Contains(got, "- found X") {
		t.Fatalf("both user notes and model notes should reach the history: %q", got)
	}
}
