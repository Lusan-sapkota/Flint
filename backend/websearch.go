package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	braveResultCount  = 10
	rankedResultCount = 3
	embeddingModel    = "nomic-embed-text"
)

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func stripWebFlag(content string) (bool, string) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(strings.ToLower(trimmed), "@web") {
		return false, content
	}
	rest := strings.TrimSpace(trimmed[len("@web"):])
	if rest == "" {
		return false, content
	}
	return true, rest
}

func braveSearch(ctx context.Context, apiKey, query string) ([]SearchResult, error) {
	endpoint := "https://api.search.brave.com/res/v1/web/search?" + url.Values{
		"q":     {query},
		"count": {fmt.Sprintf("%d", braveResultCount)},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting brave search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search returned status %d", resp.StatusCode)
	}

	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(parsed.Web.Results))
	for _, r := range parsed.Web.Results {
		results = append(results, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	return results, nil
}

func cosineSimilarity(a, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func rankByRelevance(ctx context.Context, ollama *OllamaClient, baseURL, query string, results []SearchResult, topK int) ([]SearchResult, error) {
	if len(results) <= topK {
		return results, nil
	}

	inputs := make([]string, 0, len(results)+1)
	// nomic-embed-text was trained with these task prefixes; without them
	// it ranked 62/90 instead of 70/90 on the E20 set.
	inputs = append(inputs, "search_query: "+query)
	for _, r := range results {
		inputs = append(inputs, "search_document: "+r.Title+" "+r.Snippet)
	}

	vectors, err := ollama.Embed(ctx, baseURL, embeddingModel, inputs)
	if err != nil || len(vectors) != len(inputs) {
		return nil, fmt.Errorf("embedding search results: %w", err)
	}

	queryVector := vectors[0]
	type scored struct {
		result SearchResult
		score  float64
	}
	ranked := make([]scored, len(results))
	for i, r := range results {
		ranked[i] = scored{result: r, score: cosineSimilarity(queryVector, vectors[i+1])}
	}

	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].score > ranked[j-1].score; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}

	top := make([]SearchResult, 0, topK)
	for i := 0; i < topK && i < len(ranked); i++ {
		top = append(top, ranked[i].result)
	}
	return top, nil
}

// isRankingModel reports whether an installed model's name (usually with a
// ":latest" tag) is the embedding model `@web` ranks results with.
func isRankingModel(name string) bool {
	return name == embeddingModel || strings.HasPrefix(name, embeddingModel+":")
}

const webResultsPrefix = "Web search results for "

func formatSearchResults(query string, results []SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, webResultsPrefix+"%q:\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n%s\n%s\n\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return b.String()
}

type sourceLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type sourcesItem struct {
	Query   string       `json:"query"`
	Sources []sourceLink `json:"sources"`
}

func sourcesView(query string, results []SearchResult) sourcesItem {
	item := sourcesItem{Query: query, Sources: []sourceLink{}}
	for _, r := range results {
		item.Sources = append(item.Sources, sourceLink{Title: r.Title, URL: r.URL})
	}
	return item
}

// parseSearchResults reads back a message written by formatSearchResults,
// so a saved search shows as its sources after a reload, like it did live.
func parseSearchResults(content string) (string, []SearchResult, bool) {
	header, body, _ := strings.Cut(content, "\n\n")
	quoted, found := strings.CutPrefix(strings.TrimSuffix(header, ":"), webResultsPrefix)
	if !found {
		return "", nil, false
	}
	query, err := strconv.Unquote(quoted)
	if err != nil {
		return "", nil, false
	}
	var results []SearchResult
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		lines := strings.SplitN(block, "\n", 3)
		if len(lines) < 2 {
			continue
		}
		_, title, _ := strings.Cut(lines[0], ". ")
		r := SearchResult{Title: title, URL: lines[1]}
		if len(lines) == 3 {
			r.Snippet = lines[2]
		}
		results = append(results, r)
	}
	return query, results, true
}

func (s *Server) handleListWebSearches(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	searches, err := listWebSearches(s.db, user.ID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	thisMonth, err := countWebSearchesSince(s.db, user.ID, monthStart.UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"searches": searches, "this_month": thisMonth})
}
