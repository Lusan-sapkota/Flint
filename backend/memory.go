package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Memories are facts a user chose to keep across chats. Like `@web`, they
// are only ever written or searched on an explicit `@memory` command,
// never on the model's initiative, and a drafted memory is saved only after
// the user has read and approved it: a small model's draft can be wrong,
// and a wrong memory would resurface in every later chat.

const maxMemoryChars = 4000

const memoryDraftPrompt = `From the conversation above, write down what is worth remembering in future, separate chats about this subject. The user will review your notes before they are saved.

Write at most %d short bullet points. Keep every fact, name, number, decision, requirement and preference the user stated, word for word where possible, and the key conclusions reached. Leave out anything that only mattered in the moment: greetings, questions already answered, commands that were just exploring. Never add anything that is not in the conversation. Reply with the bullet points only.`

// parseMemoryCommand recognizes `@memory save [text]` and `@memory <query>`.
func parseMemoryCommand(content string) (save bool, rest string, ok bool) {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < len("@memory") || !strings.EqualFold(trimmed[:len("@memory")], "@memory") {
		return false, "", false
	}
	rest = trimmed[len("@memory"):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\n' && rest[0] != '\t' {
		return false, "", false
	}
	rest = strings.TrimSpace(rest)
	word, after, _ := strings.Cut(rest, " ")
	if strings.EqualFold(strings.TrimSpace(word), "save") {
		return true, strings.TrimSpace(after), true
	}
	return false, rest, true
}

func folderOf(c Conversation) *string {
	if c.AttachedFolder != nil && *c.AttachedFolder != "" {
		return c.AttachedFolder
	}
	return nil
}

// fitMemories keeps memories, in the order given, until the next one
// would pass maxTokens, and reports how many were left out.
func fitMemories(memories []Memory, maxTokens int) (kept []Memory, left int) {
	used := 0
	for i, m := range memories {
		t := estimateTokens(m.Content) + perMessageTokens
		if used+t > maxTokens {
			return kept, len(memories) - i
		}
		used += t
		kept = append(kept, m)
	}
	return kept, 0
}

func formatMemories(header string, memories []Memory, left int) string {
	var b strings.Builder
	b.WriteString(header)
	for _, m := range memories {
		b.WriteString("\n\n" + strings.TrimSpace(m.Content))
	}
	if left > 0 {
		fmt.Fprintf(&b, "\n\n[%d older memories not shown to fit the context window; the user can recall them with @memory <words>]", left)
	}
	return b.String()
}

// folderMemoryBlock is the standing memory for a folder chat: every memory
// saved from a chat on the same folder, newest first, within an eighth of
// the window. It is rebuilt every turn, not stored in the conversation.
func (s *Server) folderMemoryBlock(userID string, c Conversation) string {
	folder := folderOf(c)
	if folder == nil {
		return ""
	}
	memories, err := folderMemories(s.db, userID, *folder)
	if err != nil {
		log.Printf("warning: loading folder memories: %v", err)
		return ""
	}
	if len(memories) == 0 {
		return ""
	}
	kept, left := fitMemories(memories, numCtxFor(c)/8)
	return formatMemories("Saved memories for this folder (the user saved these from earlier chats; treat them as known facts):", kept, left)
}

// recallMemories injects the memories matching query into the
// conversation, like a web search, and returns a notice for the user when
// there was nothing to add.
const recallHeaderFormat = "Saved memories matching %q (the user saved these from earlier chats; treat them as known facts):"

// parseRecallHeader reads the query back out of a saved recall block, so a
// reloaded chat can show it the way it was typed.
func parseRecallHeader(content string) (query, body string, ok bool) {
	header, body, _ := strings.Cut(content, "\n")
	prefix, suffix, _ := strings.Cut(recallHeaderFormat, "%q")
	quoted, found := strings.CutPrefix(header, prefix)
	if !found {
		return "", "", false
	}
	quoted, found = strings.CutSuffix(quoted, suffix)
	if !found {
		return "", "", false
	}
	q, err := strconv.Unquote(quoted)
	return q, body, err == nil
}

func (s *Server) recallMemories(user *User, convo Conversation, query string) (notice string) {
	memories, err := searchMemories(s.db, user.ID, query)
	if err != nil {
		log.Printf("memory search error: %v", err)
		return "[Memory search failed, answering without saved memories.]\n\n"
	}
	if len(memories) == 0 {
		return fmt.Sprintf("[No saved memories match %q.]\n\n", query)
	}
	kept, left := fitMemories(memories, numCtxFor(convo)/8)
	block := formatMemories(fmt.Sprintf(recallHeaderFormat, query), kept, left)
	if _, err := insertMessage(s.db, convo.ID, "system", block); err != nil {
		log.Printf("warning: saving recalled memories: %v", err)
	}
	return ""
}

// saveMemoryFromChat handles `@memory save`. With text, that text is saved
// as-is. Without, the model drafts a memory from the conversation and the
// draft goes back to the client for review; nothing is saved until the
// user approves it. Neither the command nor the draft enters the
// conversation history.
func (s *Server) saveMemoryFromChat(w http.ResponseWriter, r *http.Request, user *User, convoID, text string) {
	convo, err := getConversation(s.db, convoID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if text != "" {
		if len(text) > maxMemoryChars {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("a memory can be at most %d characters", maxMemoryChars))
			return
		}
		if _, err := createMemory(s.db, user.ID, folderOf(convo.Conversation), &convo.ID, text); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		line, _ := json.Marshal(map[string]string{"content": text})
		fmt.Fprintf(w, "<<<MEMORY_SAVED>>>%s\n", line)
		return
	}

	transcript := memorySource(convo)
	if transcript == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[There's nothing in this chat to remember yet.]"))
		return
	}
	w.WriteHeader(http.StatusOK)
	if !s.ollama.IsLoaded(r.Context(), s.ollamaURLFor(user), convo.Model) {
		fmt.Fprint(w, "<<<LOADING>>>\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	numCtx := numCtxFor(convo.Conversation)
	target := numCtx / 16
	draft, err := s.condense(r.Context(), user, convo, fmt.Sprintf(memoryDraftPrompt, target/15), transcript, target)
	if err != nil {
		fmt.Fprintf(w, "[Couldn't draft a memory: %v]", err)
		return
	}
	marker, _ := json.Marshal(map[string]string{"text": draft})
	fmt.Fprintf(w, "<<<MEMORY_DRAFT>>>%s\n", marker)
}

// memorySource is what a memory draft is written from: the conversation's
// summaries plus the newest unsummarized messages, up to half the window.
// The folder manifest is left out; it's file contents, not something said.
func memorySource(convo *ConversationWithMessages) string {
	var after int64 = -1
	if n := len(convo.Summaries); n > 0 {
		after = convo.Summaries[n-1].LastMessageID
	}
	_, firstSystem, _ := protectedAnchors(convo.Messages)
	count := tokenCounter(convo.TokenRatio)
	budget := numCtxFor(convo.Conversation) / 2
	var recent []Message
	for i := len(convo.Messages) - 1; i >= 0; i-- {
		m := convo.Messages[i]
		if m.ID <= after || i == firstSystem {
			continue
		}
		if budget -= count.text(m.Content); budget < 0 {
			break
		}
		recent = append([]Message{m}, recent...)
	}
	var b strings.Builder
	if len(convo.Summaries) > 0 {
		b.WriteString(formatSummaries(convo.Summaries) + "\n\n")
	}
	if len(recent) > 0 {
		b.WriteString("Conversation:\n" + formatTranscript(recent))
	}
	return strings.TrimSpace(b.String())
}

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	memories, err := listMemories(s.db, userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

func readMemoryContent(w http.ResponseWriter, r *http.Request) (content, conversationID string, ok bool) {
	var body struct {
		Content        string `json:"content"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", "", false
	}
	content = strings.TrimSpace(body.Content)
	if content == "" || len(content) > maxMemoryChars {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a memory must be 1 to %d characters", maxMemoryChars))
		return "", "", false
	}
	return content, body.ConversationID, true
}

// handleCreateMemory saves an approved draft. The folder comes from the
// conversation it was drafted in, looked up under the caller's account.
func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	content, conversationID, ok := readMemoryContent(w, r)
	if !ok {
		return
	}
	var folder, source *string
	if conversationID != "" {
		convo, err := getConversation(s.db, conversationID, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if convo == nil {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		folder = folderOf(convo.Conversation)
		source = &conversationID
	}
	m, err := createMemory(s.db, user.ID, folder, source, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func memoryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return 0, false
	}
	return id, true
}

func (s *Server) handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	content, _, ok := readMemoryContent(w, r)
	if !ok {
		return
	}
	found, err := updateMemory(s.db, id, userFromContext(r).ID, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	found, err := deleteMemory(s.db, id, userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
