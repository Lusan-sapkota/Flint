package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	// A level holding more than this many summaries has its oldest ones
	// merged into one summary a level up, so old content is re-condensed
	// only a logarithmic number of times rather than on every pass.
	summaryFanout = 3
	// Per-message cap on what the summarizer sees, so one huge tool output
	// can't push the summarization request itself past the window.
	summaryInputChars  = 1200
	userNoteChars      = 300
	maxSummariesPerRun = 4
)

// The model only writes notes on what the assistant found and did. What
// the user said is copied verbatim instead (userNotes): the model can re-read
// a file with a tool, but a user-stated fact the summary drops is gone, and
// qwen2.5-3b dropped them repeatedly - once by ignoring them for the
// chunk's latest question, once by recasting one as something the
// assistant said, which the next merge then discarded.
const summarizePrompt = `Condense the chat transcript above into notes on what the assistant found out and did. The user's messages are kept separately, so only mention them as far as needed to make the notes clear. The assistant will later rely on these notes instead of the original messages, so anything left out is forgotten.

Write at most %d short bullet points: exact file names, commands, identifiers and numbers; for each command, whether it succeeded or failed and the key result or error; and last, anything still unresolved.

Leave out greetings and repetition. Never add anything that is not in the transcript: only state a number, count or name if it appears there word for word. Reply with the bullet points only.`

const mergePrompt = `Combine the notes above, which cover consecutive earlier parts of one chat, oldest first, into a single shorter set of notes. The assistant will rely on the result instead of the originals, so anything left out is forgotten.

Write at most %d short bullet points: the key findings with exact file names, commands and identifiers, dropping details that later notes show were superseded or resolved; and last, anything still unresolved.

Never add anything that is not in the notes. Reply with the bullet points only.`

const userNotesPrompt = `The lines above are messages a user sent earlier in a chat, oldest first. Shorten them into at most %d bullet points. Keep every fact, name, number, requirement and instruction the user stated, word for word where possible. Drop greetings and plain questions that state nothing about the user or their project. Never add anything. Reply with the bullet points only.`

// userNotes is the verbatim record of what the user said in a chunk.
func userNotes(chunk []Message) string {
	var b strings.Builder
	for _, m := range chunk {
		if m.Role != "user" {
			continue
		}
		c := strings.Join(strings.Fields(m.Content), " ")
		if r := []rune(c); len(r) > userNoteChars {
			c = string(r[:userNoteChars]) + "..."
		}
		fmt.Fprintf(&b, "- %s\n", c)
		for _, a := range m.Attachments {
			fmt.Fprintf(&b, "  (attached: %s)\n", a.Filename)
		}
	}
	return b.String()
}

// summaryTokens is the target size of one summary: small against the
// window, since every active summary is sent on every request.
func summaryTokens(numCtx int) int {
	return numCtx / 32
}

// summarizableEnd is the index summarizing may run up to (exclusive). It
// never reaches the latest user message (the current turn, including an
// in-progress tool loop, stays verbatim - and an edit only ever truncates
// from there, so it never invalidates a summary), the recent window, or the
// latest image (which must keep being sent), and never splits a tool call
// from its result.
func summarizableEnd(messages []Message) int {
	end := len(messages) - protectedWindow
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			end = min(end, i)
			break
		}
	}
	if lastImage, _, _ := protectedAnchors(messages); lastImage != -1 {
		end = min(end, lastImage)
	}
	for end > 0 && messages[end].Role == "tool" {
		end--
	}
	return max(end, 0)
}

// nextChunk picks the oldest run of not-yet-summarized messages, up to
// about maxTokens, or ok=false if there's less than minTokens of it.
func nextChunk(messages []Message, summaries []Summary, count tokenCounter, minTokens, maxTokens int) (chunk []Message, ok bool) {
	var after int64 = -1
	if len(summaries) > 0 {
		after = summaries[len(summaries)-1].LastMessageID
	}
	_, firstSystem, firstUser := protectedAnchors(messages)
	end := summarizableEnd(messages)
	total := 0
	for i := 0; i < end; i++ {
		if messages[i].ID <= after || i == firstSystem || i == firstUser {
			continue
		}
		chunk = append(chunk, messages[i])
		total += count.text(messages[i].Content)
		if total >= maxTokens && messages[i+1].Role != "tool" {
			return chunk, true
		}
	}
	if len(chunk) > 0 && total >= minTokens {
		return chunk, true
	}
	return nil, false
}

func formatTranscript(messages []Message) string {
	clip := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) > summaryInputChars {
			return s[:summaryInputChars] + " ...[cut]"
		}
		return s
	}
	var b strings.Builder
	for _, m := range messages {
		switch {
		case m.Role == "assistant" && m.ToolCalls != nil:
			if c := clip(m.Content); c != "" {
				fmt.Fprintf(&b, "Assistant: %s\n", c)
			}
			fmt.Fprintf(&b, "Assistant ran: %s\n", extractToolCommand(*m.ToolCalls))
		case m.Role == "tool":
			fmt.Fprintf(&b, "Command result: %s\n", clip(m.Content))
		case m.Role == "system":
			fmt.Fprintf(&b, "Context: %s\n", clip(m.Content))
		default:
			label := "User"
			if m.Role == "assistant" {
				label = "Assistant"
			}
			fmt.Fprintf(&b, "%s: %s\n", label, clip(m.Content))
			for _, a := range m.Attachments {
				fmt.Fprintf(&b, "(%s attached: %s)\n", label, a.Filename)
			}
		}
	}
	return b.String()
}

// mergeCandidates returns the oldest summaryFanout summaries of the lowest
// level that has more than summaryFanout, or nil if no level does.
func mergeCandidates(summaries []Summary) []Summary {
	byLevel := map[int][]Summary{}
	for _, s := range summaries {
		byLevel[s.Level] = append(byLevel[s.Level], s)
	}
	for level := 0; level <= len(summaries); level++ {
		if group := byLevel[level]; len(group) > summaryFanout {
			return group[:summaryFanout]
		}
	}
	return nil
}

// summarizeInBackground condenses the oldest unsummarized messages once
// there are enough of them, and merges summaries up a level when one level
// grows too long. It runs after a reply has fully streamed, on its own
// goroutine, so it never delays or holds open a response; history built
// before it finishes just falls back to dropping the oldest messages.
func (s *Server) summarizeInBackground(user *User, convoID string) {
	if ablated["summaries"] {
		return
	}
	if _, busy := s.summarizing.LoadOrStore(convoID, true); busy {
		return
	}
	go func() {
		defer s.summarizing.Delete(convoID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for range maxSummariesPerRun {
			done, _, err := s.summarizeStep(ctx, user, convoID, false)
			if err != nil {
				log.Printf("warning: summarizing conversation %s: %v", convoID, err)
				return
			}
			if done {
				return
			}
		}
	}()
}

// summarizeStep does one unit of work - a merge if one is due, else one
// new chunk - and reports done when there was nothing to do, plus what it
// did. force summarizes whatever is eligible instead of waiting for a
// quarter of the window to build up.
func (s *Server) summarizeStep(ctx context.Context, user *User, convoID string, force bool) (done bool, did string, err error) {
	convo, err := getConversation(s.db, convoID, user.ID)
	if err != nil || convo == nil {
		return true, "", err
	}
	numCtx := s.numCtxFor(user, convo.Conversation)
	target := summaryTokens(numCtx)
	count := tokenCounter(convo.TokenRatio)

	if group := mergeCandidates(convo.Summaries); group != nil {
		var notes, said strings.Builder
		for _, g := range group {
			notes.WriteString(g.Content + "\n")
			said.WriteString(g.UserNotes)
		}
		out, err := s.condense(ctx, user, convo, fmt.Sprintf(mergePrompt, target/20), "Notes:\n"+notes.String(), target)
		if err != nil {
			return true, "", err
		}
		// Verbatim until it outgrows its share of the window; only then is
		// it condensed, alone, which a small model does far more reliably
		// than picking user facts out of a mixed transcript.
		userSaid := said.String()
		if count.text(userSaid) > numCtx/16 {
			if userSaid, err = s.condense(ctx, user, convo, fmt.Sprintf(userNotesPrompt, target/10), userSaid, numCtx/16); err != nil {
				return true, "", err
			}
			userSaid += "\n"
		}
		merged := Summary{Level: group[0].Level + 1, FirstMessageID: group[0].FirstMessageID, LastMessageID: group[len(group)-1].LastMessageID, Content: out, UserNotes: userSaid}
		return false, fmt.Sprintf("merged %d older summaries into one", len(group)), saveSummary(s.db, convoID, merged, group)
	}

	minTokens := numCtx / 4
	if force {
		minTokens = 1
	}
	chunk, ok := nextChunk(convo.Messages, convo.Summaries, count, minTokens, numCtx/4)
	if !ok {
		return true, "", nil
	}
	out, err := s.condense(ctx, user, convo, fmt.Sprintf(summarizePrompt, target/20), "Transcript:\n"+formatTranscript(chunk), target)
	if err != nil {
		return true, "", err
	}
	sm := Summary{Level: 0, FirstMessageID: chunk[0].ID, LastMessageID: chunk[len(chunk)-1].ID, Content: out, UserNotes: userNotes(chunk)}
	return false, fmt.Sprintf("condensed %d messages into a summary", len(chunk)), saveSummary(s.db, convoID, sm, nil)
}

// isCompactCommand reports whether a message is the `@compact` command.
func isCompactCommand(content string) bool {
	return strings.EqualFold(strings.TrimSpace(content), "@compact")
}

// compactNow handles `@compact`: it summarizes everything eligible right
// away, reporting each step, instead of waiting for the automatic
// threshold. The same messages stay verbatim as always (see
// summarizableEnd). It shares the background guard, so it never runs
// alongside an automatic pass on the same conversation.
func (s *Server) compactNow(w http.ResponseWriter, r *http.Request, user *User, convoID string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, busy := s.summarizing.LoadOrStore(convoID, true); busy {
		w.Write([]byte("[Already compacting this chat in the background. Try again in a moment.]"))
		return
	}
	defer s.summarizing.Delete(convoID)

	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	steps := 0
	// ponytail: a hard cap on steps per command; a huge old chat may need
	// @compact twice, which is cheaper than an unbounded request.
	for range 3 * maxSummariesPerRun {
		done, did, err := s.summarizeStep(r.Context(), user, convoID, true)
		if err != nil {
			fmt.Fprintf(w, "[Compacting stopped: %v]", err)
			return
		}
		if done {
			break
		}
		steps++
		fmt.Fprintf(w, "- %s\n", did)
		flush()
	}
	if steps == 0 {
		w.Write([]byte("[Nothing to compact: older messages are already summarized, and the most recent ones always stay word for word.]"))
		return
	}
	w.Write([]byte("\nCompacted. Your first message, the folder context, the latest tool call and the last few messages stay word for word."))
}

func (s *Server) condense(ctx context.Context, user *User, convo *ConversationWithMessages, system, input string, target int) (string, error) {
	// Instructions go after the input, not in a system message: with a
	// ~2k-token transcript ahead of them, qwen2.5-3b ignored a system prompt
	// (the same dilution the tool nudge ran into, see streamAssistantTurn).
	out, err := s.ollama.Chat(ctx, s.ollamaURLFor(user), convo.Model, []OllamaMessage{
		{Role: "user", Content: input + "\n\n---\n\n" + system},
	}, map[string]any{"num_ctx": s.numCtxFor(user, convo.Conversation), "temperature": 0.2, "num_predict": target * 2})
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("model returned an empty summary")
	}
	return out, nil
}
