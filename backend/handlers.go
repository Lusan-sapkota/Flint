package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
)

type Server struct {
	db               *sql.DB
	ollama           *OllamaClient
	defaultOllamaURL string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ollamaURLFor returns the user's own Ollama endpoint if they've set one,
// otherwise the server-wide default.
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

	var body struct {
		OllamaBaseURL *string `json:"ollama_base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := updateUserOllamaURL(s.db, user.ID, body.OllamaBaseURL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated, err := getUserByID(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
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

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	found, err := deleteConversation(s.db, id, user.ID)
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

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	id := r.PathValue("id")

	var body struct {
		Content string `json:"content"`
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

	if err := insertMessage(s.db, id, "user", body.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := maybeSetTitle(s.db, id, body.Content); err != nil {
		log.Printf("warning: failed to set title: %v", err)
	}

	history := make([]OllamaMessage, 0, len(convo.Messages)+1)
	for _, m := range convo.Messages {
		history = append(history, OllamaMessage{Role: m.Role, Content: m.Content})
	}
	history = append(history, OllamaMessage{Role: "user", Content: body.Content})

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)

	full, err := s.ollama.StreamChat(r.Context(), s.ollamaURLFor(user), convo.Model, history, func(token string) {
		w.Write([]byte(token))
		if canFlush {
			flusher.Flush()
		}
	})
	if err != nil {
		log.Printf("ollama stream error: %v", err)
		w.Write([]byte("\n[error: " + err.Error() + "]"))
	}

	if full != "" {
		if err := insertMessage(s.db, id, "assistant", full); err != nil {
			log.Printf("warning: failed to save assistant message: %v", err)
		}
	}
	if err := touchConversation(s.db, id); err != nil {
		log.Printf("warning: failed to touch conversation: %v", err)
	}
}
