package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestConsecutiveToolCycles(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "fix the failing test"},
		makeToolCallMessage(t, "go test ./..."),
		{Role: "tool", Content: "[FAILED, exit code: 1]\nsome error", ToolCallID: strPtr("call1")},
		makeToolCallMessage(t, "go test -run TestFoo ./..."),
		{Role: "tool", Content: "[FAILED, exit code: 1]\nanother error", ToolCallID: strPtr("call1")},
	}

	if got := consecutiveToolCycles(messages); got != 2 {
		t.Fatalf("expected 2 consecutive tool cycles, got %d", got)
	}

	messages = append(messages, Message{Role: "user", Content: "try a different approach"})
	if got := consecutiveToolCycles(messages); got != 0 {
		t.Fatalf("expected count to reset to 0 after a new user message, got %d", got)
	}
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

func TestEffectiveHalfLife_FailedResultsDecaySlowerThanExploring(t *testing.T) {
	failed := Message{Role: "tool", Content: "[FAILED, exit code: 1]\nsomething broke"}
	exploring := Message{Role: "tool", Content: "a.go\nb.go"}
	ordinary := Message{Role: "tool", Content: "some normal output"}

	failedHL := effectiveHalfLife(failed, "go build ./...")
	exploringHL := effectiveHalfLife(exploring, "ls -la")
	ordinaryHL := effectiveHalfLife(ordinary, "go build ./...")

	if failedHL <= ordinaryHL {
		t.Fatalf("expected a failed result to have a longer half-life than an ordinary one: failed=%f ordinary=%f", failedHL, ordinaryHL)
	}
	if exploringHL >= ordinaryHL {
		t.Fatalf("expected an exploring result to have a shorter half-life than an ordinary one: exploring=%f ordinary=%f", exploringHL, ordinaryHL)
	}

	turnsAgo := 6
	if decayWeightWithHalfLife(turnsAgo, failedHL) <= decayWeightWithHalfLife(turnsAgo, ordinaryHL) {
		t.Fatal("expected a failed result to retain more weight at the same distance than an ordinary one")
	}
	if decayWeightWithHalfLife(turnsAgo, exploringHL) >= decayWeightWithHalfLife(turnsAgo, ordinaryHL) {
		t.Fatal("expected an exploring result to retain less weight at the same distance than an ordinary one")
	}
}

func TestBuildOptimizedHistory_OnlyFirstSystemMessageProtected(t *testing.T) {
	oldSearchResult := strings.Repeat("stale web search noise ", 100)

	messages := []Message{
		{Role: "system", Content: "Attached folder manifest..."},
		{Role: "user", Content: "goal"},
		{Role: "system", Content: oldSearchResult},
	}
	for i := 0; i < 6; i++ {
		messages = append(messages, Message{Role: "user", Content: "filler"})
		messages = append(messages, Message{Role: "assistant", Content: "filler reply"})
	}

	result := buildOptimizedHistory(messages, "")

	if result[0].Content != "Attached folder manifest..." {
		t.Fatalf("expected the first system message preserved verbatim, got %q", result[0].Content)
	}

	for _, m := range result {
		if m.Role == "system" && strings.Contains(m.Content, "stale web search noise") && len(m.Content) >= len(oldSearchResult) {
			t.Fatal("expected the second (non-first) system message to decay like ordinary content, but it was preserved verbatim")
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

func TestOnlyLatestImageIsResent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	img := func(name string) []Attachment {
		return []Attachment{{MimeType: "image/png", Filename: name, FilePath: "a.png"}}
	}
	messages := []Message{
		{Role: "user", Content: "look at this", Attachments: img("old.png")},
		{Role: "assistant", Content: "a cat"},
		{Role: "user", Content: "and this", Attachments: img("new.png")},
		{Role: "assistant", Content: "a dog"},
		{Role: "user", Content: "compare them"},
	}
	result := buildOptimizedHistory(messages, dir)
	if len(result[0].Images) != 0 || !strings.Contains(result[0].Content, "[Earlier image: old.png") {
		t.Errorf("old image should be replaced by a note, got images=%d content=%q", len(result[0].Images), result[0].Content)
	}
	if len(result[2].Images) != 1 || strings.Contains(result[2].Content, "Earlier image") {
		t.Errorf("latest image should still be sent, got images=%d content=%q", len(result[2].Images), result[2].Content)
	}
}
