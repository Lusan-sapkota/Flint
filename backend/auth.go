package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "flint_session"
const sessionTTL = 30 * 24 * time.Hour

type ctxKey string

const userCtxKey ctxKey = "user"

func userFromContext(r *http.Request) *User {
	u, _ := r.Context().Value(userCtxKey).(*User)
	return u
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionUser(r *http.Request) (*User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, nil
	}
	return getSessionUser(s.db, cookie.Value)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if user == nil {
			clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}

		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) requireAuthPage(next func(w http.ResponseWriter, r *http.Request, user *User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if user == nil {
			clearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r, user)
	}
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.FullName = strings.TrimSpace(body.FullName)

	if body.FullName == "" || body.Email == "" || len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, "full name, email, and a password of at least 8 characters are required")
		return
	}

	existing, err := getUserByEmail(s.db, body.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "an account with that email already exists")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	user, err := createUser(s.db, uuid.NewString(), body.FullName, body.Email, string(hash))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.startSession(w, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	user, err := getUserByEmail(s.db, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if user == nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := s.startSession(w, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = deleteSession(s.db, cookie.Value)
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFromContext(r))
}

func (s *Server) startSession(w http.ResponseWriter, userID string) error {
	token, err := newSessionToken()
	if err != nil {
		return err
	}
	sess, err := createSession(s.db, token, userID, sessionTTL)
	if err != nil {
		return err
	}
	setSessionCookie(w, sess.ID, time.UnixMilli(sess.ExpiresAt))
	return nil
}

// handleUpdateProfile changes the name and email. A new email needs the
// current password: it's the login, so a stolen session could otherwise
// change it and lock the owner out.
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.FullName = strings.TrimSpace(body.FullName)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if body.FullName == "" || body.Email == "" {
		writeError(w, http.StatusBadRequest, "full name and email are required")
		return
	}

	if body.Email != user.Email {
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
			writeError(w, http.StatusUnauthorized, "enter your current password to change your email")
			return
		}
		existing, err := getUserByEmail(s.db, body.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing != nil {
			writeError(w, http.StatusConflict, "an account with that email already exists")
			return
		}
	}

	if err := updateUserProfile(s.db, user.ID, body.FullName, body.Email); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"full_name": body.FullName, "email": body.Email})
}

// handleChangePassword keeps the current session and signs out every other
// one, the usual reason to change a password being a device you don't trust.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.CurrentPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := updateUserPassword(s.db, user.ID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := deleteOtherSessions(s.db, user.ID, cookie.Value); err != nil {
			log.Printf("warning: failed to sign out other sessions after a password change: %v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
