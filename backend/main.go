package main

import (
	"log"
	"net/http"
	"os"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := getenv("PORT", "8080")
	dbPath := getenv("DB_PATH", "data/chat.db")
	ollamaBaseURL := getenv("OLLAMA_BASE_URL", "http://localhost:11434")

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	srv := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: ollamaBaseURL}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Auth (unauthenticated)
	mux.HandleFunc("POST /api/signup", srv.handleSignup)
	mux.HandleFunc("POST /api/login", srv.handleLogin)
	mux.HandleFunc("POST /api/logout", srv.handleLogout)

	// Everything below requires a valid session, and is scoped to that user.
	mux.HandleFunc("GET /api/me", srv.requireAuth(srv.handleMe))
	mux.HandleFunc("PATCH /api/me/settings", srv.requireAuth(srv.handleUpdateSettings))

	mux.HandleFunc("GET /api/models", srv.requireAuth(srv.handleListModels))
	mux.HandleFunc("GET /api/conversations", srv.requireAuth(srv.handleListConversations))
	mux.HandleFunc("POST /api/conversations", srv.requireAuth(srv.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations/{id}", srv.requireAuth(srv.handleGetConversation))
	mux.HandleFunc("DELETE /api/conversations/{id}", srv.requireAuth(srv.handleDeleteConversation))
	mux.HandleFunc("POST /api/conversations/{id}/messages", srv.requireAuth(srv.handlePostMessage))

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../frontend/static"))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Flint backend is running"))
	})

	log.Printf("flint listening on :%s (ollama: %s, db: %s)", port, ollamaBaseURL, dbPath)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
