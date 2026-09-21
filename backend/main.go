package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func handleHealthz(srv *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status := map[string]string{"db": "ok", "ollama": "ok"}
		healthy := true

		if err := srv.db.PingContext(ctx); err != nil {
			status["db"] = "error: " + err.Error()
			healthy = false
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.defaultOllamaURL+"/api/version", nil)
		if err != nil {
			status["ollama"] = "error: " + err.Error()
			healthy = false
		} else {
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				status["ollama"] = "unreachable: " + err.Error()
				healthy = false
			} else {
				resp.Body.Close()
			}
		}

		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func main() {
	port := getenv("PORT", "8080")
	dbPath := getenv("DB_PATH", "data/chat.db")
	ollamaBaseURL := getenv("OLLAMA_BASE_URL", "http://localhost:11434")
	attachmentsDir := getenv("ATTACHMENTS_DIR", "data/attachments")

	if err := os.MkdirAll(attachmentsDir, 0o755); err != nil {
		log.Fatalf("failed to create attachments directory: %v", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	srv := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: ollamaBaseURL, attachmentsDir: attachmentsDir}
	// Separate limiter instances, not shared: exhausting login attempts
	// (e.g. a mistyped password) shouldn't also block signup from the same IP.
	loginLimiter := newRateLimiter(5, 5*time.Minute)
	signupLimiter := newRateLimiter(5, 5*time.Minute)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz(srv))

	mux.HandleFunc("POST /api/signup", signupLimiter.middleware(srv.handleSignup))
	mux.HandleFunc("POST /api/login", loginLimiter.middleware(srv.handleLogin))
	mux.HandleFunc("POST /api/logout", srv.handleLogout)

	mux.HandleFunc("GET /api/me", srv.requireAuth(srv.handleMe))
	mux.HandleFunc("PATCH /api/me/settings", srv.requireAuth(srv.handleUpdateSettings))

	mux.HandleFunc("GET /api/models", srv.requireAuth(srv.handleListModels))
	mux.HandleFunc("GET /api/models/running", srv.requireAuth(srv.handleRunningModels))
	mux.HandleFunc("POST /api/models/show", srv.requireAuth(srv.handleShowModel))
	mux.HandleFunc("POST /api/models/pull", srv.requireAuth(srv.handlePullModel))
	mux.HandleFunc("DELETE /api/models", srv.requireAuth(srv.handleDeleteModel))
	mux.HandleFunc("GET /api/conversations", srv.requireAuth(srv.handleListConversations))
	mux.HandleFunc("POST /api/conversations", srv.requireAuth(srv.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations/{id}", srv.requireAuth(srv.handleGetConversation))
	mux.HandleFunc("DELETE /api/conversations/{id}", srv.requireAuth(srv.handleDeleteConversation))
	mux.HandleFunc("GET /api/attachments/{id}", srv.requireAuth(srv.handleGetAttachment))
	mux.HandleFunc("POST /api/conversations/{id}/attach", srv.requireAuth(srv.handleAttachFolder))
	mux.HandleFunc("POST /api/conversations/{id}/messages", srv.requireAuth(srv.handlePostMessage))
	mux.HandleFunc("POST /api/conversations/{id}/commands/{cmdId}/approve", srv.requireAuth(srv.handleApproveCommand))
	mux.HandleFunc("POST /api/conversations/{id}/commands/{cmdId}/deny", srv.requireAuth(srv.handleDenyCommand))

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../frontend/static"))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Flint backend is running"))
	})

	httpServer := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("flint listening on :%s (ollama: %s, db: %s)", port, ollamaBaseURL, dbPath)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down, waiting for in-flight requests to finish...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown timed out, forcing close: %v", err)
	}
}
