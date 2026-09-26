package main

import (
	"fmt"
	"testing"
)

func TestPreferredOrAll(t *testing.T) {
	all := []OllamaModelInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}}

	if got := preferredOrAll(all, []string{"c", "a"}); len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("preferred subset: got %v", got)
	}
	if got := preferredOrAll(all, nil); len(got) != 3 {
		t.Errorf("no preference should show all, got %v", got)
	}
	if got := preferredOrAll(all, []string{"uninstalled"}); len(got) != 3 {
		t.Errorf("stale preference should fall back to all, got %v", got)
	}
}

func TestCleanGeneratedTitle(t *testing.T) {
	cases := map[string]string{
		"Go HTTP 404 Static Files":           "Go HTTP 404 Static Files",
		"  \"Autumn Rain Haiku.\"  ":         "Autumn Rain Haiku",
		"Title: Battery Drain\nExplanation…": "Battery Drain",
		"**Python KeyError**":                "Python KeyError",
		"   ":                                "",
	}
	for in, want := range cases {
		if got := cleanGeneratedTitle(in); got != want {
			t.Errorf("cleanGeneratedTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTimelineShowsTagsAndRecallAsTyped(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: formatSearchResults("latest ollama", []SearchResult{{Title: "t", URL: "https://x.test"}})},
		{Role: "user", Content: "latest ollama"},
		{Role: "assistant", Content: "0.34.2"},
		{Role: "system", Content: fmt.Sprintf(recallHeaderFormat, "ollama") + "\n- prefers short answers"},
		{Role: "user", Content: "ollama"},
		{Role: "assistant", Content: "noted"},
		{Role: "user", Content: "plain question"},
	}
	got := buildTimeline(messages, nil)
	want := []struct{ kind, content string }{
		{"user", "@web latest ollama"},
		{"sources", ""},
		{"assistant", "0.34.2"},
		{"user", "@memory ollama"},
		{"system", "Recalled memories for \"ollama\"\n- prefers short answers"},
		{"assistant", "noted"},
		{"user", "plain question"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Kind != w.kind || (w.content != "" && got[i].Content != w.content) {
			t.Errorf("item %d: got %s %q, want %s %q", i, got[i].Kind, got[i].Content, w.kind, w.content)
		}
	}
}
