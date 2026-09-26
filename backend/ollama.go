package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"
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
	Thinking   string           `json:"thinking,omitempty"`
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
	Message      OllamaMessage `json:"message"`
	Done         bool          `json:"done"`
	Error        string        `json:"error"`
	EvalCount    int           `json:"eval_count"`
	PromptCount  int           `json:"prompt_eval_count"`
	EvalDuration int64         `json:"eval_duration"`
}

type ChatResult struct {
	Content      string
	Thinking     string
	ToolCalls    []OllamaToolCall
	TokensPerSec float64
	// Prompt plus generated tokens: how much of the context window this
	// request actually occupied. prompt_eval_count is the whole prompt even
	// when part of it came from the cache.
	ContextUsed  int
	PromptTokens int
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
	capsMu sync.Mutex
	caps   map[string][]string
}

func NewOllamaClient() *OllamaClient {
	return &OllamaClient{http: &http.Client{}, caps: map[string][]string{}}
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
		// Older Ollama versions don't report capabilities at all (nil);
		// never hide a model just because we couldn't tell.
		if caps := c.Capabilities(ctx, baseURL, m); caps == nil || slices.Contains(caps, "completion") {
			out = append(out, m)
		}
	}
	return out
}

// Capabilities returns what Ollama reports for a model ("completion",
// "thinking", "vision", "tools", "embedding", ...), or nil if unknown.
func (c *OllamaClient) Capabilities(ctx context.Context, baseURL string, m OllamaModelInfo) []string {
	c.capsMu.Lock()
	v, ok := c.caps[m.Digest]
	c.capsMu.Unlock()
	if ok {
		return v
	}

	raw, err := c.ShowModel(ctx, baseURL, m.Name)
	if err != nil {
		return nil
	}
	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	if json.Unmarshal(raw, &info) != nil || len(info.Capabilities) == 0 {
		return nil
	}

	c.capsMu.Lock()
	c.caps[m.Digest] = info.Capabilities
	c.capsMu.Unlock()
	return info.Capabilities
}

func (c *OllamaClient) RunningModels(ctx context.Context, baseURL string) (json.RawMessage, error) {
	return c.doRaw(ctx, http.MethodGet, baseURL+"/api/ps", nil)
}

// IsLoaded reports whether Ollama already has the model in memory. An
// error counts as loaded: this only decides whether to show a "loading"
// hint, never whether to send the request.
func (c *OllamaClient) IsLoaded(ctx context.Context, baseURL, model string) bool {
	names, err := c.runningNames(ctx, baseURL)
	return err != nil || slices.Contains(names, model)
}

func (c *OllamaClient) runningNames(ctx context.Context, baseURL string) ([]string, error) {
	raw, err := c.RunningModels(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	var ps struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ps.Models))
	for _, m := range ps.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// LoadModel puts a model in memory without generating anything. Ollama
// refuses /api/generate for embedding models, so those load through
// /api/embed with no input instead.
func (c *OllamaClient) LoadModel(ctx context.Context, baseURL, name string, embedding bool) error {
	if embedding {
		_, err := c.doRaw(ctx, http.MethodPost, baseURL+"/api/embed", bytes.NewReader(mustMarshal(map[string]any{"model": name, "input": []string{}})))
		return err
	}
	_, err := c.doRaw(ctx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(mustMarshal(map[string]any{"model": name, "stream": false})))
	return err
}

// UnloadModel frees a model's memory. Ollama answers before the memory is
// actually released (about a second later), so this waits until /api/ps
// stops listing it, or gives up after 10 seconds.
func (c *OllamaClient) UnloadModel(ctx context.Context, baseURL, name string) error {
	_, err := c.doRaw(ctx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(mustMarshal(map[string]any{"model": name, "keep_alive": 0, "stream": false})))
	if err != nil {
		return err
	}
	for range 40 {
		names, err := c.runningNames(ctx, baseURL)
		if err != nil || !slices.Contains(names, name) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s is still loaded after 10 seconds", name)
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

// think is nil to leave it to the model's default. It must stay nil for a
// model without the "thinking" capability: Ollama rejects think:true there
// with "does not support thinking".
func (c *OllamaClient) StreamChat(ctx context.Context, baseURL, model string, messages []OllamaMessage, tools []OllamaTool, options map[string]any, think *bool, onToken, onThinking func(string)) (ChatResult, error) {
	body, err := json.Marshal(ollamaChatRequest{Model: model, Messages: messages, Stream: true, Tools: tools, Options: options, Think: think})
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
		err := fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, buf)
		if m := nPromptTokens.FindSubmatch(buf); m != nil {
			n, _ := strconv.Atoi(string(m[1]))
			return ChatResult{}, &contextOverflowError{promptTokens: n, err: err}
		}
		return ChatResult{}, err
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var full, thinking bytes.Buffer
	var toolCalls []OllamaToolCall
	var tokensPerSec float64
	var contextUsed, promptTokens int
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
			return ChatResult{Content: full.String(), Thinking: thinking.String()}, fmt.Errorf("ollama error: %s", chunk.Error)
		}
		if chunk.Message.Thinking != "" {
			thinking.WriteString(chunk.Message.Thinking)
			onThinking(chunk.Message.Thinking)
		}
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		if chunk.Message.Content != "" {
			full.WriteString(chunk.Message.Content)
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			if chunk.EvalDuration > 0 {
				tokensPerSec = float64(chunk.EvalCount) / (float64(chunk.EvalDuration) / 1e9)
			}
			contextUsed = chunk.PromptCount + chunk.EvalCount
			promptTokens = chunk.PromptCount
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{Content: full.String(), Thinking: thinking.String(), ToolCalls: toolCalls}, err
	}

	return ChatResult{Content: full.String(), Thinking: thinking.String(), ToolCalls: toolCalls, TokensPerSec: tokensPerSec, ContextUsed: contextUsed, PromptTokens: promptTokens}, nil
}

func readAll(r interface{ Read([]byte) (int, error) }, max int) ([]byte, error) {
	buf := make([]byte, max)
	n, err := r.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	return nil, err
}

// contextOverflowError is Ollama rejecting a prompt bigger than num_ctx.
// Its body states the prompt's real size, the one exact token count Flint
// can get for a prompt that never ran.
type contextOverflowError struct {
	promptTokens int
	err          error
}

func (e *contextOverflowError) Error() string { return e.err.Error() }

// The body nests JSON inside a JSON string, so the quotes may be escaped.
var nPromptTokens = regexp.MustCompile(`n_prompt_tokens\\?"\s*:\s*(\d+)`)

// describeOllamaError turns "can't connect" into something a user can act
// on; any other error from Ollama is passed through as it is.
func describeOllamaError(err error, baseURL string) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Sprintf("Can't reach Ollama at %s. Check that it's running (start it with: ollama serve), or change the address in Settings → Connection.", baseURL)
	}
	return err.Error()
}
