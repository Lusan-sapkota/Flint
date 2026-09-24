package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
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
	Images     []string         `json:"images,omitempty"`
	ToolCalls  []OllamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []OllamaTool    `json:"tools,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
	Think    *bool           `json:"think,omitempty"`
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

type OllamaModelDetails struct {
	Format            string `json:"format,omitempty"`
	Family            string `json:"family,omitempty"`
	ParameterSize     string `json:"parameter_size,omitempty"`
	QuantizationLevel string `json:"quantization_level,omitempty"`
}

type OllamaModelInfo struct {
	Name       string             `json:"name"`
	Model      string             `json:"model,omitempty"`
	ModifiedAt string             `json:"modified_at,omitempty"`
	Size       int64              `json:"size,omitempty"`
	Digest     string             `json:"digest,omitempty"`
	Details    OllamaModelDetails `json:"details,omitempty"`
}

type ollamaTagsResponse struct {
	Models []OllamaModelInfo `json:"models"`
}

type OllamaClient struct {
	http *http.Client

	// Keyed by model digest: a given model build's capabilities never
	// change, so each is looked up via /api/show at most once.
	capsMu  sync.Mutex
	canChat map[string]bool
}

func NewOllamaClient() *OllamaClient {
	return &OllamaClient{http: &http.Client{}, canChat: map[string]bool{}}
}

func (c *OllamaClient) ListModels(ctx context.Context, baseURL string) ([]OllamaModelInfo, error) {
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
	return tags.Models, nil
}

// ChatModels drops models that can't hold a conversation, such as
// embedding-only ones (nomic-embed-text reports ["embedding"] only).
// /api/tags doesn't carry capabilities, so this asks /api/show per model.
func (c *OllamaClient) ChatModels(ctx context.Context, baseURL string, models []OllamaModelInfo) []OllamaModelInfo {
	var out []OllamaModelInfo
	for _, m := range models {
		if c.canChatWith(ctx, baseURL, m) {
			out = append(out, m)
		}
	}
	return out
}

func (c *OllamaClient) canChatWith(ctx context.Context, baseURL string, m OllamaModelInfo) bool {
	c.capsMu.Lock()
	v, ok := c.canChat[m.Digest]
	c.capsMu.Unlock()
	if ok {
		return v
	}

	raw, err := c.ShowModel(ctx, baseURL, m.Name)
	if err != nil {
		return true
	}
	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	// Older Ollama versions don't report capabilities at all; never hide a
	// model just because we couldn't tell.
	if json.Unmarshal(raw, &info) != nil || len(info.Capabilities) == 0 {
		return true
	}
	v = slices.Contains(info.Capabilities, "completion")

	c.capsMu.Lock()
	c.canChat[m.Digest] = v
	c.capsMu.Unlock()
	return v
}

func (c *OllamaClient) RunningModels(ctx context.Context, baseURL string) (json.RawMessage, error) {
	return c.doRaw(ctx, http.MethodGet, baseURL+"/api/ps", nil)
}

func (c *OllamaClient) ShowModel(ctx context.Context, baseURL, name string) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "model": name})
	return c.doRaw(ctx, http.MethodPost, baseURL+"/api/show", bytes.NewReader(body))
}

func (c *OllamaClient) DeleteModel(ctx context.Context, baseURL, name string) error {
	body, _ := json.Marshal(map[string]string{"name": name, "model": name})
	_, err := c.doRaw(ctx, http.MethodDelete, baseURL+"/api/delete", bytes.NewReader(body))
	return err
}

func (c *OllamaClient) Embed(ctx context.Context, baseURL, model string, inputs []string) ([][]float64, error) {
	data, err := c.doRaw(ctx, http.MethodPost, baseURL+"/api/embed", bytes.NewReader(mustMarshal(map[string]any{
		"model": model,
		"input": inputs,
	})))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Embeddings, nil
}

func mustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func (c *OllamaClient) PullModel(ctx context.Context, baseURL, name string, onProgress func(line []byte)) error {
	reqBody, _ := json.Marshal(map[string]any{"name": name, "model": name, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contacting ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf, _ := readAll(resp.Body, 4096)
		return fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, buf)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		onProgress(append([]byte(nil), line...))
	}
	return scanner.Err()
}

func (c *OllamaClient) doRaw(ctx context.Context, method, url string, body *bytes.Reader) (json.RawMessage, error) {
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, body)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting ollama: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, data)
	}
	return json.RawMessage(data), nil
}

// Non-streaming, with thinking disabled: a thinking model (qwen3.5)
// otherwise spends a small num_predict budget entirely on reasoning and
// returns empty content. Non-thinking models accept think:false fine.
func (c *OllamaClient) Chat(ctx context.Context, baseURL, model string, messages []OllamaMessage, options map[string]any) (string, error) {
	think := false
	body, err := json.Marshal(ollamaChatRequest{Model: model, Messages: messages, Options: options, Think: &think})
	if err != nil {
		return "", err
	}
	raw, err := c.doRaw(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	var resp ollamaChatChunk
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

func (c *OllamaClient) StreamChat(ctx context.Context, baseURL, model string, messages []OllamaMessage, tools []OllamaTool, options map[string]any, onToken func(string)) (ChatResult, error) {
	body, err := json.Marshal(ollamaChatRequest{Model: model, Messages: messages, Stream: true, Tools: tools, Options: options})
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
