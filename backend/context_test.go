package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func makeToolCallMessage(t *testing.T, command string) Message {
	t.Helper()
	calls := []OllamaToolCall{{ID: "call1", Function: struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: "run_shell", Arguments: json.RawMessage(`{"command":"` + command + `"}`)}}}
	data, err := json.Marshal(calls)
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	s := string(data)
	return Message{Role: "assistant", Content: "", ToolCalls: &s}
}

func TestBuildOptimizedHistory_ProtectsGoalAndRecentWindow(t *testing.T) {
	longOutput := strings.Repeat("line of output\n", 400) // ~6000 chars, well over the token budget alone

	messages := []Message{
		{Role: "system", Content: "Attached folder manifest..."},
		{Role: "user", Content: "ORIGINAL GOAL: fix the bug"},
	}
	for i := 0; i < 5; i++ {
		messages = append(messages, makeToolCallMessage(t, "ls"))
		messages = append(messages, Message{Role: "tool", Content: longOutput, ToolCallID: strPtr("call1")})
	}
	messages = append(messages, Message{Role: "assistant", Content: "final answer"})

	result := buildOptimizedHistory(messages, "")

	if result[0].Role != "system" {
		t.Fatalf("expected system message preserved first, got role %q", result[0].Role)
	}
	foundGoal := false
	for _, m := range result {
		if m.Content == "ORIGINAL GOAL: fix the bug" {
			foundGoal = true
		}
	}
	if !foundGoal {
		t.Fatal("original goal message was not preserved verbatim")
	}

	last := result[len(result)-1]
	if last.Content != "final answer" {
		t.Fatalf("expected last message to be the final answer verbatim, got %q", last.Content)
	}

	totalChars := 0
	for _, m := range result {
		totalChars += len(m.Content)
	}
	if totalChars >= len(messages[3].Content)*5 {
		t.Fatalf("expected condensation to significantly shrink total size, got %d chars", totalChars)
	}

	foundCondensedNote := false
	for _, m := range result {
		if m.Role == "system" && strings.Contains(m.Content, "condensed") {
			foundCondensedNote = true
		}
	}
	if !foundCondensedNote {
		t.Fatal("expected at least one condensed tool-cycle note in the output")
	}
}

func TestBuildOptimizedHistory_NeverSplitsToolCallPair(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "goal"},
		makeToolCallMessage(t, "ls"),
		{Role: "tool", Content: "output", ToolCallID: strPtr("call1")},
	}

	result := buildOptimizedHistory(messages, "")

	for i, m := range result {
		if m.Role == "tool" {
			if i == 0 || result[i-1].Role != "assistant" || len(result[i-1].ToolCalls) == 0 {
				t.Fatalf("tool message at index %d has no preceding assistant tool_call message", i)
			}
		}
	}
}

func TestDecayWeight_MonotonicallyDecreasing(t *testing.T) {
	prev := decayWeight(0)
	for turnsAgo := 1; turnsAgo <= 20; turnsAgo++ {
		w := decayWeight(turnsAgo)
		if w >= prev {
			t.Fatalf("decay weight should strictly decrease with age: turnsAgo=%d weight=%f >= prev=%f", turnsAgo, w, prev)
		}
		prev = w
	}
}

func strPtr(s string) *string { return &s }
