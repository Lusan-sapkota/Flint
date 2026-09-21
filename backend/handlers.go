package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

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
	if err := insertMessage(s.db, id, "system", manifest); err != nil {
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

	convo, err = getConversation(s.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	s.streamAssistantTurn(w, r, user, convo)
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

	cmd, err := getCommand(s.db, cmdID, convoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cmd == nil || cmd.Status != "pending" {
		writeError(w, http.StatusNotFound, "no pending command with that id")
		return
	}

	var resultText string
	if !approve {
		resultText = "User denied permission to run this command."
		if err := resolveCommand(s.db, cmd.ID, "denied", "", nil); err != nil {
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
		resultText = output
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
	s.streamAssistantTurn(w, r, user, convo)
}

func (s *Server) streamAssistantTurn(w http.ResponseWriter, r *http.Request, user *User, convo *ConversationWithMessages) {
	history := buildHistory(convo.Messages)

	var tools []OllamaTool
	if convo.AttachedFolder != nil && *convo.AttachedFolder != "" {
		tools = []OllamaTool{runShellTool}
	}

	flusher, canFlush := w.(http.Flusher)

	result, err := s.ollama.StreamChat(r.Context(), s.ollamaURLFor(user), convo.Model, history, tools, func(token string) {
		w.Write([]byte(token))
		if canFlush {
			flusher.Flush()
		}
	})
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
		if err := insertToolCallMessage(s.db, convo.ID, string(toolCallsJSON)); err != nil {
			log.Printf("warning: failed to save tool call message: %v", err)
		}

		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(tc.Function.Arguments, &args)

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
		if err := insertMessage(s.db, convo.ID, "assistant", result.Content); err != nil {
			log.Printf("warning: failed to save assistant message: %v", err)
		}
	}
	if err := touchConversation(s.db, convo.ID); err != nil {
		log.Printf("warning: failed to touch conversation: %v", err)
	}
}
