package main

import (
	"errors"
	"fmt"
	"strings"
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
	got := buildTimeline(messages, nil, nil)
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

func TestSavedMemoriesArePlacedWhereTheyWereSaved(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "first", CreatedAt: 100},
		{Role: "assistant", Content: "reply", CreatedAt: 200},
		{Role: "user", Content: "second", CreatedAt: 400},
	}
	saved := []Memory{{Content: "mid", CreatedAt: 300}, {Content: "end", CreatedAt: 500}}
	var got []string
	for _, it := range buildTimeline(messages, nil, saved) {
		got = append(got, it.Kind+":"+it.Content)
	}
	want := "user:first,assistant:reply,memorySaved:mid,user:second,memorySaved:end"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %s\nwant %s", strings.Join(got, ","), want)
	}
}

func TestCloudModelHostAndSignInError(t *testing.T) {
	if got := remoteHostName("https://ollama.com"); got != "ollama.com" {
		t.Errorf("remoteHostName = %q, want ollama.com", got)
	}
	msg := describeOllamaError(errors.New(`ollama returned status 401: {"error":"Unauthorized"}`), "http://localhost:11434")
	if !strings.Contains(msg, "ollama signin") {
		t.Errorf("a 401 must tell the user to sign in, got %q", msg)
	}
}
