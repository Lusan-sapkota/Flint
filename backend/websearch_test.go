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
