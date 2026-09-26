package main

import (
	"math"
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
	if !strings.Contains(out, "golang") || !strings.Contains(out, "Go Programming Language") || !strings.Contains(out, "https://go.dev") {
		t.Fatalf("formatted output missing expected content: %q", out)
	}
}

func TestSavedSearchShowsAsSourcesAfterTheQuestion(t *testing.T) {
	results := []SearchResult{
		{Title: "Nepal PM sworn in", URL: "https://example.test/a", Snippet: "text"},
		{Title: "Profile", URL: "https://example.test/b", Snippet: "more\nlines"},
	}
	saved := formatSearchResults(`who is "PM" of Nepal`, results)
	query, got, ok := parseSearchResults(saved)
	if !ok || query != `who is "PM" of Nepal` || len(got) != 2 || got[0].Title != "Nepal PM sworn in" || got[1].URL != "https://example.test/b" {
		t.Fatalf("round trip failed: ok=%v query=%q got=%+v", ok, query, got)
	}
	if _, _, ok := parseSearchResults("Summary of the earlier part"); ok {
		t.Error("an ordinary system message must not parse as search results")
	}

	timeline := buildTimeline([]Message{
		{Role: "system", Content: saved},
		{Role: "user", Content: "who is PM of Nepal"},
		{Role: "assistant", Content: "answer"},
	}, nil)
	kinds := []string{}
	for _, it := range timeline {
		kinds = append(kinds, it.Kind)
	}
	if strings.Join(kinds, ",") != "user,sources,assistant" || len(timeline[1].Sources) != 2 {
		t.Fatalf("expected user, sources, assistant; got %v", kinds)
	}
}
