package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

func TestParseAgentCommand(t *testing.T) {
	for _, c := range []struct {
		in   string
		task string
		ok   bool
	}{
		{"@agent summarize each file", "summarize each file", true},
		{"  @Agent   do it  ", "do it", true},
		{"@agent", "", true},
		{"@agents do it", "", false},
		{"please @agent do it", "", false},
	} {
		task, ok := parseAgentCommand(c.in)
		if task != c.task || ok != c.ok {
			t.Errorf("%q: got %q %v", c.in, task, ok)
		}
	}
}

func TestAgentFilesListsOnlyDirectTextFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o644)
	os.WriteFile(filepath.Join(dir, "img.bin"), []byte{1, 0, 2}, 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "deep.txt"), []byte("x"), 0o644)
	got, err := agentFiles(dir)
	if err != nil || len(got) != 1 || got["a.txt"] != 5 {
		t.Fatalf("want only a.txt (5 bytes), got %v %v", got, err)
	}
}

func TestValidatePlan(t *testing.T) {
	files := map[string]int64{"README.md": 100, "main.py": 200}
	plan := []planSubtask{
		{Task: "Find the port", Files: []string{"README.md", "README.md"}},
		{Task: "Read secrets", Files: []string{"../../etc/passwd", "/etc/shadow", "sub/deep.txt", ".env"}},
		{Task: "Search", Files: []string{}, WebQuery: "latest go release"},
		{Task: "  ", Files: []string{"main.py"}},
		{Task: "Imports", Files: []string{" main.py ", "go.txt"}},
	}

	got := validatePlan(plan, files, false)
	if len(got) != 2 {
		t.Fatalf("without a key: want the README and main.py subtasks only, got %+v", got)
	}
	if !slices.Equal(got[0].Files, []string{"README.md"}) || !slices.Equal(got[1].Files, []string{"main.py"}) {
		t.Errorf("files should be deduplicated, trimmed and limited to the folder list, got %+v", got)
	}

	got = validatePlan(plan, files, true)
	if len(got) != 3 || got[1].WebQuery != "latest go release" || len(got[1].Files) != 0 {
		t.Fatalf("with a key the web subtask stays, got %+v", got)
	}

	named := validatePlan([]planSubtask{
		{Task: "Find TAX_RATE in main.py and README.md.", Files: []string{}},
		{Task: "Check domain.py and main.pyc", Files: []string{}},
		{Task: "Who is on call?", Files: []string{}},
	}, files, false)
	if len(named) != 1 || !slices.Equal(named[0].Files, []string{"README.md", "main.py"}) {
		t.Errorf("files named in the task should become its inputs, whole names only, got %+v", named)
	}

	long := planSubtask{Task: strings.Repeat("é", maxAgentTask+50), Files: []string{"main.py"}, WebQuery: strings.Repeat("q", 300)}
	got = validatePlan([]planSubtask{long}, files, true)
	if len([]rune(got[0].Task)) != maxAgentTask || len(got[0].WebQuery) != maxAgentWebQuery {
		t.Errorf("task and query should be capped, got %d and %d", len([]rune(got[0].Task)), len(got[0].WebQuery))
	}

	many := make([]planSubtask, maxPlanSubtasks+3)
	for i := range many {
		many[i] = planSubtask{Task: "t", Files: []string{"main.py"}}
	}
	if got := validatePlan(many, files, false); len(got) != maxPlanSubtasks {
		t.Errorf("want at most %d subtasks, got %d", maxPlanSubtasks, len(got))
	}
}

func TestAllocateInputsAndPlanNotes(t *testing.T) {
	got := allocateInputs([]int64{100, 5000, 300}, 1000)
	if !slices.Equal(got, []int{100, 600, 300}) {
		t.Errorf("small inputs whole, the rest to the big one: got %v", got)
	}
	got = allocateInputs([]int64{800, 900}, 1000)
	if !slices.Equal(got, []int{500, 500}) {
		t.Errorf("two big inputs share equally: got %v", got)
	}
	if got := allocateInputs([]int64{10, 20}, 1000); !slices.Equal(got, []int{10, 20}) {
		t.Errorf("everything fits: got %v", got)
	}

	files := map[string]int64{"small.txt": 500, "large_log.txt": 24695}
	agents := []Agent{
		{Files: []string{"small.txt"}},
		{Files: []string{"large_log.txt"}},
		{Files: []string{"small.txt"}, WebQuery: "q"},
	}
	planNotes(agents, files, 10000)
	if agents[0].Note != "" || agents[2].Note != "" {
		t.Errorf("inputs that fit need no note, got %q / %q", agents[0].Note, agents[2].Note)
	}
	if !strings.Contains(agents[1].Note, "large_log.txt (9.8 KB of 24.1 KB") {
		t.Errorf("the log should be reported cut, got %q", agents[1].Note)
	}
	if n := agentInputChars(8192); n != (8192-responseReserve-agentPromptTokens)*2 || n >= 24695 {
		t.Errorf("at 8192 the fixture's 24.7 KB log must not count as fitting, got a budget of %d", n)
	}
}

// fakePlanner answers /api/chat with a fixed plan, and records the request.
func fakePlanner(t *testing.T, plan string, got *map[string]any) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(got)
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": plan}, "done": true})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPlanAgentRunSavesAValidatedPlanOrDeclines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("port 7070"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("on call: Omar"), 0o644)
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createUser(db, "alice", "alice", "alice@x.io", "h")
	createConversation(db, "c1", "alice", "m")
	setAttachedFolder(db, "c1", dir)
	user, _ := getUserByID(db, "alice")

	var req map[string]any
	srv := fakePlanner(t, `{"subtasks":[{"task":"Find the port","files":["README.md"],"web_query":""},{"task":"Who is on call","files":["notes.txt"],"web_query":"on call rota"},{"task":"Escape","files":["../x"],"web_query":""}]}`, &req)
	s := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: srv.URL}
	s.ollama.models.Store(srv.URL+" m", OllamaModelInfo{Name: "m"})
	convo, _ := getConversation(db, "c1", "alice")

	run, err := s.planAgentRun(context.Background(), user, convo, "port and on-call?")
	if err != nil || run == nil || len(run.Agents) != 2 {
		t.Fatalf("want a two-agent plan, got %+v %v", run, err)
	}
	if !slices.Equal(run.FolderFiles, []string{"README.md", "notes.txt"}) {
		t.Errorf("the card needs the folder's files to add from, got %v", run.FolderFiles)
	}
	if run.Agents[1].WebQuery != "" {
		t.Error("no Brave key: the web query must be dropped")
	}
	if req["format"] == nil || req["options"].(map[string]any)["num_ctx"] != float64(boostedNumCtx) {
		t.Errorf("the plan call must send the schema and the chat's own num_ctx, got %v", req)
	}
	system := req["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "- README.md (9 bytes)") || !strings.Contains(system, "NOT available") {
		t.Errorf("the prompt should list the folder's files and say web is off, got %q", system)
	}
	saved, _ := getAgentRun(db, run.ID, "alice")
	if saved == nil || saved.Status != "planned" || len(saved.Agents) != 2 {
		t.Fatalf("the plan should be saved as planned, got %+v", saved)
	}

	srv2 := fakePlanner(t, `{"subtasks":[{"task":"Everything","files":["README.md"],"web_query":""}]}`, &req)
	s.defaultOllamaURL = srv2.URL
	s.ollama.models.Store(srv2.URL+" m", OllamaModelInfo{Name: "m"})
	if run, err := s.planAgentRun(context.Background(), user, convo, "port?"); run != nil || err != nil {
		t.Errorf("one subtask doesn't split, got %+v %v", run, err)
	}
}

func TestRunAndDiscardNeedAPlannedRunOfYourOwn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("port 7070"), 0o644)
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: "http://ollama.test"}
	s.ollama.models.Store("http://ollama.test m", OllamaModelInfo{Name: "m"})
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", "h")
	}
	createConversation(db, "c1", "alice", "m")
	setAttachedFolder(db, "c1", dir)
	newRun := func(id string) {
		createAgentRun(db, AgentRun{ID: id, ConversationID: "c1", Task: "t", Status: "planned", Agents: []Agent{{ID: id + "a", Task: "x", Files: []string{"README.md"}, Status: "queued"}}})
	}
	newRun("r1")
	newRun("r2")

	call := func(user string, h http.HandlerFunc, runID, body string) int {
		u, _ := getUserByID(db, user)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.SetPathValue("id", runID)
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}

	if code := call("bob", s.handleDiscardAgentRun, "r1", ""); code != 404 {
		t.Errorf("another account discarding: want 404, got %d", code)
	}
	if code := call("bob", s.handleRunAgentRun, "r1", `{"agents":[]}`); code != 404 {
		t.Errorf("another account running: want 404, got %d", code)
	}
	if code := call("alice", s.handleDiscardAgentRun, "r1", ""); code != 204 {
		t.Fatalf("discard: want 204, got %d", code)
	}
	if code := call("alice", s.handleDiscardAgentRun, "r1", ""); code != 409 {
		t.Errorf("discarding twice: want 409, got %d", code)
	}
	if code := call("alice", s.handleRunAgentRun, "r1", `{"agents":[{"task":"x","files":["README.md"],"web_query":""}]}`); code != 409 {
		t.Errorf("running a discarded plan: want 409, got %d", code)
	}

	if code := call("alice", s.handleRunAgentRun, "r2", `{"agents":[{"task":"steal","files":["../../etc/passwd"],"web_query":"x"}]}`); code != 400 {
		t.Errorf("an edit pointing outside the folder leaves nothing to run: want 400, got %d", code)
	}
	call("alice", s.handleRunAgentRun, "r2", `{"agents":[{"task":"edited","files":["README.md","/etc/passwd"],"web_query":""}]}`)
	run, _ := getAgentRun(db, "r2", "alice")
	if len(run.Agents) != 1 || run.Agents[0].Task != "edited" || !slices.Equal(run.Agents[0].Files, []string{"README.md"}) {
		t.Errorf("the edited plan should be saved, validated, got %+v", run.Agents)
	}

	insertToolCallMessage(db, "c1", "", "", `[{"id":"call1","function":{"name":"run_shell","arguments":{"command":"ls"}}}]`)
	createCommand(db, "cmd1", "c1", "call1", "ls", dir)
	if code := call("alice", s.handleRunAgentRun, "r2", `{"agents":[{"task":"x","files":["README.md"],"web_query":""}]}`); code != 409 {
		t.Errorf("with a command pending: want 409, got %d", code)
	}
}

func TestTimelinePlacesAgentRunsByTime(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hi", CreatedAt: 100},
		{Role: "assistant", Content: "hello", CreatedAt: 200},
		{Role: "user", Content: "next", CreatedAt: 400},
	}
	runs := []AgentRun{{ID: "r1", Task: "split this", Status: "planned", CreatedAt: 300}}
	got := buildTimeline(messages, nil, nil, runs)
	var kinds []string
	for _, it := range got {
		kinds = append(kinds, it.Kind)
	}
	if !slices.Equal(kinds, []string{"user", "assistant", "user", "agentPlan", "user"}) || got[2].Content != "@agent split this" || got[3].Run.ID != "r1" {
		t.Fatalf("want the task and plan card between the reply and the next message, got %v %+v", kinds, got)
	}
}
