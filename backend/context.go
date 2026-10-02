package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	charsPerToken = 4.0
	// Chat-template framing per message (role markers, separators).
	perMessageTokens = 4
	defaultNumCtx    = 4096
	boostedNumCtx    = 8192
	// ollama.com ignores num_ctx (E23); this only bounds what each turn resends.
	cloudNumCtx            = 32768
	minNumCtx              = 2048
	maxNumCtx              = 1048576
	responseReserve        = 1024
	protectedWindow        = 6
	decayHalfLife          = 4.0
	maxToolAttemptsPerTurn = 3
)

// Always set: without num_ctx Ollama reloads the model at its server default.
func (s *Server) numCtxFor(user *User, c Conversation) int {
	info := s.ollama.modelInfo(s.ollamaURLFor(user), c.Model)
	n := defaultNumCtx
	switch {
	case info.RemoteHost != "" && user.CloudNumCtx != nil:
		n = *user.CloudNumCtx
	case info.RemoteHost != "":
		n = cloudNumCtx
	case user.NumCtx != nil:
		n = *user.NumCtx
	case c.AttachedFolder != nil && *c.AttachedFolder != "":
		n = boostedNumCtx
	}
	if limit := info.Details.ContextLength; limit > 0 && n > limit {
		n = limit
	}
	return n
}

// chars/4 calibrated by the last real prompt_eval_count: plain chars/4 was 2x off on dense text.
type tokenCounter float64

func (r tokenCounter) text(s string) int {
	return int(math.Ceil(float64(estimateTokens(s))*float64(r))) + perMessageTokens
}

func (r tokenCounter) messages(ms []OllamaMessage) int {
	total := 0
	for _, m := range ms {
		total += r.text(m.Content)
	}
	return total
}

func clampRatio(r float64) float64 {
	return math.Min(4, math.Max(0.5, r))
}

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
	return decayWeightWithHalfLife(turnsAgo, decayHalfLife)
}

func decayWeightWithHalfLife(turnsAgo int, halfLife float64) float64 {
	return math.Exp(-float64(turnsAgo) / halfLife)
}

func effectiveHalfLife(m Message, precedingCommand string) float64 {
	if strings.Contains(m.Content, "[FAILED") {
		return decayHalfLife * 3
	}
	if m.Role == "tool" && classifyCommand(precedingCommand) == "exploring" {
		return decayHalfLife * 0.5
	}
	return decayHalfLife
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

func hasImage(m Message) bool {
	for _, a := range m.Attachments {
		if isImageMime(a.MimeType) {
			return true
		}
	}
	return false
}

func toOllamaMessage(m Message, attachmentsDir string, sendImages bool) OllamaMessage {
	om := OllamaMessage{Role: m.Role, Content: m.Content}
	if m.ToolCalls != nil {
		_ = json.Unmarshal([]byte(*m.ToolCalls), &om.ToolCalls)
	}
	if m.ToolCallID != nil {
		om.ToolCallID = *m.ToolCallID
	}
	for _, a := range m.Attachments {
		if !isImageMime(a.MimeType) {
			// Non-images can't reach the model, but it should know the file exists.
			om.Content = strings.TrimRight(om.Content, "\n") + fmt.Sprintf("\n[Attached file: %s - not visible to you, only the user can see it]", a.Filename)
			continue
		}
		if !sendImages {
			om.Content = strings.TrimRight(om.Content, "\n") + fmt.Sprintf("\n[Earlier image: %s - no longer attached, to save context. If you need to look at it again, ask the user to re-send it]", a.Filename)
			continue
		}
		encoded, err := loadAttachmentBase64(attachmentsDir, a)
		if err != nil {
			continue
		}
		om.Images = append(om.Images, encoded)
	}
	return om
}

type historyEntry struct {
	msg       OllamaMessage
	tokens    int
	droppable bool
}

func coveredBySummaries(messages []Message, summaries []Summary) (covered []bool, first int) {
	_, firstSystem, firstUser := protectedAnchors(messages)
	// Latest tool call stays verbatim: summarized away, qwen2.5-3b stopped making structured calls.
	lastCall, lastResult := lastToolExchange(messages)
	covered = make([]bool, len(messages))
	first = -1
	for i, m := range messages {
		if i == firstSystem || i == firstUser || i == lastCall || i == lastResult {
			continue
		}
		for _, sm := range summaries {
			if m.ID >= sm.FirstMessageID && m.ID <= sm.LastMessageID {
				covered[i] = true
				if first == -1 {
					first = i
				}
				break
			}
		}
	}
	return covered, first
}

func condensedCount(messages []Message, summaries []Summary) int {
	covered, _ := coveredBySummaries(messages, summaries)
	n := 0
	for _, c := range covered {
		if c {
			n++
		}
	}
	return n
}

// Fits history here because Ollama, given an oversized prompt, silently drops middle messages or rejects it.
func buildOptimizedHistory(messages []Message, summaries []Summary, attachmentsDir string, budget int, count tokenCounter) []OllamaMessage {
	n := len(messages)
	protected := make([]bool, n)

	lastImage, firstSystem, firstUser := protectedAnchors(messages)
	lastCall, lastResult := lastToolExchange(messages)
	for _, i := range []int{lastImage, firstSystem, firstUser, lastCall, lastResult} {
		if i != -1 {
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

	covered, firstCovered := coveredBySummaries(messages, summaries)

	// Dialogue never decays: truncated replies made qwen2.5-3b end its own with "...[truncated]".
	shrunk := make([]Message, n)
	copy(shrunk, messages)
	for i := range shrunk {
		if protected[i] || covered[i] || (shrunk[i].Role != "tool" && shrunk[i].Role != "system") {
			continue
		}
		turnsAgo := n - i
		precedingCommand := ""
		if i > 0 && messages[i-1].Role == "assistant" && messages[i-1].ToolCalls != nil {
			precedingCommand = extractToolCommand(*messages[i-1].ToolCalls)
		}
		halfLife := effectiveHalfLife(messages[i], precedingCommand)
		shrunk[i].Content = decayTruncate(shrunk[i].Content, decayWeightWithHalfLife(turnsAgo, halfLife))
	}

	total := 0
	if firstCovered != -1 {
		total += count.text(formatSummaries(summaries))
	}
	for i, m := range shrunk {
		if !covered[i] {
			total += count.text(m.Content)
		}
	}

	var entries []historyEntry
	add := func(om OllamaMessage, droppable bool) {
		t := count.text(om.Content)
		for _, img := range om.Images {
			t += imageTokens(img)
		}
		entries = append(entries, historyEntry{msg: om, tokens: t, droppable: droppable})
	}
	i := 0
	for i < n {
		if covered[i] {
			if i == firstCovered {
				add(OllamaMessage{Role: "system", Content: formatSummaries(summaries)}, false)
			}
			i++
			continue
		}
		if !protected[i] && total > budget &&
			shrunk[i].Role == "assistant" && shrunk[i].ToolCalls != nil &&
			i+1 < n && shrunk[i+1].Role == "tool" && !protected[i+1] {
			cmd := extractToolCommand(*shrunk[i].ToolCalls)
			lines := strings.Count(messages[i+1].Content, "\n") + 1
			note := fmt.Sprintf("[earlier %s step] ran `%s` -> %d lines of output (condensed)", classifyCommand(cmd), cmd, lines)
			total -= count.text(shrunk[i].Content) + count.text(shrunk[i+1].Content) - count.text(note)
			add(OllamaMessage{Role: "system", Content: note}, true)
			i += 2
			continue
		}
		add(toOllamaMessage(shrunk[i], attachmentsDir, i == lastImage), !protected[i])
		i++
	}
	return fitBudget(entries, budget, count)
}

func lastToolExchange(messages []Message) (call, result int) {
	for i := len(messages) - 2; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].ToolCalls != nil && messages[i+1].Role == "tool" {
			return i, i + 1
		}
	}
	return -1, -1
}

func protectedAnchors(messages []Message) (lastImage, firstSystem, firstUser int) {
	lastImage, firstSystem, firstUser = -1, -1, -1
	for i, m := range messages {
		if m.Role == "system" && firstSystem == -1 {
			firstSystem = i
		}
		if firstUser == -1 && m.Role == "user" {
			firstUser = i
		}
		if hasImage(m) {
			lastImage = i
		}
	}
	return
}

func fitBudget(entries []historyEntry, budget int, count tokenCounter) []OllamaMessage {
	total := 0
	for _, e := range entries {
		total += e.tokens
	}
	dropped := 0
	keep := make([]bool, len(entries))
	for i := range keep {
		keep[i] = true
	}
	for i := 0; i < len(entries) && total > budget; i++ {
		if !entries[i].droppable {
			continue
		}
		keep[i] = false
		total -= entries[i].tokens
		dropped++
		// A tool result must never outlive the tool call it answers.
		for i+1 < len(entries) && entries[i+1].msg.Role == "tool" && entries[i+1].droppable {
			i++
			keep[i] = false
			total -= entries[i].tokens
			dropped++
		}
	}
	omitted := fmt.Sprintf("[%d earlier messages omitted to fit the context window]", dropped)
	if dropped > 0 {
		total += count.text(omitted)
	}

	// Protected messages can still overflow, so cut tool output oldest first; the model can't imitate that marker.
	for i := range entries {
		if total <= budget {
			break
		}
		e := &entries[i]
		if !keep[i] || e.msg.Role != "tool" {
			continue
		}
		const cut = "\n...[output cut to fit the context window]"
		keepTokens := max(e.tokens-(total-budget)-count.text(cut), 64)
		chars := int(float64(keepTokens) / float64(count) * charsPerToken)
		if chars >= len(e.msg.Content) {
			continue
		}
		e.msg.Content = strings.ToValidUTF8(e.msg.Content[:chars], "") + cut
		t := count.text(e.msg.Content)
		total -= e.tokens - t
		e.tokens = t
	}

	out := make([]OllamaMessage, 0, len(entries))
	noted := false
	for i, e := range entries {
		if !keep[i] {
			if !noted {
				out = append(out, OllamaMessage{Role: "system", Content: omitted})
				noted = true
			}
			continue
		}
		out = append(out, e.msg)
	}
	return out
}

func formatSummaries(summaries []Summary) string {
	var said, notes strings.Builder
	for _, s := range summaries {
		said.WriteString(s.UserNotes)
		notes.WriteString(s.Content + "\n")
	}
	var b strings.Builder
	b.WriteString("Summary of the earlier part of this conversation (the original messages are condensed; treat this as what was said and done).")
	if said.Len() > 0 {
		b.WriteString("\n\nWhat the user said:\n" + said.String())
	}
	b.WriteString("\nWhat the assistant found and did:\n" + strings.TrimRight(notes.String(), "\n"))
	return b.String()
}
