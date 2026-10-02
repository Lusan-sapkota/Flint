package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

	summarizing sync.Map
	// By command id: delivers the user's approve/deny to the waiting agent.
	agentDecisions sync.Map
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
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
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

	for _, column := range []string{"num_ctx", "cloud_num_ctx"} {
		v, ok := raw[column]
		if !ok {
			continue
		}
		var n *int
		if err := json.Unmarshal(v, &n); err != nil || (n != nil && (*n < minNumCtx || *n > maxNumCtx)) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("a context window must be %d to %d tokens, or empty for Auto", minNumCtx, maxNumCtx))
			return
		}
		if err := updateUserIntSetting(s.db, user.ID, column, n); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for column, label := range map[string]string{"max_agents": "max agents", "cloud_max_agents": "max agents", "agent_commands": "commands per agent"} {
		v, ok := raw[column]
		if !ok {
			continue
		}
		var n *int
		if err := json.Unmarshal(v, &n); err != nil || (n != nil && (*n < 1 || *n > maxMaxAgents)) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s must be 1 to %d, or empty for Auto", label, maxMaxAgents))
			return
		}
		if err := updateUserIntSetting(s.db, user.ID, column, n); err != nil {
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
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
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
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(info)
}

// Unloads every other running model first so a small GPU never has to fit two.
func (s *Server) handleLoadModel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	baseURL := s.ollamaURLFor(user)

	running, err := s.ollama.runningNames(r.Context(), baseURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	for _, name := range running {
		if name == body.Name {
			continue
		}
		if err := s.ollama.UnloadModel(r.Context(), baseURL, name); err != nil {
			writeError(w, http.StatusBadGateway, "couldn't unload "+name+": "+describeOllamaError(err, baseURL))
			return
		}
	}

	raw, err := s.ollama.ShowModel(r.Context(), baseURL, body.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	json.Unmarshal(raw, &info)
	embedding := slices.Contains(info.Capabilities, "embedding") && !slices.Contains(info.Capabilities, "completion")
	if err := s.ollama.LoadModel(r.Context(), baseURL, body.Name, embedding); err != nil {
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnloadModel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.ollama.UnloadModel(r.Context(), s.ollamaURLFor(user), body.Name); err != nil {
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
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

func (s *Server) handleSearchConversations(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	results, err := searchConversations(s.db, user.ID, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
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
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Model != "" {
		s.switchConversationModel(w, r, user, id, body.Model)
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

// Only an empty chat can switch: history, summaries and token calibration came from its model.
func (s *Server) switchConversationModel(w http.ResponseWriter, r *http.Request, user *User, id, model string) {
	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	if err != nil {
		writeError(w, http.StatusBadGateway, describeOllamaError(err, s.ollamaURLFor(user)))
		return
	}
	if !hasModel(models, model) {
		writeError(w, http.StatusBadRequest, "that model isn't installed")
		return
	}
	defer s.lockConversation(id)()
	started, found, err := conversationStarted(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if started {
		writeError(w, http.StatusConflict, "the model can't change once the chat has started")
		return
	}
	if err := setConversationModel(s.db, id, model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
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
	defer s.lockConversation(id)()

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

	if convo.AttachedFolder != nil {
		// A pending command run in a new folder would not be what the user read.
		if pending, err := getPendingCommand(s.db, id); err != nil || pending != nil {
			writeError(w, http.StatusConflict, "approve or deny the pending command before changing the folder")
			return
		}
	}

	manifest, included, err := readFolderManifest(folder)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Rewrite in place: the first system message is protected, an appended manifest would decay.
	if i := slices.IndexFunc(convo.Messages, func(m Message) bool {
		return m.Role == "system" && strings.HasPrefix(m.Content, manifestPrefix)
	}); i != -1 {
		err = updateMessageContent(s.db, convo.Messages[i].ID, manifest)
	} else {
		_, err = insertMessage(s.db, id, "system", manifest)
	}
	if err != nil {
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
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageBody)
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
	if s.refuseOversized(w, user, convo, body.Content) {
		return
	}

	defer s.lockConversation(id)()
	s.runUserTurn(w, r, user, id, body.Content, body.Attachments, nil)
}

// Only the latest user message is editable (it and everything after, pending command included, are dropped), so no history branch is silently rewritten.
func (s *Server) handleEditLastMessage(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	var body struct {
		Content string `json:"content"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageBody)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	// These commands never become a user message, so editing into one would just delete the original.
	_, isAgent := parseAgentCommand(body.Content)
	if _, _, ok := parseMemoryCommand(body.Content); ok || isCompactCommand(body.Content) || isAgent {
		writeError(w, http.StatusBadRequest, "send @memory, @compact or @agent as a new message instead of an edit")
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

	// Re-read inside the lock: a reply that finished while we waited changes what "everything after" means.
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

	// `@web` results saved just before the user message belong to the old text, so they go too.
	start := last
	for start > 0 && convo.Messages[start-1].Role == "system" && strings.HasPrefix(convo.Messages[start-1].Content, webResultsPrefix) {
		start--
	}

	// Checked before truncating, so a refused edit loses nothing.
	kept := *convo
	kept.Messages = convo.Messages[:start]
	if s.refuseOversized(w, user, &kept, body.Content) {
		return
	}

	carried := convo.Messages[last].Attachments
	from := convo.Messages[start]
	if err := truncateConversation(s.db, id, from.ID, from.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.runUserTurn(w, r, user, id, body.Content, nil, carried)
}

// carried: an edited message's attachments, still on disk, re-linked to the new message.
func (s *Server) runUserTurn(w http.ResponseWriter, r *http.Request, user *User, id, content string, uploads []AttachmentUpload, carried []Attachment) {
	decoded, err := decodeUploads(uploads)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	notice := ""
	// Search and agent progress may start the response before the model runs.
	started := false
	start := func() {
		if !started {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			started = true
		}
	}
	if isCompactCommand(content) {
		s.compactNow(w, r, user, id)
		return
	}
	isAgent := false
	if task, ok := parseAgentCommand(content); ok {
		isAgent = true
		if task == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("[Use @agent <task> to split a task into subtasks that run as separate agents.]"))
			return
		}
		convo, err := getConversation(s.db, id, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		content = task
		if folderOf(convo.Conversation) == nil {
			notice = "[@agent works on an attached folder, so this is answered as a normal chat. Attach a folder to use agents, or start a message with @web to search the web.]\n\n"
		} else {
			start()
			if !s.ollama.IsLoaded(r.Context(), s.ollamaURLFor(user), convo.Model) {
				fmt.Fprint(w, "<<<LOADING>>>\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			run, err := s.planAgentRun(r.Context(), user, convo, task)
			switch {
			case err != nil:
				fmt.Fprintf(w, "[Couldn't plan the agents: %s]", describeOllamaError(err, s.ollamaURLFor(user)))
				return
			case run != nil:
				line, _ := json.Marshal(run)
				fmt.Fprintf(w, "<<<AGENT_PLAN>>>%s\n", line)
				return
			}
			notice = "[This doesn't split into independent parts, so it's answered as a normal chat.]\n\n"
		}
	}
	if save, rest, ok := parseMemoryCommand(content); ok && !isAgent {
		if save {
			s.saveMemoryFromChat(w, r, user, id, rest)
			return
		}
		if rest == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("[Use @memory <words> to recall saved memories, or @memory save to save one.]"))
			return
		}
		convo, err := getConversation(s.db, id, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		notice = s.recallMemories(user, convo.Conversation, rest)
		content = rest
	} else if isWeb, query := stripWebFlag(content); isWeb && !isAgent {
		content = query
		switch {
		case user.BraveAPIKey == nil || *user.BraveAPIKey == "":
			notice = "[Web search isn't configured — add a Brave Search API key in Settings to enable it. Answering without web results.]\n\n"
		default:
			start()
			line, _ := json.Marshal(map[string]string{"query": query})
			fmt.Fprintf(w, "<<<SEARCHING>>>%s\n", line)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			results, err := s.injectWebSearchResults(r.Context(), id, user, query)
			switch {
			case err != nil:
				log.Printf("web search error: %v", err)
				notice = "[Web search failed, answering without web results.]\n\n"
			case len(results) == 0:
				notice = "[The web search found nothing, answering without web results.]\n\n"
			default:
				line, _ := json.Marshal(sourcesView(query, results))
				fmt.Fprintf(w, "<<<SOURCES>>>%s\n", line)
			}
		}
	}

	messageID, err := insertMessage(s.db, id, "user", content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := saveAttachments(s.attachmentsDir, s.db, messageID, decoded); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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

	start()
	if notice != "" {
		w.Write([]byte(notice))
	}
	s.streamAssistantTurn(w, r, user, convo)

	// No title after a failed reply: it wastes a model call and keeps input blocked while the user retries.
	if firstMessage && hasAssistantMessage(s.db, id) {
		s.generateTitle(r.Context(), user, convo.Model, s.numCtxFor(user, convo.Conversation), id, placeholderTitle, content)
	}
}

const titlePrompt = `You name chat conversations. Read the user's first message and reply with a short, natural title (2 to 6 words) describing what they want. Reply with the title only, no quotes or trailing punctuation.

Message: how do i reverse a list in python without making a copy
Title: Reversing a Python List In Place

Message: my laptop battery drains really fast since the last update
Title: Battery Drain After Update`

// Runs after the reply so it never delays the first token; any failure keeps the placeholder.
func (s *Server) generateTitle(ctx context.Context, user *User, model string, numCtx int, id, placeholder, firstMessage string) {
	if ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := s.ollama.Chat(ctx, s.ollamaURLFor(user), model, []OllamaMessage{
		{Role: "system", Content: titlePrompt},
		{Role: "user", Content: "Message: " + firstMessage + "\nTitle:"},
	}, map[string]any{"num_ctx": numCtx, "temperature": 0.2, "num_predict": 24})
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

func (s *Server) injectWebSearchResults(ctx context.Context, conversationID string, user *User, query string) ([]SearchResult, error) {
	ranked, err := s.searchWeb(ctx, user, query)
	if err != nil || len(ranked) == 0 {
		return nil, err
	}
	_, err = insertMessage(s.db, conversationID, "system", formatSearchResults(query, ranked))
	return ranked, err
}

func (s *Server) searchWeb(ctx context.Context, user *User, query string) ([]SearchResult, error) {
	results, err := braveSearch(ctx, *user.BraveAPIKey, query)
	if err != nil || len(results) == 0 {
		return nil, err
	}
	ranked, err := rankByRelevance(ctx, s.ollama, s.ollamaURLFor(user), query, results, rankedResultCount)
	if err != nil {
		log.Printf("warning: embedding rank failed, using unranked results: %v", err)
		ranked = results[:min(len(results), rankedResultCount)]
	}
	return ranked, nil
}

// The result always states success or failure first: empty output after a nonzero exit looked like success.
func (s *Server) executeCommand(ctx context.Context, cmd *Command) (resultText, displayStatus string) {
	if cmd.Edit != nil {
		return s.applyEdit(cmd)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
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

	if strings.TrimSpace(output) == "" {
		output = "(no output)"
		if exitCode == 1 && strings.Contains(cmd.Command, "grep") {
			output = "(no output: grep matched nothing. Search again with one shorter word, since the text may be spelled differently.)"
		}
	}
	if exitCode == 0 {
		return fmt.Sprintf("[exit code: 0]\n%s", output), "success"
	}
	return fmt.Sprintf("[FAILED, exit code: %d]\n%s", exitCode, output), "failed"
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

	// The user's next message says what to do instead, so no retry hint and no model turn here.
	replyInstead := !approve && r.URL.Query().Get("reply") == "1"

	var resultText, displayStatus string
	if replyInstead {
		resultText = "User denied this command and wrote what to do instead in their next message. Follow that message rather than retrying this command."
		displayStatus = "denied"
		if err := resolveCommand(s.db, cmd.ID, "denied", "", nil); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}
	} else if !approve {
		resultText = "User denied this command. It was their choice not to run it, and nothing is wrong with your access. Don't guess what it would have output. Try a different command that gets the same information, or ask the user how they want to proceed."
		displayStatus = "denied"
		if err := resolveCommand(s.db, cmd.ID, "denied", "", nil); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}
	} else if blocked, reason := checkCommandShield(cmd.Command); blocked && cmd.Edit == nil {
		resultText = shieldBlockedMessage(reason)
		displayStatus = "blocked"
		if err := resolveCommand(s.db, cmd.ID, "blocked", resultText, nil); err != nil {
			log.Printf("warning: failed to resolve command: %v", err)
		}
	} else {
		resultText, displayStatus = s.executeCommand(r.Context(), cmd)
	}

	if err := insertToolResultMessage(s.db, convoID, cmd.ToolCallID, resultText); err != nil {
		log.Printf("warning: failed to save tool result message: %v", err)
	}
	if replyInstead {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	convo, err = getConversation(s.db, convoID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Command output otherwise only reaches the DB, so the UI gets a display-only marker.
	// A denial's text is written for the model; the card's "Denied" status covers the user.
	shown := resultText
	if displayStatus == "denied" {
		shown = ""
	}
	resultMarker, _ := json.Marshal(map[string]string{"status": displayStatus, "output": shown})
	fmt.Fprintf(w, "<<<TOOL_RESULT>>>%s\n", resultMarker)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	s.streamAssistantTurn(w, r, user, convo)
}

// nil means the model's own default; the client only sends it for thinking-capable models.
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
	s.streamAssistantTurnAttempt(w, r, user, convo, false)
}

func (s *Server) turnSetup(user *User, convo *ConversationWithMessages, numCtx int) (tools []OllamaTool, suffix string, toolsTokens, budget int) {
	approvals, cycles := consecutiveToolCycles(convo.Messages)
	if convo.AttachedFolder != nil && *convo.AttachedFolder != "" && approvals < maxToolAttemptsPerTurn && cycles < maxToolCyclesPerTurn {
		tools = []OllamaTool{runShellTool}
		if !ablated["filetools"] {
			tools = append(tools, readFileTool, writeFileTool, editFileTool)
		}
		if !ablated["nudge"] && !forbidsCommands(convo.Messages) {
			suffix = toolReasoningPrompt
		}
		if note := groundingNote(convo.Messages, *convo.AttachedFolder); note != "" && !ablated["grounding"] {
			suffix = strings.TrimLeft(note+"\n\n"+suffix, "\n")
		}
		if anchor := buildAnchorHeader(*convo.AttachedFolder); anchor != "" && !ablated["anchor"] {
			suffix = strings.TrimLeft(anchor+"\n\n"+suffix, "\n")
		}
	} else if (convo.AttachedFolder == nil || *convo.AttachedFolder == "") && asksToRun(convo.Messages) {
		suffix = noFolderNote
	}
	// Next to the anchor: as a system message after the manifest, qwen2.5-3b ignored them (E14).
	if memories := s.folderMemoryBlock(user, convo.Conversation); memories != "" {
		suffix = strings.TrimLeft(memories+"\n\n"+suffix, "\n")
	}
	toolsJSON, _ := json.Marshal(tools)
	toolsTokens = estimateTokens(string(toolsJSON))
	overhead := toolsTokens + estimateTokens(suffix)
	budget = numCtx - responseReserve - int(float64(overhead)*float64(tokenCounter(convo.TokenRatio)))
	return
}

// Only what fitting can't drop or condense counts against the next message.
func (s *Server) maxInputTokens(user *User, convo *ConversationWithMessages, numCtx int) int {
	_, _, _, budget := s.turnSetup(user, convo, numCtx)
	count := tokenCounter(convo.TokenRatio)
	floor := 0
	for _, m := range buildOptimizedHistory(convo.Messages, convo.Summaries, s.attachmentsDir, 0, count) {
		floor += count.text(m.Content)
		for _, img := range m.Images {
			floor += imageTokens(img)
		}
	}
	return max(budget-floor-perMessageTokens, 0)
}

func (s *Server) refuseOversized(w http.ResponseWriter, user *User, convo *ConversationWithMessages, content string) bool {
	limit := s.maxInputTokens(user, convo, s.numCtxFor(user, convo.Conversation))
	size := tokenCounter(convo.TokenRatio).text(content)
	if size <= limit {
		return false
	}
	writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("This message is about %d tokens, but this chat has room for %d. Send it in parts, or save it to a file and attach its folder.", size, limit))
	return true
}

func (s *Server) streamAssistantTurnAttempt(w http.ResponseWriter, r *http.Request, user *User, convo *ConversationWithMessages, retried bool) {
	numCtx := s.numCtxFor(user, convo.Conversation)
	options := map[string]any{"num_ctx": numCtx}
	count := tokenCounter(convo.TokenRatio)
	tools, suffix, toolsTokens, budget := s.turnSetup(user, convo, numCtx)

	history := buildOptimizedHistory(convo.Messages, convo.Summaries, s.attachmentsDir, budget, count)
	if ablated["fit"] {
		history = rawHistory(convo.Messages, s.attachmentsDir)
	}
	if suffix != "" && len(history) > 0 {
		last := &history[len(history)-1]
		last.Content = strings.TrimRight(last.Content, "\n") + "\n\n" + suffix
	}

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// A cold load can take tens of seconds; say so instead of looking hung.
	if !s.ollama.IsLoaded(r.Context(), s.ollamaURLFor(user), convo.Model) {
		fmt.Fprint(w, "<<<LOADING>>>\n")
		flush()
	}

	// One JSON-string line per chunk keeps thinking apart from the answer without escaping ambiguity.
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
		// On Stop keep what the user saw; drop a cut-off tool call, a partial command must never become approvable.
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

	var overflow *contextOverflowError
	if errors.As(err, &overflow) && !retried && r.Context().Err() == nil {
		s.calibrateTokenRatio(convo.ID, history, toolsTokens, overflow.promptTokens)
		if refreshed, rerr := getConversation(s.db, convo.ID, user.ID); rerr == nil && refreshed != nil {
			s.streamAssistantTurnAttempt(w, r, user, refreshed, true)
			return
		}
	}
	if err != nil {
		log.Printf("ollama stream error: %v", err)
		w.Write([]byte("\n[error: " + describeOllamaError(err, s.ollamaURLFor(user)) + "]"))
		if err := touchConversation(s.db, convo.ID); err != nil {
			log.Printf("warning: failed to touch conversation: %v", err)
		}
		return
	}

	// Sent for every response so the bar also moves after tool-call turns.
	if result.ContextUsed > 0 {
		if err := setContextUsage(s.db, convo.ID, result.ContextUsed, numCtx); err != nil {
			log.Printf("warning: failed to save context usage: %v", err)
		}
		// The unsaved reply will be protected history for the next message.
		maxInput := max(s.maxInputTokens(user, convo, numCtx)-(result.ContextUsed-result.PromptTokens)-perMessageTokens, 0)
		line, _ := json.Marshal(map[string]any{"used": result.ContextUsed, "max": numCtx, "condensed": condensedCount(convo.Messages, convo.Summaries), "maxInput": maxInput, "charsPerToken": charsPerToken / float64(count)})
		fmt.Fprintf(w, "<<<CONTEXT>>>%s\n", line)
		flush()
	}
	s.calibrateTokenRatio(convo.ID, history, toolsTokens, result.PromptTokens)

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
		folder := *convo.AttachedFolder

		continueTurn := func(toolResult string) {
			if err := insertToolResultMessage(s.db, convo.ID, tc.ID, toolResult); err != nil {
				log.Printf("warning: failed to save tool result: %v", err)
			}
			refreshed, err := getConversation(s.db, convo.ID, user.ID)
			if err != nil {
				log.Printf("warning: failed to refresh conversation: %v", err)
				return
			}
			s.streamAssistantTurn(w, r, user, refreshed)
		}

		switch tc.Function.Name {
		case "read_file":
			// A cloud model sends what it reads to its host, so there a read waits for approval like a command.
			if s.ollama.modelInfo(s.ollamaURLFor(user), convo.Model).RemoteHost != "" {
				var a struct {
					Path string `json:"path"`
				}
				_ = json.Unmarshal(tc.Function.Arguments, &a)
				if _, err := resolveInFolder(folder, a.Path); err != nil {
					continueTurn(readFileResult(folder, tc.Function.Arguments))
					return
				}
				read, _ := json.Marshal(plannedEdit{Read: tc.Function.Arguments})
				readStr := string(read)
				cmdID := tc.ID
				if cmdID == "" {
					cmdID = uuid.NewString()
				}
				label := toolCallLabel(tc)
				if _, err := createCommandWithEdit(s.db, cmdID, convo.ID, tc.ID, label, folder, &readStr); err != nil {
					log.Printf("warning: failed to save pending read: %v", err)
				}
				marker, _ := json.Marshal(map[string]any{"id": cmdID, "command": label, "missing": missingPaths(folder)(result.Content)})
				fmt.Fprintf(w, "\n<<<TOOL_CALL>>>%s\n", marker)
				return
			}
			out := readFileResult(folder, tc.Function.Arguments)
			if strings.HasPrefix(out, "[BLOCKED") {
				fmt.Fprintf(w, "\n[Blocked a proposed command: %s]\n\n", secretFileReason)
			} else {
				line, _ := json.Marshal(map[string]string{"label": toolCallLabel(tc), "result": out})
				fmt.Fprintf(w, "\n<<<READ>>>%s\n", line)
				flush()
			}
			continueTurn(out)
			return
		case "write_file", "edit_file":
			edit, err := planEdit(folder, tc.Function.Name, tc.Function.Arguments)
			var blocked shieldError
			if errors.As(err, &blocked) {
				fmt.Fprintf(w, "\n[Blocked a proposed command: %s]\n\n", blocked.reason)
				continueTurn(shieldBlockedMessage(blocked.reason))
				return
			}
			if err != nil {
				fmt.Fprintf(w, "\n[Edit not proposed: %v]\n\n", err)
				continueTurn(fmt.Sprintf("[PRECONDITION FAILED: %v]\nNothing was written. Fix the arguments and try again, or ask the user.", err))
				return
			}
			editJSON, _ := json.Marshal(edit)
			editStr := string(editJSON)
			cmdID := tc.ID
			if cmdID == "" {
				cmdID = uuid.NewString()
			}
			label := tc.Function.Name + " " + edit.Display
			if _, err := createCommandWithEdit(s.db, cmdID, convo.ID, tc.ID, label, folder, &editStr); err != nil {
				log.Printf("warning: failed to save pending edit: %v", err)
			}
			if err := touchConversation(s.db, convo.ID); err != nil {
				log.Printf("warning: failed to touch conversation: %v", err)
			}
			marker, _ := json.Marshal(map[string]any{"id": cmdID, "command": label, "diff": edit.Diff, "missing": missingPaths(folder)(result.Content)})
			fmt.Fprintf(w, "\n<<<TOOL_CALL>>>%s\n", marker)
			return
		}

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

		if ok, reason := checkCommandPreconditions(args.Command, *convo.AttachedFolder); !ok && !ablated["preconditions"] {
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

		marker, _ := json.Marshal(map[string]any{"id": cmdID, "command": args.Command, "missing": missingPaths(*convo.AttachedFolder)(result.Content)})
		fmt.Fprintf(w, "\n<<<TOOL_CALL>>>%s\n", marker)
		return
	}

	if result.Content != "" {
		if err := insertAssistantMessage(s.db, convo.ID, result.Content, result.Thinking, result.TokensPerSec); err != nil {
			log.Printf("warning: failed to save assistant message: %v", err)
		}
		var missing []string
		if folder := folderOf(convo.Conversation); folder != nil {
			missing = missingPaths(*folder)(result.Content)
		}
		if result.TokensPerSec > 0 || missing != nil {
			stats, _ := json.Marshal(map[string]any{"tokensPerSec": math.Round(result.TokensPerSec*10) / 10, "missing": missing})
			fmt.Fprintf(w, "\n<<<STATS>>>%s\n", stats)
		}
		s.summarizeInBackground(user, convo.ID)
	}
	if err := touchConversation(s.db, convo.ID); err != nil {
		log.Printf("warning: failed to touch conversation: %v", err)
	}
}

// Skipped when images were sent: their cost is a separate estimate and would skew the text ratio.
func (s *Server) calibrateTokenRatio(convoID string, history []OllamaMessage, toolsTokens, promptTokens int) {
	if promptTokens == 0 {
		return
	}
	for _, m := range history {
		if len(m.Images) > 0 {
			return
		}
	}
	estimate := tokenCounter(1).messages(history) + toolsTokens
	if err := setTokenRatio(s.db, convoID, clampRatio(float64(promptTokens)/float64(estimate))); err != nil {
		log.Printf("warning: failed to save token ratio: %v", err)
	}
}
