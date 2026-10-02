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
	loadAblations()
	port := getenv("PORT", "8080")
	// Localhost by default: model-proposed commands run as this process and signup is open.
	host := getenv("HOST", "127.0.0.1")
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

	srv := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: ollamaBaseURL, attachmentsDir: attachmentsDir, pages: loadPages()}
	loginLimiter := newRateLimiter(5, 5*time.Minute)
	signupLimiter := newRateLimiter(5, 5*time.Minute)
	recoveryLimiter := newRateLimiter(5, 30*time.Minute)
	deleteAccountLimiter := newRateLimiter(5, 30*time.Minute)
	accountLimiter := newRateLimiter(10, 15*time.Minute)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz(srv))

	mux.HandleFunc("POST /api/signup", signupLimiter.middleware(srv.handleSignup))
	mux.HandleFunc("POST /api/login", loginLimiter.middleware(srv.handleLogin))
	mux.HandleFunc("POST /api/logout", srv.handleLogout)
	mux.HandleFunc("POST /api/recovery/questions", recoveryLimiter.middleware(srv.handleRecoveryQuestions))
	mux.HandleFunc("POST /api/recovery/reset", recoveryLimiter.middleware(srv.handleRecoveryReset))

	mux.HandleFunc("GET /api/me", srv.requireAuth(srv.handleMe))
	mux.HandleFunc("PATCH /api/me/settings", srv.requireAuth(srv.handleUpdateSettings))
	mux.HandleFunc("GET /api/me/security-questions", srv.requireAuth(srv.handleGetSecurityQuestions))
	mux.HandleFunc("PUT /api/me/security-questions", srv.requireAuth(srv.handleSetSecurityQuestions))
	mux.HandleFunc("GET /api/me/web-searches", srv.requireAuth(srv.handleListWebSearches))
	mux.HandleFunc("PATCH /api/me/profile", accountLimiter.middleware(srv.requireAuth(srv.handleUpdateProfile)))
	mux.HandleFunc("POST /api/me/password", accountLimiter.middleware(srv.requireAuth(srv.handleChangePassword)))
	mux.HandleFunc("DELETE /api/me", deleteAccountLimiter.middleware(srv.requireAuth(srv.handleDeleteAccount)))

	mux.HandleFunc("GET /api/models", srv.requireAuth(srv.handleListModels))
	mux.HandleFunc("GET /api/models/running", srv.requireAuth(srv.handleRunningModels))
	mux.HandleFunc("POST /api/models/show", srv.requireAuth(srv.handleShowModel))
	mux.HandleFunc("POST /api/models/pull", srv.requireAuth(srv.handlePullModel))
	mux.HandleFunc("POST /api/models/load", srv.requireAuth(srv.handleLoadModel))
	mux.HandleFunc("POST /api/models/unload", srv.requireAuth(srv.handleUnloadModel))
	mux.HandleFunc("DELETE /api/models", srv.requireAuth(srv.handleDeleteModel))
	mux.HandleFunc("GET /api/conversations", srv.requireAuth(srv.handleListConversations))
	mux.HandleFunc("POST /api/conversations", srv.requireAuth(srv.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations/search", srv.requireAuth(srv.handleSearchConversations))
	mux.HandleFunc("GET /api/conversations/{id}", srv.requireAuth(srv.handleGetConversation))
	mux.HandleFunc("PATCH /api/conversations/{id}", srv.requireAuth(srv.handleRenameConversation))
	mux.HandleFunc("DELETE /api/conversations/{id}", srv.requireAuth(srv.handleDeleteConversation))
	mux.HandleFunc("GET /api/agent-runs/{id}", srv.requireAuth(srv.handleGetAgentRun))
	mux.HandleFunc("POST /api/agent-runs/{id}/run", srv.requireAuth(srv.handleRunAgentRun))
	mux.HandleFunc("POST /api/agent-runs/{id}/discard", srv.requireAuth(srv.handleDiscardAgentRun))
	mux.HandleFunc("POST /api/agent-runs/{id}/commands/{cmdId}/{action}", srv.requireAuth(srv.handleDecideAgentCommand))
	mux.HandleFunc("GET /api/agent-runs/{id}/agents/{agentId}", srv.requireAuth(srv.handleGetAgentTranscript))
	mux.HandleFunc("GET /api/fs/dirs", srv.requireAuth(srv.handleListDirs))
	mux.HandleFunc("GET /api/memories", srv.requireAuth(srv.handleListMemories))
	mux.HandleFunc("POST /api/memories", srv.requireAuth(srv.handleCreateMemory))
	mux.HandleFunc("PUT /api/memories/{id}", srv.requireAuth(srv.handleUpdateMemory))
	mux.HandleFunc("DELETE /api/memories/{id}", srv.requireAuth(srv.handleDeleteMemory))
	mux.HandleFunc("GET /api/attachments/{id}", srv.requireAuth(srv.handleGetAttachment))
	mux.HandleFunc("POST /api/conversations/{id}/attach", srv.requireAuth(srv.handleAttachFolder))
	mux.HandleFunc("POST /api/conversations/{id}/messages", srv.requireAuth(srv.handlePostMessage))
	mux.HandleFunc("PUT /api/conversations/{id}/messages/last", srv.requireAuth(srv.handleEditLastMessage))
	mux.HandleFunc("POST /api/conversations/{id}/commands/{cmdId}/approve", srv.requireAuth(srv.handleApproveCommand))
	mux.HandleFunc("GET /api/conversations/{id}/review", srv.requireAuth(srv.handleGetReview))
	mux.HandleFunc("POST /api/conversations/{id}/review", srv.requireAuth(srv.handleDecideReview))
	mux.HandleFunc("POST /api/conversations/{id}/commands/{cmdId}/deny", srv.requireAuth(srv.handleDenyCommand))

	static := http.StripPrefix("/static/", http.FileServer(http.Dir("../frontend/static")))
	// no-cache keeps 304s but stops browsers heuristically serving stale JS/CSS.
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	}))

	mux.HandleFunc("GET /login", srv.handleLoginPage)
	mux.HandleFunc("GET /signup", srv.handleSignupPage)
	mux.HandleFunc("GET /recover", srv.handleRecoverPage)
	mux.HandleFunc("GET /settings", srv.requireAuthPage(srv.handleSettingsPage))
	mux.HandleFunc("GET /{$}", srv.requireAuthPage(srv.handleChatPage))
	mux.HandleFunc("GET /c/{id}", srv.requireAuthPage(srv.handleChatPage))
	mux.HandleFunc("POST /conversations", srv.requireAuthPage(srv.handleCreateConversationPage))

	httpServer := &http.Server{Addr: host + ":" + port, Handler: mux}

	go func() {
		log.Printf("flint listening on http://%s:%s (ollama: %s, db: %s)", host, port, ollamaBaseURL, dbPath)
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
