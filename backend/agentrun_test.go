package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCutKeepsStartAndEndOnWholeLines(t *testing.T) {
	path := filepath.Join("..", "bench", "fixture", "large_log.txt")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	limit := agentInputChars(8192)
	got, err := readCut(path, info.Size(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > limit+100 || !strings.Contains(got, "cut to fit the window") {
		t.Fatalf("want the log cut to about %d chars with a marker, got %d", limit, len(got))
	}
	if !strings.HasPrefix(got, "2026-09-01T10:01:00 INFO request 1 ") || !strings.Contains(got, "ERROR 4471") || !strings.Contains(got[len(got)-100:], "request 400 ") {
		t.Errorf("the start, the ERROR line near the end and the last line should survive the cut, got start %q end %q", got[:60], got[len(got)-80:])
	}
	for _, line := range strings.Split(got, "\n") {
		if line != "" && !strings.HasPrefix(line, "2026-") && !strings.HasPrefix(line, "[... ") {
			t.Fatalf("a line was cut in half: %q", line)
		}
	}

	small, _ := readCut(filepath.Join("..", "bench", "fixture", "notes.txt"), 65, 1000)
	if strings.Contains(small, "cut to fit") || !strings.Contains(small, "Omar") {
		t.Errorf("a file that fits is read whole, got %q", small)
	}
	if cut := cutToChars(strings.Repeat("é", 500), 101); !utf8.ValidString(cut) {
		t.Error("cutting must never leave broken UTF-8")
	}
}

func TestParseAgentResult(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{`{"answer":"port 7070","found":true}`, true},
		{`{"answer":"","found":false}`, true},
		{`{"answer":"","found":true}`, false},
		{`{"answer":"x"}`, false},
		{`{"answer":7,"found":true}`, false},
		{`port 7070`, false},
	} {
		if _, ok := parseAgentResult(c.in); ok != c.ok {
			t.Errorf("%s: got ok=%v", c.in, ok)
		}
	}
	long, ok := parseAgentResult(`{"answer":"` + strings.Repeat("a", 5000) + `","found":true}`)
	var r struct{ Answer string }
	json.Unmarshal([]byte(long), &r)
	if !ok || len(r.Answer) != maxAgentResultChars {
		t.Errorf("a long answer is bounded to %d chars, got %d", maxAgentResultChars, len(r.Answer))
	}
}

// fakeAgentOllama plays the model for agents: a format request gets the
// result JSON (or junk, for a task containing "junk"), a tools request
// gets script's answer, and every request is counted and recorded.
type fakeAgentOllama struct {
	mu        sync.Mutex
	inFlight  int32
	peak      int32
	delay     time.Duration
	tools     bool
	withTools int
	script    func(task string, n int) string // command to propose, "" to answer in text
	calls     map[string]int
}

func (f *fakeAgentOllama) serve(t *testing.T) *httptest.Server {
	f.calls = map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			caps := []string{"completion"}
			if f.tools {
				caps = append(caps, "tools")
			}
			json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
			return
		}
		n := atomic.AddInt32(&f.inFlight, 1)
		defer atomic.AddInt32(&f.inFlight, -1)
		for {
			p := atomic.LoadInt32(&f.peak)
			if n <= p || atomic.CompareAndSwapInt32(&f.peak, p, n) {
				break
			}
		}
		var req struct {
			Messages []OllamaMessage `json:"messages"`
			Tools    []any           `json:"tools"`
			Format   json.RawMessage `json:"format"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		task := strings.SplitN(req.Messages[1].Content, "\n", 2)[0] // the "Subtask: ..." line, before any nudge
		select {
		case <-time.After(f.delay):
		case <-r.Context().Done():
			return
		}
		if req.Format != nil {
			answer := `{"answer":"answer to ` + task + `","found":true}`
			if strings.Contains(task, "junk") {
				answer = "not json at all"
			}
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": answer}, "done": true})
			return
		}
		f.mu.Lock()
		if req.Tools != nil {
			f.withTools++
		}
		f.calls[task]++
		n2 := f.calls[task]
		f.mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "done looking"}
		if cmd := f.script(task, n2); req.Tools != nil && cmd != "" {
			msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"function": map[string]any{"name": "run_shell", "arguments": map[string]string{"command": cmd}}}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"message": msg, "done": true, "prompt_eval_count": 100, "eval_count": 10})
	}))
	t.Cleanup(srv.Close)
	return srv
}

type agentFixture struct {
	s     *Server
	user  *User
	convo *ConversationWithMessages
}

func newAgentFixture(t *testing.T, f *fakeAgentOllama, tasks ...string) (*agentFixture, *agentRunStream, *strings.Builder) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("port 7070\n"), 0o644)
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := f.serve(t)
	s := &Server{db: db, ollama: NewOllamaClient(), defaultOllamaURL: srv.URL}
	s.ollama.models.Store(srv.URL+" m", OllamaModelInfo{Name: "m", Digest: "d"})
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", "h")
	}
	createConversation(db, "c1", "alice", "m")
	setAttachedFolder(db, "c1", dir)
	run := AgentRun{ID: "run1", ConversationID: "c1", Task: "t", Status: "running"}
	for i, task := range tasks {
		run.Agents = append(run.Agents, Agent{ID: "a" + string(rune('0'+i)), Position: i, Task: task, Files: []string{"README.md"}, Status: "queued"})
	}
	if err := createAgentRun(db, run); err != nil {
		t.Fatal(err)
	}
	user, _ := getUserByID(db, "alice")
	convo, _ := getConversation(db, "c1", "alice")
	out := &strings.Builder{}
	var mu sync.Mutex
	st := &agentRunStream{w: lockedWriter{out, &mu}, run: &run, status: "running", flush: func() {}}
	return &agentFixture{s: s, user: user, convo: convo}, st, out
}

// exploring gives every agent of the run no files, so on a tool-capable
// model it looks through the folder with commands.
func exploring(st *agentRunStream) {
	for i := range st.run.Agents {
		st.run.Agents[i].Files = []string{}
	}
}

type lockedWriter struct {
	b  *strings.Builder
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// decide answers every pending agent command through the HTTP handler, as
// user would, until ctx ends.
func decide(ctx context.Context, fx *agentFixture, user string, pick func(command string) (action, reply string)) {
	u, _ := getUserByID(fx.s.db, user)
	for ctx.Err() == nil {
		rows, _ := fx.s.db.Query(`SELECT id, command FROM commands WHERE status = 'pending' AND agent_id IS NOT NULL`)
		var pending [][2]string
		for rows.Next() {
			var id, cmd string
			rows.Scan(&id, &cmd)
			pending = append(pending, [2]string{id, cmd})
		}
		rows.Close()
		for _, p := range pending {
			action, reply := pick(p[1])
			body, _ := json.Marshal(map[string]string{"reply": reply})
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
			r.SetPathValue("id", "run1")
			r.SetPathValue("cmdId", p[0])
			r.SetPathValue("action", action)
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
			fx.s.handleDecideAgentCommand(httptest.NewRecorder(), r)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func agentsOf(t *testing.T, fx *agentFixture) []Agent {
	run, err := getAgentRun(fx.s.db, "run1", "alice")
	if err != nil || run == nil {
		t.Fatal(err)
	}
	return run.Agents
}

func TestAgentsRespectTheLimitAndReportEveryResult(t *testing.T) {
	f := &fakeAgentOllama{delay: 30 * time.Millisecond, script: func(string, int) string { return "" }}
	fx, st, out := newAgentFixture(t, f, "one", "two", "junk three", "four", "five")
	fx.s.runAgents(context.Background(), st, fx.user, fx.convo, 2)

	if f.peak > 2 {
		t.Errorf("at most 2 agents may call the model at once, saw %d", f.peak)
	}
	if f.withTools != 0 {
		t.Error("a model without tool support must never be offered the shell")
	}
	agents := agentsOf(t, fx)
	for _, a := range agents {
		want := "done"
		if strings.Contains(a.Task, "junk") {
			want = "failed"
		}
		if a.Status != want {
			t.Errorf("%s: want %s, got %s (%s)", a.Task, want, a.Status, a.Error)
		}
	}
	if agents[2].Error == "" || agents[2].Result != "" {
		t.Errorf("a malformed result is shown as failed with a reason, got %+v", agents[2])
	}
	if !strings.Contains(agents[0].Result, `"found":true`) {
		t.Errorf("a done agent keeps its bounded JSON result, got %q", agents[0].Result)
	}
	msgs, _, _ := getAgentMessages(fx.s.db, "run1", "a0", "alice")
	if len(msgs) != 3 || !strings.Contains(msgs[1].Content, "port 7070") || msgs[2].Role != "result" {
		t.Errorf("the transcript holds the prompt with the file read in Go, then the result, got %+v", msgs)
	}
	if !strings.Contains(out.String(), `<<<AGENTS>>>{"agents":`) {
		t.Errorf("progress should stream as <<<AGENTS>>> lines, got %q", out.String()[:min(200, len(out.String()))])
	}
}

func TestStopCancelsRunningQueuedAndWaitingAgents(t *testing.T) {
	f := &fakeAgentOllama{tools: true, script: func(task string, n int) string {
		if strings.Contains(task, "cmd") {
			return "ls"
		}
		return ""
	}, delay: 200 * time.Millisecond}
	fx, st, _ := newAgentFixture(t, f, "cmd one", "slow two", "three")
	exploring(st)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			var n int
			fx.s.db.QueryRow(`SELECT count(*) FROM commands WHERE status = 'pending'`).Scan(&n)
			if n > 0 {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	done := make(chan struct{})
	go func() { fx.s.runAgents(ctx, st, fx.user, fx.convo, 2); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop didn't end the run")
	}
	for _, a := range agentsOf(t, fx) {
		if a.Status != "cancelled" {
			t.Errorf("%s: want cancelled after Stop, got %s", a.Task, a.Status)
		}
	}
	var status string
	fx.s.db.QueryRow(`SELECT status FROM commands`).Scan(&status)
	if status != "cancelled" {
		t.Errorf("the waiting command must be cancelled, never left to run, got %s", status)
	}
}

func TestAgentCommandsNeedApprovalAndStopAtTheLimits(t *testing.T) {
	f := &fakeAgentOllama{tools: true, script: func(task string, n int) string {
		switch {
		case strings.Contains(task, "fails"):
			return "false"
		case strings.Contains(task, "replied"):
			return "false # replied"
		}
		return "cat README.md"
	}}
	fx, st, out := newAgentFixture(t, f, "keeps reading", "fails", "replied")
	exploring(st)
	fx.s.db.Exec(`UPDATE users SET agent_commands = 7 WHERE id = 'alice'`)
	fx.user, _ = getUserByID(fx.s.db, "alice")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	denied := 0
	go decide(ctx, fx, "alice", func(command string) (string, string) {
		if command == "false # replied" {
			denied++
			if denied%3 == 0 {
				return "deny", "look in README.md instead"
			}
		}
		return "approve", ""
	})
	fx.s.runAgents(context.Background(), st, fx.user, fx.convo, 3)
	cancel()

	count := func(agent string) (n int) {
		fx.s.db.QueryRow(`SELECT count(*) FROM commands WHERE agent_id = ?`, agent).Scan(&n)
		return n
	}
	if n := count("a0"); n != 7 {
		t.Errorf("a successful agent stops at the commands-per-agent setting (7), got %d", n)
	}
	if n := count("a1"); n != agentFailureLimit {
		t.Errorf("an agent stops after %d failures in a row, got %d commands", agentFailureLimit, n)
	}
	if n := count("a2"); n <= agentFailureLimit {
		t.Errorf("a reply instead of a denial resets the failure count, so more than %d commands, got %d", agentFailureLimit, n)
	}
	for _, a := range agentsOf(t, fx) {
		if a.Status != "done" {
			t.Errorf("%s: should still answer after its commands stop, got %s %s", a.Task, a.Status, a.Error)
		}
	}
	var output string
	fx.s.db.QueryRow(`SELECT output FROM commands WHERE agent_id = 'a0' LIMIT 1`).Scan(&output)
	if !strings.Contains(output, "port 7070") {
		t.Errorf("an approved command runs in the attached folder, got %q", output)
	}
	msgs, _, _ := getAgentMessages(fx.s.db, "run1", "a2", "alice")
	var sawReply bool
	for _, m := range msgs {
		sawReply = sawReply || (m.Role == "tool" && strings.Contains(m.Content, "look in README.md instead"))
	}
	if !sawReply {
		t.Error("the user's reply must reach the agent")
	}
	if !strings.Contains(out.String(), `"type":"command"`) || !strings.Contains(out.String(), `"agent":1`) {
		t.Error("each proposed command should stream with the agent's number")
	}
	var mainPending *Command
	mainPending, _ = getPendingCommand(fx.s.db, "c1")
	if mainPending != nil {
		t.Error("agent commands never count as the chat's own pending command")
	}
}

func TestAgentCommandsCantBeDecidedByOthersOrTheChatRoute(t *testing.T) {
	f := &fakeAgentOllama{tools: true, script: func(string, int) string { return "ls" }}
	fx, st, _ := newAgentFixture(t, f, "cmd")
	exploring(st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { fx.s.runAgents(ctx, st, fx.user, fx.convo, 1); close(done) }()

	var cmdID string
	for cmdID == "" {
		fx.s.db.QueryRow(`SELECT id FROM commands WHERE status = 'pending'`).Scan(&cmdID)
		time.Sleep(5 * time.Millisecond)
	}
	call := func(user string, h http.HandlerFunc, runID string) int {
		u, _ := getUserByID(fx.s.db, user)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		r.SetPathValue("id", runID)
		r.SetPathValue("cmdId", cmdID)
		r.SetPathValue("action", "approve")
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	if code := call("bob", fx.s.handleDecideAgentCommand, "run1"); code != 404 {
		t.Errorf("another account approving: want 404, got %d", code)
	}
	if code := call("alice", fx.s.handleDecideAgentCommand, "other-run"); code != 404 {
		t.Errorf("approving through the wrong run: want 404, got %d", code)
	}
	if code := call("alice", fx.s.handleApproveCommand, "c1"); code != 404 {
		t.Errorf("the chat's own approve route must not run an agent command: want 404, got %d", code)
	}
	var status string
	fx.s.db.QueryRow(`SELECT status FROM commands WHERE id = ?`, cmdID).Scan(&status)
	if status != "pending" {
		t.Errorf("nothing should have decided the command, got %s", status)
	}
	cancel()
	<-done
}

func TestAgentTimeLimitSkipsApprovalWaits(t *testing.T) {
	old := agentTimeLimit
	agentTimeLimit = 150 * time.Millisecond
	defer func() { agentTimeLimit = old }()

	f := &fakeAgentOllama{tools: true, delay: 40 * time.Millisecond, script: func(task string, n int) string {
		if n == 1 {
			return "true"
		}
		return ""
	}}
	fx, st, _ := newAgentFixture(t, f, "waits for approval")
	exploring(st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go decide(ctx, fx, "alice", func(string) (string, string) {
		time.Sleep(300 * time.Millisecond) // longer than the whole limit
		return "approve", ""
	})
	fx.s.runAgents(context.Background(), st, fx.user, fx.convo, 1)
	if a := agentsOf(t, fx)[0]; a.Status != "done" {
		t.Errorf("time spent waiting for approval must not count, got %s %s", a.Status, a.Error)
	}

	f2 := &fakeAgentOllama{delay: 400 * time.Millisecond, script: func(string, int) string { return "" }}
	fx2, st2, _ := newAgentFixture(t, f2, "slow model")
	fx2.s.runAgents(context.Background(), st2, fx2.user, fx2.convo, 1)
	if a := agentsOf(t, fx2)[0]; a.Status != "failed" || !strings.Contains(a.Error, "limit") {
		t.Errorf("a model slower than the limit fails with a reason, got %s %q", a.Status, a.Error)
	}
}

func TestAnAgentThatReadNothingHasNoAnswer(t *testing.T) {
	f := &fakeAgentOllama{tools: true, script: func(task string, n int) string {
		if strings.Contains(task, "looks") && n == 1 {
			return "cat README.md"
		}
		return ""
	}}
	fx, st, _ := newAgentFixture(t, f, "guesses", "looks first")
	exploring(st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go decide(ctx, fx, "alice", func(string) (string, string) { return "approve", "" })
	fx.s.runAgents(context.Background(), st, fx.user, fx.convo, 2)

	agents := agentsOf(t, fx)
	if agents[0].Result != noEvidenceResult {
		t.Errorf("an agent with no files that ran nothing can't have an answer, got %q", agents[0].Result)
	}
	if !strings.Contains(agents[1].Result, `"found":true`) {
		t.Errorf("an agent whose command ran keeps its answer, got %q", agents[1].Result)
	}
}

func TestFormatAgentResultsSaysWhatEachAgentFound(t *testing.T) {
	got := formatAgentResults(&AgentRun{Agents: []Agent{
		{Position: 0, Task: "port", Status: "done", Result: `{"answer":"7070","found":true}`},
		{Position: 1, Task: "owner", Status: "done", Result: `{"answer":"no owner line","found":false}`},
		{Position: 2, Task: "log", Status: "failed", Error: "ran out of its 5-minute limit"},
		{Position: 3, Task: "on call", Status: "cancelled"},
	}})
	for _, want := range []string{agentResultsPrefix, "Agent 1 (port): 7070", "Agent 2 (owner): not found in its inputs. no owner line", "Agent 3 (log): failed: ran out", "Agent 4 (on call): stopped before it finished."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func runViaHandler(t *testing.T, fx *agentFixture, tasks ...string) string {
	fx.s.db.Exec(`UPDATE agent_runs SET status = 'planned' WHERE id = 'run1'`)
	var agents []map[string]any
	for _, task := range tasks {
		agents = append(agents, map[string]any{"task": task, "files": []string{"README.md"}, "web_query": ""})
	}
	body, _ := json.Marshal(map[string]any{"agents": agents})
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	r.SetPathValue("id", "run1")
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, fx.user))
	w := httptest.NewRecorder()
	fx.s.handleRunAgentRun(w, r)
	return w.Body.String()
}

func TestCombineSavesTaskResultsAndAnswerOnly(t *testing.T) {
	f := &fakeAgentOllama{script: func(string, int) string { return "" }}
	fx, _, _ := newAgentFixture(t, f, "one", "two")
	out := runViaHandler(t, fx, "port", "owner")

	c, _ := getConversation(fx.s.db, "c1", "alice")
	var roles []string
	for _, m := range c.Messages {
		roles = append(roles, m.Role)
		if strings.Contains(m.Content, "helper agent") {
			t.Errorf("an agent's own prompt leaked into the chat history: %q", m.Content)
		}
	}
	if !slices.Equal(roles, []string{"user", "system", "assistant"}) {
		t.Fatalf("want the task, the agents' results and the answer, got %v", roles)
	}
	if c.Messages[0].Content != "t" || !strings.HasPrefix(c.Messages[1].Content, agentResultsPrefix) || c.Messages[2].Content != "done looking" {
		t.Errorf("unexpected history: %+v", c.Messages)
	}
	run, _ := getAgentRun(fx.s.db, "run1", "alice")
	if run.Status != "done" || run.Answer != "done looking" || run.MessageID == nil || *run.MessageID != c.Messages[0].ID {
		t.Errorf("the run should be done, keep its answer and point at its task message, got %+v", run)
	}
	if !strings.Contains(out, `"type":"results"`) || !strings.Contains(out, "done looking") || !strings.Contains(out, `"status":"done"`) {
		t.Errorf("the stream should carry the results line, the answer and the final state, got %q", out)
	}

	timeline := buildTimeline(c.Messages, nil, nil, []AgentRun{*run})
	var kinds []string
	for _, it := range timeline {
		kinds = append(kinds, it.Kind)
	}
	if !slices.Equal(kinds, []string{"user", "agentPlan", "agentResults", "assistant"}) {
		t.Errorf("after a reload the task shows once, as the run, then the results and the answer; got %v", kinds)
	}
}

func TestNoResultsMeansNothingToCombine(t *testing.T) {
	f := &fakeAgentOllama{script: func(string, int) string { return "" }}
	fx, _, _ := newAgentFixture(t, f, "junk")
	out := runViaHandler(t, fx, "junk one", "junk two")

	c, _ := getConversation(fx.s.db, "c1", "alice")
	if len(c.Messages) != 0 {
		t.Errorf("with no agent result nothing enters the history, got %+v", c.Messages)
	}
	run, _ := getAgentRun(fx.s.db, "run1", "alice")
	if run.Status != "failed" || !strings.Contains(out, "nothing to combine") {
		t.Errorf("want a failed run and a notice, got %s %q", run.Status, out)
	}
}

func TestAnAgentWithFilesWorksFromThemOnly(t *testing.T) {
	f := &fakeAgentOllama{tools: true, script: func(string, int) string { return "ls" }}
	fx, st, _ := newAgentFixture(t, f, "has README")
	fx.s.runAgents(context.Background(), st, fx.user, fx.convo, 1)
	if f.withTools != 0 {
		t.Errorf("an agent given its file must not be offered the shell, saw %d tool requests", f.withTools)
	}
	if a := agentsOf(t, fx)[0]; a.Status != "done" || !strings.Contains(a.Result, `"found":true`) {
		t.Errorf("it answers from its file, got %s %q", a.Status, a.Result)
	}
}
