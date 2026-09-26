package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripWebFlag(t *testing.T) {
	cases := []struct {
		input     string
		wantIsWeb bool
		wantQuery string
	}{
		{"@web latest golang release", true, "latest golang release"},
		{"@WEB latest golang release", true, "latest golang release"},
		{"  @web   what is rust  ", true, "what is rust"},
		{"@web", false, "@web"},
		{"just a normal message", false, "just a normal message"},
		{"tell me about @web scraping", false, "tell me about @web scraping"},
	}

	for _, c := range cases {
		isWeb, query := stripWebFlag(c.input)
		if isWeb != c.wantIsWeb || query != c.wantQuery {
			t.Errorf("stripWebFlag(%q) = (%v, %q), want (%v, %q)", c.input, isWeb, query, c.wantIsWeb, c.wantQuery)
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	identical := cosineSimilarity([]float64{1, 2, 3}, []float64{1, 2, 3})
	if math.Abs(identical-1.0) > 1e-9 {
		t.Errorf("expected identical vectors to have similarity 1.0, got %f", identical)
	}

	orthogonal := cosineSimilarity([]float64{1, 0}, []float64{0, 1})
	if math.Abs(orthogonal) > 1e-9 {
		t.Errorf("expected orthogonal vectors to have similarity 0, got %f", orthogonal)
	}

	opposite := cosineSimilarity([]float64{1, 0}, []float64{-1, 0})
	if math.Abs(opposite-(-1.0)) > 1e-9 {
		t.Errorf("expected opposite vectors to have similarity -1.0, got %f", opposite)
	}
}

func TestFormatSearchResults(t *testing.T) {
	results := []SearchResult{
		{Title: "Go Programming Language", URL: "https://go.dev", Snippet: "An open source language"},
	}
	out := formatSearchResults("golang", results)
	if strings.Contains(out, "Published") {
		t.Error("with no dated result, the date note must be left out, or the model invents a date")
	}
	if !strings.Contains(formatSearchResults("q", []SearchResult{{Title: "t", URL: "u", Date: "2026-08-28"}}), "Published: 2026-08-28") {
		t.Error("a dated result must show its date")
	}
	if strings.Contains(formatSearchResults("q", []SearchResult{{Title: "a", URL: "u", Date: "2026-08-28"}, {Title: "b", URL: "v"}}), "Published") {
		t.Error("with only some results dated, dates must be left out, or the model trusts the dated one")
	}
	if !strings.Contains(out, "golang") || !strings.Contains(out, "Go Programming Language") || !strings.Contains(out, "https://go.dev") {
		t.Fatalf("formatted output missing expected content: %q", out)
	}
}

func TestSavedSearchShowsAsSourcesAfterTheQuestion(t *testing.T) {
	results := []SearchResult{
		{Title: "Nepal PM sworn in", URL: "https://example.test/a", Snippet: "text", Date: "2026-09-01"},
		{Title: "Profile", URL: "https://example.test/b", Snippet: "more\nlines", Date: "2026-08-28"},
	}
	saved := formatSearchResults(`who is "PM" of Nepal`, results)
	query, got, ok := parseSearchResults(saved)
	if !ok || query != `who is "PM" of Nepal` || len(got) != 2 || got[0].Title != "Nepal PM sworn in" || got[1].URL != "https://example.test/b" {
		t.Fatalf("round trip failed: ok=%v query=%q got=%+v", ok, query, got)
	}
	if got[0].Date != "2026-09-01" || got[1].Date != "2026-08-28" || got[1].Snippet != "more\nlines" {
		t.Errorf("dates must round trip without leaking into snippets, got %+v", got)
	}
	if _, old, _ := parseSearchResults("Web search results for \"q\":\n\n1. T\nhttps://x.test\nsnippet\n\n"); len(old) != 1 || old[0].Snippet != "snippet" || old[0].Date != "" {
		t.Errorf("a search saved before dates existed must still parse, got %+v", old)
	}
	if _, _, ok := parseSearchResults("Summary of the earlier part"); ok {
		t.Error("an ordinary system message must not parse as search results")
	}

	timeline := buildTimeline([]Message{
		{Role: "system", Content: saved},
		{Role: "user", Content: "who is PM of Nepal"},
		{Role: "assistant", Content: "answer"},
	}, nil, nil)
	kinds := []string{}
	for _, it := range timeline {
		kinds = append(kinds, it.Kind)
	}
	if strings.Join(kinds, ",") != "user,sources,assistant" || len(timeline[1].Sources) != 2 {
		t.Fatalf("expected user, sources, assistant; got %v", kinds)
	}
}

func TestListWebSearchesIsScopedAndParsed(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", "h")
		createConversation(db, u+"-chat", u, "m")
	}
	insertMessage(db, "alice-chat", "user", "hi")
	insertMessage(db, "alice-chat", "system", formatSearchResults(`who is "PM" of Nepal`, []SearchResult{{Title: "t", URL: "https://x.test"}}))
	insertMessage(db, "alice-chat", "system", "folder manifest")
	insertMessage(db, "bob-chat", "system", formatSearchResults("bob's query", nil))

	got, err := listWebSearches(db, "alice", 10)
	if err != nil || len(got) != 1 || got[0].Query != `who is "PM" of Nepal` || got[0].ConversationID != "alice-chat" {
		t.Fatalf("want alice's one search with its query intact, got %+v err=%v", got, err)
	}
	if n, _ := countWebSearchesSince(db, "alice", 0); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	if n, _ := countWebSearchesSince(db, "alice", got[0].At+1); n != 0 {
		t.Errorf("count after the search = %d, want 0", n)
	}
}

func TestIsRankingModel(t *testing.T) {
	for name, want := range map[string]bool{
		"nomic-embed-text":           true,
		"nomic-embed-text:latest":    true,
		"nomic-embed-text-v2:latest": false,
		"qwen2.5-3b-instruct:latest": false,
	} {
		if got := isRankingModel(name); got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}
