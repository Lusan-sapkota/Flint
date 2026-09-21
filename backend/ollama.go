package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OllamaToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type OllamaTool struct {
	Type     string             `json:"type"`
	Function OllamaToolFunction `json:"function"`
}

type OllamaToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type OllamaMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []OllamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []OllamaTool    `json:"tools,omitempty"`
}

type ollamaChatChunk struct {
	Message OllamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Error   string        `json:"error"`
}

type ChatResult struct {
	Content   string
	ToolCalls []OllamaToolCall
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type OllamaClient struct {
	http *http.Client
}

func NewOllamaClient() *OllamaClient {
	return &OllamaClient{http: &http.Client{}}
}

func (c *OllamaClient) ListModels(ctx context.Context, baseURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

func (c *OllamaClient) StreamChat(ctx context.Context, baseURL, model string, messages []OllamaMessage, tools []OllamaTool, onToken func(string)) (ChatResult, error) {
	body, err := json.Marshal(ollamaChatRequest{Model: model, Messages: messages, Stream: true, Tools: tools})
	if err != nil {
		return ChatResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResult{}, fmt.Errorf("contacting ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf, _ := readAll(resp.Body, 4096)
		return ChatResult{}, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, buf)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var full bytes.Buffer
	var toolCalls []OllamaToolCall
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var chunk ollamaChatChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return ChatResult{Content: full.String()}, fmt.Errorf("ollama error: %s", chunk.Error)
		}
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		if chunk.Message.Content != "" {
			full.WriteString(chunk.Message.Content)
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{Content: full.String(), ToolCalls: toolCalls}, err
	}

	return ChatResult{Content: full.String(), ToolCalls: toolCalls}, nil
}

func readAll(r interface{ Read([]byte) (int, error) }, max int) ([]byte, error) {
	buf := make([]byte, max)
	n, err := r.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	return nil, err
}
