package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// E24: on a 6 GB GPU two parallel requests cost almost nothing on the
	// GQA models, while four already pushed phi3 onto the CPU. A stock
	// Ollama queues same-model requests anyway, so 2 is free there too.
	defaultMaxAgents = 2
	cloudMaxAgents   = 10
	maxMaxAgents     = 64

	// The prompt asks for 2 to 4 (E24); a model that lists more is cut here.
	maxPlanSubtasks  = 8
	maxAgentTask     = 500
	maxAgentWebQuery = 200
	// Room in an agent's window for its instructions and task, besides the
	// inputs and the reply.
	agentPromptTokens = 512
	// Three ranked results with title, URL and snippet (formatSearchResults).
	webReserveChars = 3000
	// Tool rounds an exploring agent gets before it must answer with what
	// it has read (maxToolAttemptsPerTurn's bounded-retry idea again).
	maxAgentToolRounds = 6
	agentCharsPerToken = 2
)

// Measured in E24 (second version): "never the answer" stopped llama3.2
// writing guessed summaries as tasks, and the web example made qwen2.5
// use web_query at all. Whether to split is not asked: the model's own
// flag contradicted its subtasks, so Go decides from what survives
// validation.
const agentPlanPrompt = `You plan work for helper agents. Each agent sees only the inputs you give it, never the other agents or the chat.
If the task has 2 to 4 parts that can each be answered from different inputs, list one subtask per part. If it can't be split that way, return an empty subtasks list.
Each subtask has: task, an instruction for the agent (what to find out, never the answer); files, the exact names it needs from the list below, or [] if none; web_query, a search query, or "" if none. %s

Example: "Who wrote a.txt and what does b.py import?" -> {"subtasks":[{"task":"Find who wrote a.txt","files":["a.txt"],"web_query":""},{"task":"List what b.py imports","files":["b.py"],"web_query":""}]}
%s
Files in the attached folder:
%s`

const planExplore = `Agents can also look through the folder themselves, so files is optional: list the files you know a subtask needs, or [] to let its agent find them.`

const (
	planWebOn      = `Web search is available: a subtask that needs current facts from the internet gets one web_query and no files.`
	planWebOff     = `Web search is NOT available: web_query is always "".`
	planWebExample = `Example: "Compare the newest Python and Node versions" -> {"subtasks":[{"task":"Find the newest Python version","files":[],"web_query":"latest Python release"},{"task":"Find the newest Node.js version","files":[],"web_query":"latest Node.js release"}]}
`
)

var agentPlanSchema = json.RawMessage(`{"type":"object","properties":{"subtasks":{"type":"array","items":{"type":"object","properties":{"task":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"web_query":{"type":"string"}},"required":["task","files","web_query"]}}},"required":["subtasks"]}`)

type planSubtask struct {
	Task     string   `json:"task"`
	Files    []string `json:"files"`
	WebQuery string   `json:"web_query"`
}

// parseAgentCommand recognizes "@agent <task>".
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

// agentFiles lists what an agent may be given: the attached folder's
// direct, non-hidden, non-binary files, by name, with their sizes. Unlike
// the manifest there's no size budget here, since an agent's input is cut
// to fit its own window instead. Only names from this list are ever read,
// so a planned "../x" or "/etc/passwd" is simply not in it.
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

// validatePlan applies E24's rules: files must be in the folder's list,
// and a web query needs a Brave key and a subtask without files. A subtask
// left with no input at all is kept only when agents can explore the
// folder themselves; otherwise it's dropped, since that agent could only
// reason about what it's given.
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
		// qwen2.5:1.5b split correctly but named the file only in the task
		// ("Find TAX_RATE in utils.py") and left files empty.
		planned := len(kept)
		for f := range files {
			if namesFile(task, f) && !slices.Contains(kept, f) {
				kept = append(kept, f)
			}
		}
		sort.Strings(kept[planned:])
		// Files or one web query, never both: qwen2.5:1.5b added searches
		// like "Stockroom server port" next to the file that answers it,
		// which only sends a private detail to Brave for nothing.
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

// namesFile reports whether text mentions name as a whole word, so
// "main.py" isn't found inside "domain.py".
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

// canExplore reports whether this chat's agents may look through the
// attached folder themselves with the read-only tools: that needs a folder
// and a model that supports tool calls. Others get their inputs only.
func (s *Server) canExplore(ctx context.Context, user *User, c Conversation) bool {
	if folderOf(c) == nil {
		return false
	}
	info := s.ollama.modelInfo(s.ollamaURLFor(user), c.Model)
	return slices.Contains(s.ollama.Capabilities(ctx, s.ollamaURLFor(user), info), "tools")
}

// agentInputChars is how much input text fits one agent's window, in
// characters, after its reply and instructions. Inputs are files, not the
// chat's own text, so the chat's calibration doesn't apply to them; they
// are sized as dense text (~2 chars per token, E16), which the fixture's
// log really is (24.7 KB, ~11k tokens). Prose gets less than would fit.
func agentInputChars(numCtx int) int {
	return max(0, (numCtx-responseReserve-agentPromptTokens)*agentCharsPerToken)
}

// allocateInputs splits budget characters between inputs of the given
// sizes: smaller ones get all they need, and what they leave goes to the
// bigger ones in equal shares.
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

// planNotes says, per agent, which of its files will be cut to fit.
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

// planAgentRun asks the model for a plan and saves it as a run awaiting
// approval. A nil run means the task doesn't split and should be answered
// as a normal chat turn.
func (s *Server) planAgentRun(ctx context.Context, user *User, convo *ConversationWithMessages, task string) (*AgentRun, error) {
	files := map[string]int64{}
	if folder := folderOf(convo.Conversation); folder != nil {
		var err error
		if files, err = agentFiles(*folder); err != nil {
			return nil, err
		}
	}
	webOK := user.BraveAPIKey != nil && *user.BraveAPIKey != ""

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
		list.WriteString("(no folder attached)\n")
	}
	web, example := planWebOff, ""
	if webOK {
		web, example = planWebOn, planWebExample
	}
	explore := s.canExplore(ctx, user, convo.Conversation)
	if explore {
		web += " " + planExplore
	}

	numCtx := s.numCtxFor(user, convo.Conversation)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := s.ollama.ChatJSON(ctx, s.ollamaURLFor(user), convo.Model, []OllamaMessage{
		{Role: "system", Content: fmt.Sprintf(agentPlanPrompt, web, example, list.String())},
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
	agents := validatePlan(plan.Subtasks, files, webOK, explore)
	if len(agents) < 2 {
		return nil, nil
	}
	planNotes(agents, files, agentInputChars(numCtx))

	now := time.Now().UnixMilli()
	run := AgentRun{ID: uuid.NewString(), ConversationID: convo.ID, Task: task, Status: "planned", CreatedAt: now, UpdatedAt: now, FolderFiles: names, WebAvailable: webOK, CanExplore: explore}
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

// lockPlannedRun finds the caller's run, takes its conversation's lock and
// re-reads it inside, since a Run and a Discard (or a double-click) racing
// each other must see each other's result. It answers the request itself
// and returns nil unless the run is still planned.
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

// handleRunAgentRun takes the plan as edited on the card. Edits are
// untrusted input like the model's plan was, so they go through the same
// validation against the folder as it is now.
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
	writeError(w, http.StatusNotImplemented, "the plan is saved; running agents isn't built yet")
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
