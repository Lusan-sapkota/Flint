package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// E24: 2 parallel requests are nearly free on a 6 GB GPU; 4 pushed phi3 onto the CPU.
	defaultMaxAgents = 2
	cloudMaxAgents   = 10
	maxMaxAgents     = 64
	// Each command needs user approval; this guards the user's attention, so it isn't split local/cloud.
	defaultAgentCommands = 8

	// The prompt asks for 2 to 4 (E24); a model that lists more is cut here.
	maxPlanSubtasks  = 8
	maxAgentTask     = 500
	maxAgentWebQuery = 200

	agentPromptTokens = 512
	// Three ranked results with title, URL and snippet (formatSearchResults).
	webReserveChars    = 3000
	agentCharsPerToken = 1.5
)

// E24: Go, not the model, decides whether to split. Searches are added only by the user
// (qwen2.5 searched private facts); web_query stays "" to keep the measured shape.
const agentPlanPrompt = `You plan work for helper agents. Each agent sees only the inputs you give it, never the other agents or the chat.
If the task has 2 to 4 parts that can each be answered from different inputs, list one subtask per part. If it can't be split that way, return an empty subtasks list.
Each subtask has: task, an instruction for the agent (what to find out, never the answer); files, the exact names it needs from the list below, or [] if none; web_query, always "". %s

Example: "Who wrote a.txt and what does b.py import?" -> {"subtasks":[{"task":"Find who wrote a.txt","files":["a.txt"],"web_query":""},{"task":"List what b.py imports","files":["b.py"],"web_query":""}]}

Files in the attached folder:
%s`

const planExplore = `Agents can also look through the folder themselves, so files is optional: list the files you know a subtask needs, or [] to let its agent find them.`

var agentPlanSchema = json.RawMessage(`{"type":"object","properties":{"subtasks":{"type":"array","items":{"type":"object","properties":{"task":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"web_query":{"type":"string"}},"required":["task","files","web_query"]}}},"required":["subtasks"]}`)

type planSubtask struct {
	Task     string   `json:"task"`
	Files    []string `json:"files"`
	WebQuery string   `json:"web_query"`
}

func parseAgentCommand(content string) (task string, ok bool) {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < len("@agent") || !strings.EqualFold(trimmed[:len("@agent")], "@agent") {
		return "", false
	}
	rest := trimmed[len("@agent"):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\n' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// Only names from this list are ever read, so a planned "../x" or "/etc/passwd" can't be.
func agentFiles(folder string) (map[string]int64, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(folder, name)
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(full)
		if err != nil {
			continue
		}
		head := make([]byte, 8000)
		n, _ := io.ReadFull(f, head)
		f.Close()
		if bytes.IndexByte(head[:n], 0) != -1 {
			continue
		}
		out[name] = info.Size()
	}
	return out, nil
}

// qwen2.5:1.5b named files only in the task text. Model plans only: on an edited plan it
// would put back a file the user removed.
func addNamedFiles(subtasks []planSubtask, files map[string]int64) {
	for i := range subtasks {
		st := &subtasks[i]
		var named []string
		for f := range files {
			if namesFile(st.Task, f) && !slices.Contains(st.Files, f) {
				named = append(named, f)
			}
		}
		sort.Strings(named)
		st.Files = append(st.Files, named...)
	}
}

func validatePlan(subtasks []planSubtask, files map[string]int64, webOK, explore bool) []Agent {
	var out []Agent
	for _, st := range subtasks {
		task := truncateRunes(strings.TrimSpace(st.Task), maxAgentTask)
		var kept []string
		for _, f := range st.Files {
			f = strings.TrimSpace(f)
			if _, ok := files[f]; ok && !slices.Contains(kept, f) {
				kept = append(kept, f)
			}
		}
		// Files or a web query, never both: otherwise private details go to Brave for nothing.
		query := ""
		if webOK && len(kept) == 0 {
			query = truncateRunes(strings.TrimSpace(st.WebQuery), maxAgentWebQuery)
		}
		if task == "" || (len(kept) == 0 && query == "" && !explore) {
			continue
		}
		if kept == nil {
			kept = []string{}
		}
		out = append(out, Agent{Task: task, Files: kept, WebQuery: query})
		if len(out) == maxPlanSubtasks {
			break
		}
	}
	return out
}

// Whole-word match, so "main.py" isn't found inside "domain.py".
func namesFile(text, name string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if (start == 0 || !isNameByte(text[start-1])) && (end == len(text) || !isNameByte(text[end])) {
			return true
		}
		i = start + 1
	}
}

func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Exploring agents propose shell commands, each approved by the user in the main chat.
func (s *Server) canExplore(ctx context.Context, user *User, c Conversation) bool {
	if folderOf(c) == nil {
		return false
	}
	info := s.ollama.modelInfo(s.ollamaURLFor(user), c.Model)
	return slices.Contains(s.ollama.Capabilities(ctx, s.ollamaURLFor(user), info), "tools")
}

// Files are sized as dense text, not by the chat's calibration: 2 chars/token (E16) overflowed 8192.
func agentInputChars(numCtx int) int {
	return max(0, int(float64(numCtx-responseReserve-agentPromptTokens)*agentCharsPerToken))
}

func allocateInputs(sizes []int64, budget int) []int {
	order := make([]int, len(sizes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return sizes[order[a]] < sizes[order[b]] })
	out := make([]int, len(sizes))
	left := budget
	for k, i := range order {
		share := left / (len(order) - k)
		out[i] = int(min(sizes[i], int64(share)))
		left -= out[i]
	}
	return out
}

func planNotes(agents []Agent, files map[string]int64, budget int) {
	for i := range agents {
		a := &agents[i]
		b := budget
		if a.WebQuery != "" {
			b -= webReserveChars
		}
		sizes := make([]int64, len(a.Files))
		for j, f := range a.Files {
			sizes[j] = files[f]
		}
		var cut []string
		for j, n := range allocateInputs(sizes, max(0, b)) {
			if int64(n) < sizes[j] {
				cut = append(cut, fmt.Sprintf("%s (%s of %s, start and end kept)", a.Files[j], formatKB(n), formatKB(int(sizes[j]))))
			}
		}
		a.Note = ""
		if len(cut) > 0 {
			a.Note = "Cut to fit the window: " + strings.Join(cut, ", ")
		}
	}
}

func formatKB(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}

// A nil run means the task doesn't split and is answered as a normal chat turn.
func (s *Server) planAgentRun(ctx context.Context, user *User, convo *ConversationWithMessages, task string) (*AgentRun, error) {
	files, err := agentFiles(*convo.AttachedFolder)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var list strings.Builder
	for _, name := range names {
		fmt.Fprintf(&list, "- %s (%d bytes)\n", name, files[name])
	}
	if len(names) == 0 {
		list.WriteString("(the folder has no readable files)\n")
	}
	explore := s.canExplore(ctx, user, convo.Conversation)
	extra := ""
	if explore {
		extra = planExplore
	}

	numCtx := s.numCtxFor(user, convo.Conversation)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := s.ollama.ChatJSON(ctx, s.ollamaURLFor(user), convo.Model, []OllamaMessage{
		{Role: "system", Content: fmt.Sprintf(agentPlanPrompt, extra, list.String())},
		{Role: "user", Content: task},
	}, map[string]any{"num_ctx": numCtx, "temperature": 0}, agentPlanSchema)
	if err != nil {
		return nil, err
	}
	var plan struct {
		Subtasks []planSubtask `json:"subtasks"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		return nil, fmt.Errorf("the model's plan wasn't valid JSON: %w", err)
	}
	addNamedFiles(plan.Subtasks, files)
	agents := validatePlan(plan.Subtasks, files, false, explore)
	if len(agents) < 2 {
		return nil, nil
	}
	planNotes(agents, files, agentInputChars(numCtx))

	now := time.Now().UnixMilli()
	webOK := user.BraveAPIKey != nil && *user.BraveAPIKey != ""
	run := AgentRun{ID: uuid.NewString(), ConversationID: convo.ID, Task: task, Status: "planned", CreatedAt: now, UpdatedAt: now, FolderFiles: names, WebAvailable: webOK, CanExplore: explore, MaxCommands: agentCommandsFor(user)}
	for i := range agents {
		agents[i].ID = uuid.NewString()
		agents[i].Position = i
		agents[i].Status = "queued"
	}
	run.Agents = agents
	if err := createAgentRun(s.db, run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Server) maxAgentsFor(user *User, c Conversation) int {
	if s.ollama.modelInfo(s.ollamaURLFor(user), c.Model).RemoteHost != "" {
		if user.CloudMaxAgents != nil {
			return *user.CloudMaxAgents
		}
		return cloudMaxAgents
	}
	if user.MaxAgents != nil {
		return *user.MaxAgents
	}
	return defaultMaxAgents
}

func agentCommandsFor(user *User) int {
	if user.AgentCommands != nil {
		return *user.AgentCommands
	}
	return defaultAgentCommands
}

func (s *Server) handleGetAgentRun(w http.ResponseWriter, r *http.Request) {
	run, err := getAgentRun(s.db, r.PathValue("id"), userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// Re-reads the run inside the lock so a racing Run/Discard/double-click sees the other's result.
// Returns nil, having already answered the request, unless the run is still planned.
func (s *Server) lockPlannedRun(w http.ResponseWriter, r *http.Request) (*AgentRun, func()) {
	user := userFromContext(r)
	run, err := getAgentRun(s.db, r.PathValue("id"), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, nil
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return nil, nil
	}
	unlock := s.lockConversation(run.ConversationID)
	run, err = getAgentRun(s.db, run.ID, user.ID)
	if err != nil || run == nil || run.Status != "planned" {
		unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		} else {
			writeError(w, http.StatusConflict, "this plan was already run or discarded")
		}
		return nil, nil
	}
	return run, unlock
}

func (s *Server) handleDiscardAgentRun(w http.ResponseWriter, r *http.Request) {
	run, unlock := s.lockPlannedRun(w, r)
	if run == nil {
		return
	}
	defer unlock()
	if err := setAgentRunStatus(s.db, run.ID, "discarded"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Card edits are untrusted like the model's plan, so they get the same validation.
func (s *Server) handleRunAgentRun(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	var body struct {
		Agents []planSubtask `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, unlock := s.lockPlannedRun(w, r)
	if run == nil {
		return
	}
	defer unlock()

	convo, err := getConversation(s.db, run.ConversationID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pending, err := getPendingCommand(s.db, convo.ID); err != nil || pending != nil {
		writeError(w, http.StatusConflict, "approve or deny the pending command before running agents")
		return
	}
	files := map[string]int64{}
	if folder := folderOf(convo.Conversation); folder != nil {
		if files, err = agentFiles(*folder); err != nil {
			writeError(w, http.StatusBadRequest, "the attached folder can't be read: "+err.Error())
			return
		}
	}
	agents := validatePlan(body.Agents, files, user.BraveAPIKey != nil && *user.BraveAPIKey != "", s.canExplore(r.Context(), user, convo.Conversation))
	if len(agents) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to run: each subtask needs a task, and this model needs a file from the folder or a web query for it")
		return
	}
	planNotes(agents, files, agentInputChars(s.numCtxFor(user, convo.Conversation)))
	for i := range agents {
		agents[i].ID = uuid.NewString()
		agents[i].Position = i
		agents[i].Status = "queued"
	}
	if err := replaceAgents(s.db, run.ID, agents); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := setAgentRunStatus(s.db, run.ID, "running"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	run.Agents = agents

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	st := &agentRunStream{w: w, run: run, status: "running", flush: func() {
		if canFlush {
			flusher.Flush()
		}
	}}
	st.setStatus("running")
	s.runAgents(r.Context(), st, user, convo, s.maxAgentsFor(user, convo.Conversation))

	if r.Context().Err() != nil {
		if err := setAgentRunStatus(s.db, run.ID, "cancelled"); err != nil {
			log.Printf("warning: saving agent run status: %v", err)
		}
		st.setStatus("cancelled")
		return
	}
	s.combineAgents(w, r, st, user, convo)
}

func (s *Server) handleGetAgentTranscript(w http.ResponseWriter, r *http.Request) {
	msgs, found, err := getAgentMessages(s.db, r.PathValue("id"), r.PathValue("agentId"), userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}
