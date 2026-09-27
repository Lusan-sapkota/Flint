package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaxAgentsForDefaultsAndSettings(t *testing.T) {
	s := &Server{ollama: NewOllamaClient(), defaultOllamaURL: "http://ollama.test"}
	s.ollama.models.Store("http://ollama.test local", OllamaModelInfo{Name: "local"})
	s.ollama.models.Store("http://ollama.test big:cloud", OllamaModelInfo{Name: "big:cloud", RemoteHost: "https://ollama.com"})
	n := func(v int) *int { return &v }

	for _, tc := range []struct {
		name  string
		user  *User
		model string
		want  int
	}{
		{"local default", &User{}, "local", defaultMaxAgents},
		{"cloud default", &User{}, "big:cloud", cloudMaxAgents},
		{"local setting", &User{MaxAgents: n(48)}, "local", 48},
		{"local setting leaves cloud alone", &User{MaxAgents: n(48)}, "big:cloud", cloudMaxAgents},
		{"cloud setting", &User{CloudMaxAgents: n(3)}, "big:cloud", 3},
		{"cloud setting leaves local alone", &User{CloudMaxAgents: n(3)}, "local", defaultMaxAgents},
	} {
		if got := s.maxAgentsFor(tc.user, Conversation{Model: tc.model}); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestMaxAgentsSettingValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	createUser(db, "alice", "alice", "alice@x.io", "h")

	patch := func(body string) (int, *User) {
		alice, _ := getUserByID(db, "alice")
		r := httptest.NewRequest(http.MethodPatch, "/api/me/settings", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, alice))
		w := httptest.NewRecorder()
		s.handleUpdateSettings(w, r)
		u, _ := getUserByID(db, "alice")
		return w.Code, u
	}

	if code, u := patch(`{"max_agents": 6, "cloud_max_agents": 64}`); code != 200 || *u.MaxAgents != 6 || *u.CloudMaxAgents != 64 {
		t.Fatalf("valid values should save, got %d %+v", code, u)
	}
	for _, body := range []string{`{"max_agents": 0}`, `{"max_agents": 65}`, `{"cloud_max_agents": -1}`, `{"max_agents": "4"}`, `{"max_agents": 2.5}`} {
		if code, u := patch(body); code != 400 || *u.MaxAgents != 6 || *u.CloudMaxAgents != 64 {
			t.Errorf("%s: want 400 and nothing changed, got %d %+v", body, code, u)
		}
	}
	if code, u := patch(`{"max_agents": null}`); code != 200 || u.MaxAgents != nil || *u.CloudMaxAgents != 64 {
		t.Errorf("null should go back to Auto for that one only, got %d %+v", code, u)
	}
}

func TestAgentRunsAreScopedToTheirOwner(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", "h")
	}
	createConversation(db, "alice-chat", "alice", "m")
	now := time.Now().UnixMilli()
	run := AgentRun{ID: "run1", ConversationID: "alice-chat", Task: "t", Status: "planned", CreatedAt: now, UpdatedAt: now, Agents: []Agent{
		{ID: "a1", Position: 0, Task: "read the README", Files: []string{"README.md"}, Status: "queued"},
		{ID: "a2", Position: 1, Task: "search", Files: []string{}, WebQuery: "go release", Status: "queued"},
	}}
	if err := createAgentRun(db, run); err != nil {
		t.Fatal(err)
	}
	insertAgentMessage(db, "a1", "user", "full transcript")

	get := func(user string, h http.HandlerFunc, runID, agentID string) (int, string) {
		u, _ := getUserByID(db, user)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("id", runID)
		r.SetPathValue("agentId", agentID)
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code, w.Body.String()
	}

	code, body := get("alice", s.handleGetAgentRun, "run1", "")
	var got AgentRun
	json.Unmarshal([]byte(body), &got)
	if code != 200 || len(got.Agents) != 2 || got.Agents[0].Files[0] != "README.md" || got.Agents[1].WebQuery != "go release" {
		t.Fatalf("owner should get the run with its agents in order, got %d %s", code, body)
	}
	if code, body := get("alice", s.handleGetAgentTranscript, "run1", "a1"); code != 200 || !strings.Contains(body, "full transcript") {
		t.Fatalf("owner should get the transcript, got %d %s", code, body)
	}

	for _, tc := range []struct {
		user, runID, agentID string
		h                    http.HandlerFunc
	}{
		{"bob", "run1", "", s.handleGetAgentRun},
		{"bob", "run1", "a1", s.handleGetAgentTranscript},
		{"alice", "nope", "", s.handleGetAgentRun},
		{"alice", "run1", "nope", s.handleGetAgentTranscript},
		{"alice", "nope", "a1", s.handleGetAgentTranscript},
	} {
		if code, _ := get(tc.user, tc.h, tc.runID, tc.agentID); code != 404 {
			t.Errorf("%+v: want 404, got %d", tc, code)
		}
	}

	deleteConversation(db, "alice-chat", "alice")
	var left int
	db.QueryRow(`SELECT (SELECT count(*) FROM agent_runs) + (SELECT count(*) FROM agents) + (SELECT count(*) FROM agent_messages)`).Scan(&left)
	if left != 0 {
		t.Errorf("deleting the chat should delete its runs, agents and transcripts, %d rows left", left)
	}
}
