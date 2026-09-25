package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
