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

	if approvals, total := consecutiveToolCycles(messages); approvals != 2 || total != 2 {
		t.Fatalf("expected 2 approved cycles of 2, got %d of %d", approvals, total)
	}

	messages = append(messages,
		makeToolCallMessage(t, "read_file a.go"), Message{Role: "tool", Content: "[read a.go: lines 1-3 of 3]"},
		makeToolCallMessage(t, "edit_file a.go"), Message{Role: "tool", Content: "[PRECONDITION FAILED: old_text was not found]"})
	if approvals, total := consecutiveToolCycles(messages); approvals != 2 || total != 4 {
		t.Fatalf("reads and refused proposals shouldn't count as approvals: got %d of %d", approvals, total)
	}

	messages = append(messages, Message{Role: "user", Content: "try a different approach"})
	if approvals, total := consecutiveToolCycles(messages); approvals != 0 || total != 0 {
		t.Fatalf("expected counts to reset after a new user message, got %d of %d", approvals, total)
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

	result := buildOptimizedHistory(messages, nil, "", 3000, 1)

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

	// The protected window alone is over budget here, so the old cycles are
	// condensed and then dropped outright - either way the model is told.
	foundNote := false
	for _, m := range result {
		if m.Role == "system" && (strings.Contains(m.Content, "condensed") || strings.Contains(m.Content, "omitted")) {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatal("expected a condensed or omitted note for the old tool cycles")
	}
}

func TestBuildOptimizedHistory_NeverSplitsToolCallPair(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "goal"},
		makeToolCallMessage(t, "ls"),
		{Role: "tool", Content: "output", ToolCallID: strPtr("call1")},
	}

	result := buildOptimizedHistory(messages, nil, "", 3000, 1)

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

	result := buildOptimizedHistory(messages, nil, "", 3000, 1)

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
	result := buildOptimizedHistory(messages, nil, dir, 3000, 1)
	if len(result[0].Images) != 0 || !strings.Contains(result[0].Content, "[Earlier image: old.png") {
		t.Errorf("old image should be replaced by a note, got images=%d content=%q", len(result[0].Images), result[0].Content)
	}
	if len(result[2].Images) != 1 || strings.Contains(result[2].Content, "Earlier image") {
		t.Errorf("latest image should still be sent, got images=%d content=%q", len(result[2].Images), result[2].Content)
	}
}

func TestBuildOptimizedHistory_SummariesReplaceCoveredMessages(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "system", Content: "folder manifest"},
		{ID: 2, Role: "user", Content: "ORIGINAL GOAL"},
		{ID: 3, Role: "assistant", Content: "old reply one"},
		{ID: 4, Role: "user", Content: "old question"},
		{ID: 5, Role: "assistant", Content: "old reply two"},
		{ID: 6, Role: "user", Content: "current question"},
	}
	summaries := []Summary{{FirstMessageID: 1, LastMessageID: 5, Content: "- user asked X"}}
	result := buildOptimizedHistory(messages, summaries, "", 3000, 1)

	var got []string
	for _, m := range result {
		got = append(got, m.Content)
	}
	joined := strings.Join(got, "|")
	if strings.Contains(joined, "old reply") || strings.Contains(joined, "old question") {
		t.Fatalf("covered messages should be replaced by the summary: %q", joined)
	}
	if len(result) != 4 || result[0].Content != "folder manifest" || result[1].Content != "ORIGINAL GOAL" ||
		!strings.Contains(result[2].Content, "- user asked X") || result[3].Content != "current question" {
		t.Fatalf("expected manifest, goal, summary, current question in order, got %q", joined)
	}
	if n := condensedCount(messages, summaries); n != 3 {
		t.Fatalf("condensedCount = %d, want 3: the manifest and goal stay verbatim", n)
	}
}

func TestBuildOptimizedHistory_DropsOldestToFitBudget(t *testing.T) {
	big := strings.Repeat("x", 4000) // ~1000 tokens each
	messages := []Message{{Role: "user", Content: "goal"}}
	for i := 0; i < 4; i++ {
		messages = append(messages, Message{Role: "user", Content: big}, Message{Role: "assistant", Content: big})
	}
	for i := 0; i < protectedWindow; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent"})
	}
	result := buildOptimizedHistory(messages, nil, "", 300, 1)

	total := 0
	for _, m := range result {
		total += tokenCounter(1).text(m.Content)
	}
	if total > 300 {
		t.Fatalf("history should fit the budget, got %d tokens", total)
	}
	if result[0].Content != "goal" || !strings.Contains(result[1].Content, "omitted") || result[len(result)-1].Content != "recent" {
		t.Fatalf("expected goal, an omitted note, ..., recent; got first=%q second=%q", result[0].Content, result[1].Content)
	}
}

func TestBuildOptimizedHistory_CutsProtectedToolOutputToFit(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "goal"},
		makeToolCallMessage(t, "cat big.go"),
		{Role: "tool", Content: "[exit code: 0]\n" + strings.Repeat("code line\n", 2000), ToolCallID: strPtr("call1")},
	}
	result := buildOptimizedHistory(messages, nil, "", 1000, 1)
	total := 0
	for _, m := range result {
		total += tokenCounter(1).text(m.Content)
	}
	last := result[len(result)-1]
	if total > 1000 || last.Role != "tool" || !strings.HasPrefix(last.Content, "[exit code: 0]") || !strings.Contains(last.Content, "cut to fit") {
		t.Fatalf("protected tool output should be cut to fit, keeping its status line; total=%d", total)
	}
}

func TestNumCtxForFollowsSettingModelAndLimit(t *testing.T) {
	s := &Server{ollama: NewOllamaClient(), defaultOllamaURL: "http://ollama.test"}
	store := func(name, host string, limit int) {
		s.ollama.models.Store("http://ollama.test "+name, OllamaModelInfo{Name: name, RemoteHost: host, Details: OllamaModelDetails{ContextLength: limit}})
	}
	store("qwen2.5-3b", "", 32768)
	store("tiny", "", 2048)
	store("big:cloud", "https://ollama.com", 262144)
	folder := "/tmp/x"
	custom := func(n int) *User { return &User{NumCtx: &n} }
	cloudCustom := func(n int) *User { return &User{CloudNumCtx: &n} }

	for _, tc := range []struct {
		name string
		user *User
		c    Conversation
		want int
	}{
		{"local default", &User{}, Conversation{Model: "qwen2.5-3b"}, defaultNumCtx},
		{"local with a folder", &User{}, Conversation{Model: "qwen2.5-3b", AttachedFolder: &folder}, boostedNumCtx},
		{"cloud", &User{}, Conversation{Model: "big:cloud"}, cloudNumCtx},
		{"custom", custom(16000), Conversation{Model: "qwen2.5-3b", AttachedFolder: &folder}, 16000},
		{"custom above the model's limit", custom(100000), Conversation{Model: "qwen2.5-3b"}, 32768},
		{"default above the model's limit", &User{}, Conversation{Model: "tiny", AttachedFolder: &folder}, 2048},
		{"local setting leaves cloud alone", custom(8192), Conversation{Model: "big:cloud"}, cloudNumCtx},
		{"cloud setting", cloudCustom(131072), Conversation{Model: "big:cloud"}, 131072},
		{"cloud setting leaves local alone", cloudCustom(131072), Conversation{Model: "qwen2.5-3b"}, defaultNumCtx},
	} {
		if got := s.numCtxFor(tc.user, tc.c); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
