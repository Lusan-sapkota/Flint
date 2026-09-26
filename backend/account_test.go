package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestDeleteAccountNeedsPasswordAndAnswersAndWipesOnlyThatAccount(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db, attachmentsDir: dir}

	hash := func(v string) string {
		h, _ := bcrypt.GenerateFromPassword([]byte(v), bcrypt.MinCost)
		return string(h)
	}
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", hash("password123"))
		createConversation(db, u+"-chat", u, "m")
		msgID, _ := insertMessage(db, u+"-chat", "user", "hi")
		os.WriteFile(filepath.Join(dir, u+".png"), []byte("img"), 0o644)
		createAttachment(db, u+"-att", msgID, "image/png", u+".png", u+".png")
	}
	setSecurityQuestions(db, "alice", []SecurityQuestion{
		{ID: "q1", Question: "Pet?", AnswerHash: hash("rex")},
		{ID: "q2", Question: "City?", AnswerHash: hash("kathmandu")},
	})
	alice, _ := getUserByID(db, "alice")

	del := func(body string) int {
		r := httptest.NewRequest(http.MethodDelete, "/api/me", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, alice))
		w := httptest.NewRecorder()
		s.handleDeleteAccount(w, r)
		return w.Code
	}
	for _, body := range []string{
		`{"password":"wrong","answers":["rex","kathmandu"]}`,
		`{"password":"password123","answers":["rex","pokhara"]}`,
		`{"password":"password123","answers":["rex"]}`,
		`{"password":"password123"}`,
	} {
		if code := del(body); code != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401", body, code)
		}
	}
	if u, _ := getUserByID(db, "alice"); u == nil {
		t.Fatal("a rejected attempt must not delete the account")
	}

	if code := del(`{"password":"password123","answers":[" Rex ","KATHMANDU"]}`); code != http.StatusNoContent {
		t.Fatalf("correct password and answers: got %d, want 204", code)
	}
	if u, _ := getUserByID(db, "alice"); u != nil {
		t.Error("account should be gone")
	}
	if c, _ := listConversations(db, "alice"); len(c) != 0 {
		t.Errorf("conversations should cascade, got %d", len(c))
	}
	if _, err := os.Stat(filepath.Join(dir, "alice.png")); !os.IsNotExist(err) {
		t.Error("attachment file should be removed")
	}
	if c, _ := listConversations(db, "bob"); len(c) != 1 {
		t.Error("another account's chats must be untouched")
	}
	if _, err := os.Stat(filepath.Join(dir, "bob.png")); err != nil {
		t.Error("another account's files must be untouched")
	}
}

func TestProfileAndPasswordChanges(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	createUser(db, "alice", "Alice", "alice@x.io", string(hash))
	createUser(db, "bob", "Bob", "bob@x.io", string(hash))
	createSession(db, "this-device", "alice", time.Hour)
	createSession(db, "other-device", "alice", time.Hour)

	call := func(h http.HandlerFunc, body string) int {
		u, _ := getUserByID(db, "alice")
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "this-device"})
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}

	// In order: the last case changes the email the earlier ones compare against.
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"full_name":"Alice R","email":"alice@x.io"}`, http.StatusOK},
		{`{"full_name":"Alice R","email":"new@x.io"}`, http.StatusUnauthorized},
		{`{"full_name":"Alice R","email":"bob@x.io","password":"password123"}`, http.StatusConflict},
		{`{"full_name":"","email":"alice@x.io"}`, http.StatusBadRequest},
		{`{"full_name":"Alice R","email":" New@X.io ","password":"password123"}`, http.StatusOK},
	} {
		if code := call(s.handleUpdateProfile, c.body); code != c.want {
			t.Errorf("profile %s: got %d, want %d", c.body, code, c.want)
		}
	}
	if u, _ := getUserByID(db, "alice"); u.FullName != "Alice R" || u.Email != "new@x.io" {
		t.Errorf("profile not saved: %+v", u)
	}

	if code := call(s.handleChangePassword, `{"current_password":"wrong","new_password":"newpassword1"}`); code != http.StatusUnauthorized {
		t.Errorf("wrong current password: got %d", code)
	}
	if code := call(s.handleChangePassword, `{"current_password":"password123","new_password":"short"}`); code != http.StatusBadRequest {
		t.Errorf("short new password: got %d", code)
	}
	if code := call(s.handleChangePassword, `{"current_password":"password123","new_password":"newpassword1"}`); code != http.StatusNoContent {
		t.Fatalf("change: got %d", code)
	}
	u, _ := getUserByID(db, "alice")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("newpassword1")) != nil {
		t.Error("new password should work")
	}
	if kept, _ := getSessionUser(db, "this-device"); kept == nil {
		t.Error("this device should stay signed in")
	}
	if other, _ := getSessionUser(db, "other-device"); other != nil {
		t.Error("other devices should be signed out")
	}
}

func TestUserJSONNeverIncludesTheBraveKey(t *testing.T) {
	key := "BSA-secret-key-1234"
	out, _ := json.Marshal(User{ID: "u", BraveAPIKey: &key})
	if strings.Contains(string(out), key) {
		t.Fatalf("the key leaked into the user JSON: %s", out)
	}
}
