package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	charsPerToken          = 4.0
	contextTokenBudget     = 3000
	protectedWindow        = 6
	decayHalfLife          = 4.0
	boostedNumCtx          = 8192
	maxToolAttemptsPerTurn = 3
)

// consecutiveToolCycles counts tool calls issued since the last user
// message, bounding a self-correction loop (see maxToolAttemptsPerTurn):
// a model that keeps failing shouldn't get unlimited attempts, each of
// which still costs the user an approval decision.
func consecutiveToolCycles(messages []Message) int {
	count := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			break
		}
		if messages[i].Role == "assistant" && messages[i].ToolCalls != nil {
			count++
		}
	}
	return count
}

func estimateTokens(s string) int {
	return int(math.Ceil(float64(len(s)) / charsPerToken))
}

func decayWeight(turnsAgo int) float64 {
	return math.Exp(-float64(turnsAgo) / decayHalfLife)
}

var phasePatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"exploring", regexp.MustCompile(`(?i)^\s*(ls|cat|head|tail|grep|find|tree)\b`)},
	{"testing", regexp.MustCompile(`(?i)\b(go test|npm test|pytest|jest|go vet)\b`)},
	{"vcs", regexp.MustCompile(`(?i)^\s*git\b`)},
	{"building", regexp.MustCompile(`(?i)\b(go build|npm run build|make)\b`)},
}

func classifyCommand(cmd string) string {
	for _, p := range phasePatterns {
		if p.re.MatchString(cmd) {
			return p.name
		}
	}
	return "acting"
}

func extractToolCommand(toolCallsJSON string) string {
	var calls []OllamaToolCall
	if err := json.Unmarshal([]byte(toolCallsJSON), &calls); err != nil || len(calls) == 0 {
		return ""
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(calls[0].Function.Arguments, &args)
	return args.Command
}

func decayTruncate(content string, weight float64) string {
	if weight >= 0.9 || len(content) < 80 {
		return content
	}
	if weight < 0.05 {
		if len(content) <= 60 {
			return content
		}
		return content[:60] + "...[decayed]"
	}
	keep := int(weight * float64(len(content)))
	if keep >= len(content) {
		return content
	}
	if keep < 60 {
		keep = 60
	}
	return content[:keep] + "...[truncated]"
}

func toOllamaMessage(m Message, attachmentsDir string) OllamaMessage {
	om := OllamaMessage{Role: m.Role, Content: m.Content}
	if m.ToolCalls != nil {
		_ = json.Unmarshal([]byte(*m.ToolCalls), &om.ToolCalls)
	}
	if m.ToolCallID != nil {
		om.ToolCallID = *m.ToolCallID
	}
	for _, a := range m.Attachments {
		encoded, err := loadAttachmentBase64(attachmentsDir, a)
		if err != nil {
			continue
		}
		om.Images = append(om.Images, encoded)
	}
	return om
}

// buildOptimizedHistory assembles a bounded context: the original goal and
// the first system message (the standing folder-attach context, if any) are
// always kept verbatim, the most recent messages are kept verbatim, and
// everything in between is shrunk by an exponential decay proportional to
// its distance from the current turn, falling back to collapsing whole
// tool-call/result pairs into a one-line note if that still isn't enough to
// fit the token budget. Later system messages (e.g. web search injections)
// are not blanket-protected - they decay like anything else, or they would
// accumulate unboundedly over a long conversation.
func buildOptimizedHistory(messages []Message, attachmentsDir string) []OllamaMessage {
	n := len(messages)
	protected := make([]bool, n)

	firstSystem := -1
	firstUser := -1
	for i, m := range messages {
		if m.Role == "system" && firstSystem == -1 {
			firstSystem = i
			protected[i] = true
		}
		if len(m.Attachments) > 0 {
			protected[i] = true
		}
		if firstUser == -1 && m.Role == "user" {
			firstUser = i
			protected[i] = true
		}
	}
	for i := n - protectedWindow; i < n; i++ {
		if i >= 0 {
			protected[i] = true
		}
	}
	for i := 1; i < n; i++ {
		if protected[i] && messages[i].Role == "tool" && messages[i-1].Role == "assistant" && messages[i-1].ToolCalls != nil {
			protected[i-1] = true
		}
	}

	shrunk := make([]Message, n)
	copy(shrunk, messages)
	for i := range shrunk {
		if protected[i] {
			continue
		}
		turnsAgo := n - i
		shrunk[i].Content = decayTruncate(shrunk[i].Content, decayWeight(turnsAgo))
	}

	total := 0
	for _, m := range shrunk {
		total += estimateTokens(m.Content)
	}

	result := make([]OllamaMessage, 0, n)
	i := 0
	for i < n {
		if !protected[i] && total > contextTokenBudget &&
			shrunk[i].Role == "assistant" && shrunk[i].ToolCalls != nil &&
			i+1 < n && shrunk[i+1].Role == "tool" && !protected[i+1] {
			cmd := extractToolCommand(*shrunk[i].ToolCalls)
			lines := strings.Count(messages[i+1].Content, "\n") + 1
			note := fmt.Sprintf("[earlier %s step] ran `%s` -> %d lines of output (condensed)", classifyCommand(cmd), cmd, lines)
			total -= estimateTokens(shrunk[i].Content) + estimateTokens(shrunk[i+1].Content) - estimateTokens(note)
			result = append(result, OllamaMessage{Role: "system", Content: note})
			i += 2
			continue
		}
		result = append(result, toOllamaMessage(shrunk[i], attachmentsDir))
		i++
	}
	return result
}
