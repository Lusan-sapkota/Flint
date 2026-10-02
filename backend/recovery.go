package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	minSecurityQuestions = 2
	maxSecurityQuestions = 5
)

func normalizeAnswer(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (s *Server) handleGetSecurityQuestions(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	questions, err := getSecurityQuestionsForUser(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, questions)
}

func (s *Server) handleSetSecurityQuestions(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	var body struct {
		Questions []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"questions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(body.Questions) < minSecurityQuestions || len(body.Questions) > maxSecurityQuestions {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("provide between %d and %d questions", minSecurityQuestions, maxSecurityQuestions))
		return
	}

	toStore := make([]SecurityQuestion, 0, len(body.Questions))
	for _, q := range body.Questions {
		question := strings.TrimSpace(q.Question)
		answer := normalizeAnswer(q.Answer)
		if question == "" || answer == "" {
			writeError(w, http.StatusBadRequest, "each question and answer must be non-empty")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(answer), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		toStore = append(toStore, SecurityQuestion{ID: uuid.NewString(), Question: question, AnswerHash: string(hash)})
	}

	if err := setSecurityQuestions(s.db, user.ID, toStore); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRecoveryQuestions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	_, questions, err := getSecurityQuestionsByEmail(s.db, strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	texts := []string{}
	if len(questions) >= minSecurityQuestions {
		for _, q := range questions {
			texts = append(texts, q.Question)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": texts})
}

func (s *Server) handleRecoveryReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string   `json:"email"`
		Answers     []string `json:"answers"`
		NewPassword string   `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	userID, questions, err := getSecurityQuestionsByEmail(s.db, strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(questions) < minSecurityQuestions || len(body.Answers) != len(questions) {
		writeError(w, http.StatusUnauthorized, "incorrect answers")
		return
	}

	for i, q := range questions {
		if bcrypt.CompareHashAndPassword([]byte(q.AnswerHash), []byte(normalizeAnswer(body.Answers[i]))) != nil {
			writeError(w, http.StatusUnauthorized, "incorrect answers")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := updateUserPassword(s.db, userID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := deleteAllSessionsForUser(s.db, userID); err != nil {
		log.Printf("warning: failed to invalidate sessions after password recovery: %v", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// Security answers are required too when set: a stolen session alone must not erase an account.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		Password string   `json:"password"`
		Answers  []string `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	const wrong = "incorrect password or answers"
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		writeError(w, http.StatusUnauthorized, wrong)
		return
	}
	questions, err := getSecurityQuestionsForUser(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(body.Answers) != len(questions) {
		writeError(w, http.StatusUnauthorized, wrong)
		return
	}
	for i, q := range questions {
		if bcrypt.CompareHashAndPassword([]byte(q.AnswerHash), []byte(normalizeAnswer(body.Answers[i]))) != nil {
			writeError(w, http.StatusUnauthorized, wrong)
			return
		}
	}

	paths, err := getAttachmentPathsForUser(s.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := deleteUser(s.db, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, p := range paths {
		if err := os.Remove(filepath.Join(s.attachmentsDir, p)); err != nil && !os.IsNotExist(err) {
			log.Printf("warning: failed to remove attachment file %s: %v", p, err)
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
