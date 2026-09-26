package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
