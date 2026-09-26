package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChatReportsContextOverflowSize(t *testing.T) {
	// The body Ollama 0.34.2 actually sent for an oversized prompt.
	body := `{"error":"{\"error\":{\"code\":400,\"message\":\"request (12628 tokens) exceeds the available context size (8192 tokens), try increasing it\",\"type\":\"exceed_context_size_error\",\"n_prompt_tokens\":12628,\"n_ctx\":8192}}"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := NewOllamaClient().StreamChat(t.Context(), srv.URL, "m", nil, nil, nil, nil, func(string) {}, func(string) {})
	var overflow *contextOverflowError
	if !errors.As(err, &overflow) || overflow.promptTokens != 12628 {
		t.Fatalf("expected a context overflow of 12628 tokens, got %v", err)
	}
}

func TestDescribeOllamaError(t *testing.T) {
	c := NewOllamaClient()
	_, err := c.ListModels(context.Background(), "http://127.0.0.1:1")
	if got := describeOllamaError(err, "http://127.0.0.1:1"); !strings.Contains(got, "Can't reach Ollama at http://127.0.0.1:1") {
		t.Errorf("connection failure: got %q", got)
	}
	if got := describeOllamaError(errors.New("model not found"), "x"); got != "model not found" {
		t.Errorf("other errors should pass through, got %q", got)
	}
}

func TestSystemFallbackOnlyForModelsThatRefuse(t *testing.T) {
	c := NewOllamaClient()
	history := []OllamaMessage{{Role: "system", Content: "manifest"}, {Role: "user", Content: "hi"}, {Role: "system", Content: "Saved memories"}, {Role: "user", Content: "lcore"}}
	sends := 0
	strict := func(m []OllamaMessage) (string, error) {
		sends++
		for i, msg := range m {
			if i > 0 && msg.Role == "system" {
				return "", fmt.Errorf("ollama returned status 500: Jinja Exception: %s.", systemNotFirst)
			}
		}
		return m[0].Role + "," + m[2].Role, nil
	}

	if got, err := withSystemFallback(c, "qwen3.5-4b", history, strict); err != nil || got != "system,user" || sends != 2 {
		t.Fatalf("want a resend with the later system message as user, got %q %v after %d sends", got, err, sends)
	}
	if history[2].Role != "system" {
		t.Error("the caller's history must not be changed")
	}
	sends = 0
	if _, err := withSystemFallback(c, "qwen3.5-4b", history, strict); err != nil || sends != 1 {
		t.Errorf("a model already known to refuse must go straight to the fallback, got %d sends", sends)
	}

	lenient := func(m []OllamaMessage) (string, error) { return m[2].Role, nil }
	if got, _ := withSystemFallback(c, "qwen2.5-3b-instruct", history, lenient); got != "system" {
		t.Errorf("a model that accepts later system messages must get them unchanged, got %q", got)
	}
}
