package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Server struct {
	db               *sql.DB
	ollama           *OllamaClient
	defaultOllamaURL string
	attachmentsDir   string
	pages            *pages

	conversationQueueMu sync.Mutex
	conversationQueue   map[string]*sync.Mutex
}

func (s *Server) lockConversation(id string) func() {
	s.conversationQueueMu.Lock()
	if s.conversationQueue == nil {
		s.conversationQueue = make(map[string]*sync.Mutex)
	}
	lock, ok := s.conversationQueue[id]
	if !ok {
		lock = &sync.Mutex{}
		s.conversationQueue[id] = lock
	}
	s.conversationQueueMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) ollamaURLFor(u *User) string {
	if u.OllamaBaseURL != nil && *u.OllamaBaseURL != "" {
		return *u.OllamaBaseURL
	}
	return s.defaultOllamaURL
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if v, ok := raw["ollama_base_url"]; ok {
		var baseURL *string
		if err := json.Unmarshal(v, &baseURL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid ollama_base_url")
			return
		}
		if err := updateUserOllamaURL(s.db, user.ID, baseURL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if v, ok := raw["preferred_models"]; ok {
		var models []string
		if err := json.Unmarshal(v, &models); err != nil {
			writeError(w, http.StatusBadRequest, "invalid preferred_models")
			return
		}
		if err := updateUserPreferredModels(s.db, user.ID, models); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if v, ok := raw["brave_api_key"]; ok {
		var key *string
		if err := json.Unmarshal(v, &key); err != nil {
			writeError(w, http.StatusBadRequest, "invalid brave_api_key")
			return
		}
		if err := updateUserBraveAPIKey(s.db, user.ID, key); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	updated, err := getUserByID(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRunningModels(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	info, err := s.ollama.RunningModels(r.Context(), s.ollamaURLFor(user))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(info)
}

func (s *Server) handleShowModel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	info, err := s.ollama.ShowModel(r.Context(), s.ollamaURLFor(user), body.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(info)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := s.ollama.DeleteModel(r.Context(), s.ollamaURLFor(user), body.Name); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePullModel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)

	err := s.ollama.PullModel(r.Context(), s.ollamaURLFor(user), body.Name, func(line []byte) {
		w.Write(line)
		w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}
	})
	if err != nil {
		log.Printf("pull model error: %v", err)
	}
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	convos, err := listConversations(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, convos)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	c, err := createConversation(s.db, uuid.NewString(), user.ID, body.Model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	convo, err := getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, convo)
}

func (s *Server) deleteConversationAndFiles(id, userID string) (found bool, err error) {
	paths, err := getAttachmentPathsForConversation(s.db, id)
	if err != nil {
		return false, err
	}

	found, err = deleteConversation(s.db, id, userID)
	if err != nil || !found {
		return found, err
	}

	for _, p := range paths {
		if err := os.Remove(filepath.Join(s.attachmentsDir, p)); err != nil && !os.IsNotExist(err) {
			log.Printf("warning: failed to remove attachment file %s: %v", p, err)
		}
	}
	return true, nil
}

func (s *Server) handleRenameConversation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := normalizeTitle(body.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	found, err := renameConversation(s.db, id, user.ID, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"title": title})
}

func (s *Server) handleListDirs(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = string(filepath.Separator)
		}
		dir = home
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	dirs, err := listSubdirs(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "can't open that folder")
		return
	}
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	entries := make([]entry, len(dirs))
	for i, name := range dirs {
		entries[i] = entry{Name: name, Path: filepath.Join(dir, name)}
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": dir, "parent": parent, "dirs": entries})
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	found, err := s.deleteConversationAndFiles(id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	a, err := getAttachmentOwned(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	disposition := "attachment"
	if isImageMime(a.MimeType) {
		disposition = "inline" // so <img src> keeps rendering it, not offering a download
	}
	safeName := strings.NewReplacer("\r", "", "\n", "", `"`, `'`).Replace(a.Filename)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeName))
	w.Header().Set("Content-Type", a.MimeType)
	http.ServeFile(w, r, filepath.Join(s.attachmentsDir, a.FilePath))
}

func (s *Server) handleAttachFolder(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	convo, err := getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	var body struct {
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Folder == "" {
		writeError(w, http.StatusBadRequest, "folder is required")
		return
	}

	folder := filepath.Clean(body.Folder)
	if !filepath.IsAbs(folder) {
		writeError(w, http.StatusBadRequest, "folder must be an absolute path")
		return
	}
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "folder does not exist or is not a directory")
		return
	}

	manifest, included, err := readFolderManifest(folder)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := insertMessage(s.db, id, "system", manifest); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := setAttachedFolder(s.db, id, folder); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"folder": folder, "files": included})
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	var body struct {
		Content     string             `json:"content"`
		Attachments []AttachmentUpload `json:"attachments,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	convo, err := getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	defer s.lockConversation(id)()
	s.runUserTurn(w, r, user, id, body.Content, body.Attachments, nil)
}

// Edits the conversation's latest user message: that message and
// everything after it (the reply, tool calls/results, any proposed
// commands - including a still-pending one) are dropped, then the new text
// runs through exactly the same path as a freshly sent message. Only the
// latest one is editable, so no branch of history is ever silently
// rewritten.
func (s *Server) handleEditLastMessage(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	if convo, err := getConversation(s.db, id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	defer s.lockConversation(id)()

	// Re-read inside the lock: a reply that finished streaming while we
	// waited has changed what "everything after" means.
	convo, err := getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	last := -1
	for i := len(convo.Messages) - 1; i >= 0; i-- {
		if convo.Messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last == -1 {
		writeError(w, http.StatusNotFound, "no message to edit")
		return
	}

	// An @web search injects its results as a system message just before
	// the user message; they belong to the old text, so they go too.
	start := last
	for start > 0 && convo.Messages[start-1].Role == "system" && strings.HasPrefix(convo.Messages[start-1].Content, webResultsPrefix) {
		start--
	}

	carried := convo.Messages[last].Attachments
	from := convo.Messages[start]
	if err := truncateConversation(s.db, id, from.ID, from.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.runUserTurn(w, r, user, id, body.Content, nil, carried)
}

// Shared by sending and editing. carried are attachments from an edited
// message whose files are still on disk and get re-linked to the new one.
func (s *Server) runUserTurn(w http.ResponseWriter, r *http.Request, user *User, id, content string, uploads []AttachmentUpload, carried []Attachment) {
	webNotice := ""
	if isWeb, query := stripWebFlag(content); isWeb {
		content = query
		switch {
		case user.BraveAPIKey == nil || *user.BraveAPIKey == "":
			webNotice = "[Web search isn't configured — add a Brave Search API key in Settings to enable it. Answering without web results.]\n\n"
		default:
			if err := s.injectWebSearchResults(r.Context(), id, user, query); err != nil {
				log.Printf("web search error: %v", err)
				webNotice = "[Web search failed, answering without web results.]\n\n"
			}
		}
	}

	messageID, err := insertMessage(s.db, id, "user", content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(uploads) > 0 {
		if err := saveAttachments(s.attachmentsDir, s.db, messageID, uploads); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, a := range carried {
		if err := createAttachment(s.db, a.ID, messageID, a.MimeType, a.Filename, a.FilePath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	placeholderTitle, firstMessage, err := maybeSetTitle(s.db, id, content)
	if err != nil {
		log.Printf("warning: failed to set title: %v", err)
	}

	convo, err := getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if webNotice != "" {
		w.Write([]byte(webNotice))
	}
	s.streamAssistantTurn(w, r, user, convo)

	if firstMessage {
		s.generateTitle(r.Context(), user, convo.Model, id, placeholderTitle, content)
	}
}

const titlePrompt = `You name chat conversations. Read the user's first message and reply with a short, natural title (2 to 6 words) describing what they want. Reply with the title only, no quotes or trailing punctuation.

Message: how do i reverse a list in python without making a copy
Title: Reversing a Python List In Place

Message: my laptop battery drains really fast since the last update
Title: Battery Drain After Update`

// Runs after the reply has streamed, so it never delays the first token;
// the model is already loaded at that point, which keeps this well under a
// second in practice. Any failure just leaves the placeholder title.
func (s *Server) generateTitle(ctx context.Context, user *User, model, id, placeholder, firstMessage string) {
	if ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := s.ollama.Chat(ctx, s.ollamaURLFor(user), model, []OllamaMessage{
		{Role: "system", Content: titlePrompt},
		{Role: "user", Content: "Message: " + firstMessage + "\nTitle:"},
	}, map[string]any{"temperature": 0.2, "num_predict": 24})
	if err != nil {
		log.Printf("warning: title generation failed: %v", err)
		return
	}
	title := cleanGeneratedTitle(out)
	if title == "" {
		return
	}
	if err := replacePlaceholderTitle(s.db, id, placeholder, title); err != nil {
		log.Printf("warning: failed to save generated title: %v", err)
	}
}

func cleanGeneratedTitle(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "Title:"))
	s = strings.Trim(s, "\"'`*#. ")
	return normalizeTitle(s)
}

func (s *Server) injectWebSearchResults(ctx context.Context, conversationID string, user *User, query string) error {
	results, err := braveSearch(ctx, *user.BraveAPIKey, query)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}

	ranked, err := rankByRelevance(ctx, s.ollama, s.ollamaURLFor(user), query, results, rankedResultCount)
	if err != nil {
		log.Printf("warning: embedding rank failed, using unranked results: %v", err)
		ranked = results
		if len(ranked) > rankedResultCount {
			ranked = ranked[:rankedResultCount]
		}
	}

	_, err = insertMessage(s.db, conversationID, "system", formatSearchResults(query, ranked))
	return err
}

func (s *Server) handleApproveCommand(w http.ResponseWriter, r *http.Request) {
	s.resolveCommandAndContinue(w, r, true)
}

func (s *Server) handleDenyCommand(w http.ResponseWriter, r *http.Request) {
	s.resolveCommandAndContinue(w, r, false)
}

func (s *Server) resolveCommandAndContinue(w http.ResponseWriter, r *http.Request, approve bool) {
	user := userFromContext(r)
	convoID := r.PathValue("id")
	cmdID := r.PathValue("cmdId")

	convo, err := getConversation(s.db, convoID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	defer s.lockConversation(convoID)()

	cmd, err := getCommand(s.db, cmdID, convoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cmd == nil || cmd.Status != "pending" {
		writeError(w, http.StatusNotFound, "no pending command with that id")
		return
	}

	var resultText, displayStatus string
	if !approve {
		resultText = "User denied permission to run this command."
		displayStatus = "denied"
		if err := resolveCommand(s.db, cmd.ID, "denied", "", nil); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}
	} else if blocked, reason := checkCommandShield(cmd.Command); blocked {
		resultText = shieldBlockedMessage(reason)
		displayStatus = "blocked"
		if err := resolveCommand(s.db, cmd.ID, "blocked", resultText, nil); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		execCmd := exec.CommandContext(ctx, "sh", "-c", cmd.Command)
		execCmd.Dir = cmd.Cwd
		outputBytes, runErr := execCmd.CombinedOutput()

		output := string(outputBytes)
		if len(output) > 20000 {
			output = output[:20000] + "\n...[truncated]"
		}

		status := "executed"
		exitCode := 0
		if runErr != nil {
			status = "error"
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				output += fmt.Sprintf("\n[error: %v]", runErr)
				exitCode = -1
			}
		}

		if err := resolveCommand(s.db, cmd.ID, status, output, &exitCode); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}

		if exitCode == 0 {
			resultText = fmt.Sprintf("[exit code: 0]\n%s", output)
			displayStatus = "success"
		} else {
			resultText = fmt.Sprintf("[FAILED, exit code: %d]\n%s", exitCode, output)
			displayStatus = "failed"
		}
	}

	if err := insertToolResultMessage(s.db, convoID, cmd.ToolCallID, resultText); err != nil {
		log.Printf("warning: failed to save tool result message: %v", err)
	}

	convo, err = getConversation(s.db, convoID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// The plain-text stream below carries only the assistant's own tokens
	// (and a possible <<<TOOL_CALL>>> marker) for the model's next turn -
	// the command's actual output never otherwise reaches the browser, since
	// it's persisted straight to the tool-result DB row. The UI needs to
	// show the human what really happened, so a matching <<<TOOL_RESULT>>>
	// marker is emitted first, display-only, before the assistant continues.
	resultMarker, _ := json.Marshal(map[string]string{"status": displayStatus, "output": resultText})
	fmt.Fprintf(w, "<<<TOOL_RESULT>>>%s\n", resultMarker)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	s.streamAssistantTurn(w, r, user, convo)
}

// ?think=1 / ?think=0 from the thinking toggle; absent means the model's
// own default. The client only sends it for models that report the
// "thinking" capability.
func thinkParam(r *http.Request) *bool {
	switch r.URL.Query().Get("think") {
	case "1":
		v := true
		return &v
	case "0":
		v := false
		return &v
	}
	return nil
}

func (s *Server) streamAssistantTurn(w http.ResponseWriter, r *http.Request, user *User, convo *ConversationWithMessages) {
	history := buildOptimizedHistory(convo.Messages, s.attachmentsDir)

	var tools []OllamaTool
	var options map[string]any
	if convo.AttachedFolder != nil && *convo.AttachedFolder != "" {
		options = map[string]any{"num_ctx": boostedNumCtx}
		if consecutiveToolCycles(convo.Messages) < maxToolAttemptsPerTurn {
			tools = []OllamaTool{runShellTool}
			if len(history) > 0 {
				last := &history[len(history)-1]
				suffix := toolReasoningPrompt
				if anchor := buildAnchorHeader(*convo.AttachedFolder); anchor != "" {
					suffix = anchor + "\n\n" + suffix
				}
				last.Content = strings.TrimRight(last.Content, "\n") + "\n\n" + suffix
			}
		}
	}

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Thinking tokens travel as one JSON-string line each, so the client can
	// show them apart from the answer without any escaping ambiguity.
	onThinking := func(t string) {
		line, _ := json.Marshal(t)
		fmt.Fprintf(w, "<<<THINK>>>%s\n", line)
		flush()
	}
	result, err := s.ollama.StreamChat(r.Context(), s.ollamaURLFor(user), convo.Model, history, tools, options, thinkParam(r), func(token string) {
		w.Write([]byte(token))
		flush()
	}, onThinking)
	if err != nil && r.Context().Err() != nil {
		// The user pressed Stop (or the tab closed): keep what they already
		// saw so history matches the screen. A tool call cut off mid-way is
		// dropped, since a partial command must never become approvable.
		if result.Content != "" || result.Thinking != "" {
			if err := insertAssistantMessage(s.db, convo.ID, result.Content, result.Thinking, 0); err != nil {
				log.Printf("warning: failed to save stopped assistant message: %v", err)
			}
		}
		if err := touchConversation(s.db, convo.ID); err != nil {
			log.Printf("warning: failed to touch conversation: %v", err)
		}
		return
	}
	if err != nil {
		log.Printf("ollama stream error: %v", err)
		w.Write([]byte("\n[error: " + err.Error() + "]"))
		if err := touchConversation(s.db, convo.ID); err != nil {
			log.Printf("warning: failed to touch conversation: %v", err)
		}
		return
	}

	if len(result.ToolCalls) > 0 {
		tc := result.ToolCalls[0]
		toolCallsJSON, _ := json.Marshal(result.ToolCalls)
		if err := insertToolCallMessage(s.db, convo.ID, result.Content, result.Thinking, string(toolCallsJSON)); err != nil {
			log.Printf("warning: failed to save tool call message: %v", err)
		}

		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(tc.Function.Arguments, &args)

		if blocked, reason := checkCommandShield(args.Command); blocked {
			if err := insertToolResultMessage(s.db, convo.ID, tc.ID, shieldBlockedMessage(reason)); err != nil {
				log.Printf("warning: failed to save blocked tool result: %v", err)
			}
			fmt.Fprintf(w, "\n[Blocked a proposed command: %s]\n\n", reason)

			refreshed, err := getConversation(s.db, convo.ID, user.ID)
			if err != nil {
				log.Printf("warning: failed to refresh conversation after block: %v", err)
				return
			}
			s.streamAssistantTurn(w, r, user, refreshed)
			return
		}

		if ok, reason := checkCommandPreconditions(args.Command, *convo.AttachedFolder); !ok {
			msg := fmt.Sprintf("[PRECONDITION FAILED: %s]\nThis command was not run. Check your assumptions and try a different command, or ask the user for clarification.", reason)
			if err := insertToolResultMessage(s.db, convo.ID, tc.ID, msg); err != nil {
				log.Printf("warning: failed to save precondition-failed tool result: %v", err)
			}
			fmt.Fprintf(w, "\n[Precondition failed: %s]\n\n", reason)

			refreshed, err := getConversation(s.db, convo.ID, user.ID)
			if err != nil {
				log.Printf("warning: failed to refresh conversation after precondition failure: %v", err)
				return
			}
			s.streamAssistantTurn(w, r, user, refreshed)
			return
		}

		cmdID := tc.ID
		if cmdID == "" {
			cmdID = uuid.NewString()
		}
		if _, err := createCommand(s.db, cmdID, convo.ID, tc.ID, args.Command, *convo.AttachedFolder); err != nil {
			log.Printf("warning: failed to save pending command: %v", err)
		}
		if err := touchConversation(s.db, convo.ID); err != nil {
			log.Printf("warning: failed to touch conversation: %v", err)
		}

		marker, _ := json.Marshal(map[string]string{"id": cmdID, "command": args.Command})
		fmt.Fprintf(w, "\n<<<TOOL_CALL>>>%s\n", marker)
		return
	}

	if result.Content != "" {
		if err := insertAssistantMessage(s.db, convo.ID, result.Content, result.Thinking, result.TokensPerSec); err != nil {
			log.Printf("warning: failed to save assistant message: %v", err)
		}
		if result.TokensPerSec > 0 {
			stats, _ := json.Marshal(map[string]float64{"tokensPerSec": math.Round(result.TokensPerSec*10) / 10})
			fmt.Fprintf(w, "\n<<<STATS>>>%s\n", stats)
		}
	}
	if err := touchConversation(s.db, convo.ID); err != nil {
		log.Printf("warning: failed to touch conversation: %v", err)
	}
}
