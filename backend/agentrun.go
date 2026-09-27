package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Model time per agent (each command has its own 60-second cap). Waiting
// for the user to approve a command doesn't count: reading a command
// carefully must never be what makes an agent fail. A variable so tests
// can shorten it.
var agentTimeLimit = 5 * time.Minute

const (
	// Failed, blocked or denied commands in a row before the agent must
	// answer with what it has. A success or a reply from the user resets it.
	agentFailureLimit = 5
	// An agent stops being offered the shell once less than this is left
	// of its window: too little for any output worth reading.
	agentMinToolRoom     = 1024
	agentResultTokens    = 400
	maxAgentResultChars  = 1500
	maxAgentCommandChars = 20000
)

const agentSystemPrompt = `You are a helper agent. You work on one subtask of a larger task and see only what is given here, never the rest of the conversation or the other agents. Answer only your subtask, using only the inputs below%s. If they don't contain the answer, say so plainly instead of guessing.`

const agentExplorePrompt = ` and the output of commands you run. You can run shell commands in the attached folder to look at files (ls, grep, cat and similar); the user sees each one and must approve it before it runs. Use them only for what the inputs don't show. When you have what you need, answer in plain text.`

const agentResultPrompt = `Give your result for the subtask as JSON: "answer" is your answer in at most 150 words, keeping exact names, numbers and quotes; "found" is true only if what you were given or what your commands showed actually contains the answer.`

const noEvidenceResult = `{"answer":"It was given no files and ran no command that worked, so it had nothing to answer from.","found":false}`

var agentResultSchema = json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"},"found":{"type":"boolean"}},"required":["answer","found"]}`)

type agentDecision struct {
	approve bool
	reply   string
}

// agentRunStream owns a run's response stream and its in-memory state:
// agents write their progress from several goroutines, and every change
// goes out as one <<<AGENTS>>> line.
type agentRunStream struct {
	mu     sync.Mutex
	w      io.Writer
	flush  func()
	run    *AgentRun
	status string
}

func (st *agentRunStream) line(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(st.w, "<<<AGENTS>>>%s\n", b)
	st.flush()
}

func (st *agentRunStream) update(i int, change func(a *Agent)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	change(&st.run.Agents[i])
	st.line(map[string]any{"type": "state", "run_id": st.run.ID, "status": st.status, "agents": st.run.Agents})
}

func (st *agentRunStream) setStatus(status string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.status = status
	st.line(map[string]any{"type": "state", "run_id": st.run.ID, "status": st.status, "agents": st.run.Agents})
}

func (st *agentRunStream) command(i int, cmd *Command, status, output string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.line(map[string]any{"type": "command", "run_id": st.run.ID, "agent_id": st.run.Agents[i].ID, "agent": st.run.Agents[i].Position + 1,
		"id": cmd.ID, "command": cmd.Command, "status": status, "output": output})
}

// runAgents runs every agent of a run, at most limit at once and in plan
// order, until all finish or ctx (the request: Stop, a closed tab) ends.
func (s *Server) runAgents(ctx context.Context, st *agentRunStream, user *User, convo *ConversationWithMessages, limit int) {
	env := agentEnv{
		user:     user,
		convo:    convo,
		numCtx:   s.numCtxFor(user, convo.Conversation),
		explore:  s.canExplore(ctx, user, convo.Conversation),
		commands: agentCommandsFor(user),
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range st.run.Agents {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			s.runAgent(ctx, st, i, env)
		}()
	}
	wg.Wait()
	for i, a := range st.run.Agents {
		if a.Status == "queued" {
			finishAgent(s.db, a.ID, "cancelled", "", "")
			st.update(i, func(a *Agent) { a.Status = "cancelled" })
		}
	}
}

type agentEnv struct {
	user     *User
	convo    *ConversationWithMessages
	numCtx   int
	explore  bool
	commands int
}

// runAgent is one agent from start to result. Its history lives only
// here and in its transcript; nothing of it reaches the chat's history.
func (s *Server) runAgent(ctx context.Context, st *agentRunStream, i int, env agentEnv) {
	a := st.run.Agents[i]
	startAgent(s.db, a.ID)
	st.update(i, func(a *Agent) { a.Status = "running" })

	fail := func(msg string) {
		status := "failed"
		if ctx.Err() != nil {
			status, msg = "cancelled", ""
		}
		finishAgent(s.db, a.ID, status, "", msg)
		st.update(i, func(a *Agent) { a.Status, a.Error = status, msg })
	}

	inputs, err := s.agentInputs(ctx, env, a)
	if err != nil {
		fail(err.Error())
		return
	}
	extra := ""
	if env.explore {
		extra = agentExplorePrompt
		if anchor := buildAnchorHeader(*env.convo.AttachedFolder); anchor != "" {
			extra += "\n\n" + anchor
		}
	}
	messages := []OllamaMessage{
		{Role: "system", Content: fmt.Sprintf(agentSystemPrompt, extra)},
		{Role: "user", Content: "Subtask: " + a.Task + "\n\n" + inputs},
	}
	for _, m := range messages {
		insertAgentMessage(s.db, a.ID, m.Role, m.Content)
	}

	var used time.Duration
	call := func(msgs []OllamaMessage, tools []OllamaTool) (ChatResult, error) {
		cctx, cancel := context.WithTimeout(ctx, agentTimeLimit-used)
		defer cancel()
		start := time.Now()
		off := false
		res, err := s.ollama.StreamChat(cctx, s.ollamaURLFor(env.user), env.convo.Model, msgs, tools, map[string]any{"num_ctx": env.numCtx}, &off, func(string) {}, func(string) {})
		used += time.Since(start)
		return res, err
	}
	explain := func(err error) string {
		var overflow *contextOverflowError
		switch {
		case errors.As(err, &overflow):
			return "its inputs didn't fit its context window"
		case errors.Is(err, context.DeadlineExceeded):
			return fmt.Sprintf("ran out of its %d-minute limit", int(agentTimeLimit.Minutes()))
		}
		return describeOllamaError(err, s.ollamaURLFor(env.user))
	}

	// Whether the agent has read anything at all: given files or web
	// results, or a command that ran. Without that, whatever it answers is
	// made up (qwen2.5:1.5b answered "John Doe" as found), so it isn't kept.
	evidence := len(a.Files) > 0 || a.WebQuery != ""
	proposed, failures := 0, 0
	for env.explore {
		room := env.numCtx - responseReserve - estimateAgentTokens(messages)
		if proposed >= env.commands || failures >= agentFailureLimit || room < agentMinToolRoom {
			break
		}
		// The same nudge as the chat's, on the last message only and never
		// stored: without it qwen2.5 wrote "I'll look through the folder"
		// or a command in a code block instead of calling the tool.
		nudged := slices.Clone(messages)
		last := &nudged[len(nudged)-1]
		last.Content = strings.TrimRight(last.Content, "\n") + "\n\n" + toolReasoningPrompt
		res, err := call(nudged, []OllamaTool{runShellTool})
		if err != nil {
			fail(explain(err))
			return
		}
		if len(res.ToolCalls) == 0 {
			messages = append(messages, OllamaMessage{Role: "assistant", Content: res.Content})
			insertAgentMessage(s.db, a.ID, "assistant", res.Content)
			break
		}
		tc := res.ToolCalls[0]
		var args struct {
			Command string `json:"command"`
		}
		json.Unmarshal(tc.Function.Arguments, &args)
		proposed++
		messages = append(messages, OllamaMessage{Role: "assistant", Content: res.Content, ToolCalls: []OllamaToolCall{tc}})
		insertAgentMessage(s.db, a.ID, "tool_call", args.Command)

		result, ok, reset := s.agentCommand(ctx, st, i, env, args.Command)
		if ctx.Err() != nil {
			fail("")
			return
		}
		evidence = evidence || ok
		if ok || reset {
			failures = 0
		} else {
			failures++
		}
		room = env.numCtx - responseReserve - agentPromptTokens - estimateAgentTokens(messages)
		result = cutToChars(result, min(maxAgentCommandChars, int(float64(max(0, room))*agentCharsPerToken)))
		messages = append(messages, OllamaMessage{Role: "tool", Content: result, ToolCallID: tc.ID})
		insertAgentMessage(s.db, a.ID, "tool", result)
	}

	final := append(messages[:len(messages):len(messages)], OllamaMessage{Role: "user", Content: agentResultPrompt})
	cctx, cancel := context.WithTimeout(ctx, agentTimeLimit-used)
	defer cancel()
	out, err := s.ollama.ChatJSON(cctx, s.ollamaURLFor(env.user), env.convo.Model, final,
		map[string]any{"num_ctx": env.numCtx, "num_predict": agentResultTokens, "temperature": 0}, agentResultSchema)
	if err != nil {
		fail(explain(err))
		return
	}
	insertAgentMessage(s.db, a.ID, "result", out)
	result, ok := parseAgentResult(out)
	if !ok {
		fail("its result wasn't the required JSON shape")
		return
	}
	if !evidence {
		result = noEvidenceResult
	}
	finishAgent(s.db, a.ID, "done", result, "")
	st.update(i, func(a *Agent) { a.Status, a.Result = "done", result })
}

// agentCommand takes one proposed command through the same checks as the
// chat's own and waits for the user's decision in the main chat. ok means
// it ran and succeeded; reset means the user replied instead, which counts
// as guidance rather than a failure.
func (s *Server) agentCommand(ctx context.Context, st *agentRunStream, i int, env agentEnv, command string) (result string, ok, reset bool) {
	if strings.TrimSpace(command) == "" {
		return "[FAILED] The tool call had no command.", false, false
	}
	if blocked, reason := checkCommandShield(command); blocked {
		return shieldBlockedMessage(reason), false, false
	}
	folder := *env.convo.AttachedFolder
	if ok, reason := checkCommandPreconditions(command, folder); !ok && !ablated["preconditions"] {
		return fmt.Sprintf("[PRECONDITION FAILED: %s]\nThis command was not run. Check your assumptions and try a different command.", reason), false, false
	}

	cmd, err := createAgentCommand(s.db, uuid.NewString(), env.convo.ID, st.run.Agents[i].ID, command, folder)
	if err != nil {
		return "[FAILED] The command couldn't be saved: " + err.Error(), false, false
	}
	decision := make(chan agentDecision, 1)
	s.agentDecisions.Store(cmd.ID, decision)
	defer s.agentDecisions.Delete(cmd.ID)
	st.update(i, func(a *Agent) { a.Status = "waiting" })
	st.command(i, cmd, "pending", "")

	var d agentDecision
	select {
	case d = <-decision:
	case <-ctx.Done():
		cancelAgentCommand(s.db, cmd.ID)
		return "", false, false
	}
	st.update(i, func(a *Agent) { a.Status = "running" })

	switch {
	case !d.approve && d.reply != "":
		resolveCommand(s.db, cmd.ID, "denied", "", nil)
		st.command(i, cmd, "denied", "")
		return "User denied this command and replied: " + d.reply + "\nFollow their reply.", false, true
	case !d.approve:
		resolveCommand(s.db, cmd.ID, "denied", "", nil)
		st.command(i, cmd, "denied", "")
		return "User denied this command. It was their choice, and nothing is wrong with your access. Don't guess what it would have output; try another way or answer with what you have.", false, false
	}
	if blocked, reason := checkCommandShield(cmd.Command); blocked {
		text := shieldBlockedMessage(reason)
		resolveCommand(s.db, cmd.ID, "blocked", text, nil)
		st.command(i, cmd, "blocked", text)
		return text, false, false
	}
	text, status := s.executeCommand(ctx, cmd)
	st.command(i, cmd, status, text)
	return text, status == "success", false
}

// agentInputs is the text an agent is given: each planned file cut to its
// share of the window, and the web results for its query.
func (s *Server) agentInputs(ctx context.Context, env agentEnv, a Agent) (string, error) {
	budget := agentInputChars(env.numCtx)
	var b strings.Builder
	if a.WebQuery != "" {
		results, err := s.searchWeb(ctx, env.user, a.WebQuery)
		if err != nil {
			return "", fmt.Errorf("the web search failed: %v", err)
		}
		text := formatSearchResults(a.WebQuery, results)
		if len(results) == 0 {
			text = fmt.Sprintf("The web search for %q found nothing.", a.WebQuery)
		}
		b.WriteString(cutToChars(text, webReserveChars) + "\n\n")
		budget -= webReserveChars
	}
	if len(a.Files) > 0 {
		folder := *env.convo.AttachedFolder
		sizes := make([]int64, len(a.Files))
		for j, f := range a.Files {
			info, err := os.Stat(filepath.Join(folder, f))
			if err != nil {
				return "", fmt.Errorf("%s can't be read: %v", f, err)
			}
			sizes[j] = info.Size()
		}
		for j, n := range allocateInputs(sizes, max(0, budget)) {
			text, err := readCut(filepath.Join(folder, a.Files[j]), sizes[j], n)
			if err != nil {
				return "", fmt.Errorf("%s can't be read: %v", a.Files[j], err)
			}
			fmt.Fprintf(&b, "--- %s ---\n%s\n\n", a.Files[j], text)
		}
	}
	if b.Len() == 0 {
		return "No files were given: look through the folder yourself.", nil
	}
	return "Inputs:\n\n" + strings.TrimRight(b.String(), "\n"), nil
}

// readCut reads a file, or only its start and end when it's over limit
// bytes, without loading the part in between.
func readCut(path string, size int64, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if size <= int64(limit) {
		b, err := io.ReadAll(f)
		return string(b), err
	}
	half := limit / 2
	head := make([]byte, half)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", err
	}
	tail := make([]byte, half)
	if _, err := f.ReadAt(tail, size-int64(half)); err != nil && err != io.EOF {
		return "", err
	}
	return joinCut(string(head), string(tail), size-int64(2*half)), nil
}

// cutToChars keeps the start and end of text within limit characters.
func cutToChars(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	half := limit / 2
	return joinCut(text[:half], text[len(text)-half:], int64(len(text)-2*half))
}

// joinCut trims both halves to whole lines, so no line is shown half cut.
func joinCut(head, tail string, cut int64) string {
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		cut += int64(len(head) - i - 1)
		head = head[:i+1]
	}
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		cut += int64(i + 1)
		tail = tail[i+1:]
	}
	return strings.ToValidUTF8(fmt.Sprintf("%s[... %s cut to fit the window ...]\n%s", head, formatKB(int(cut)), tail), "")
}

func estimateAgentTokens(messages []OllamaMessage) int {
	n := 0
	for _, m := range messages {
		n += int(float64(len(m.Content))/agentCharsPerToken) + perMessageTokens
	}
	return n
}

// parseAgentResult checks the fixed result shape and bounds the answer.
func parseAgentResult(out string) (string, bool) {
	var r struct {
		Answer *string `json:"answer"`
		Found  *bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Answer == nil || r.Found == nil {
		return "", false
	}
	answer := strings.TrimSpace(*r.Answer)
	if answer == "" && *r.Found {
		return "", false
	}
	b, _ := json.Marshal(map[string]any{"answer": truncateRunes(answer, maxAgentResultChars), "found": *r.Found})
	return string(b), true
}

// handleDecideAgentCommand approves or denies a command an agent proposed.
// It never takes the conversation's lock: the run holds that for as long
// as its agents work, and this is how the user answers them meanwhile.
func (s *Server) handleDecideAgentCommand(w http.ResponseWriter, r *http.Request) {
	approve := r.PathValue("action") == "approve"
	if !approve && r.PathValue("action") != "deny" {
		writeError(w, http.StatusNotFound, "unknown action")
		return
	}
	var body struct {
		Reply string `json:"reply"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	status := "approved"
	if !approve {
		status = "denied"
	}
	ok, err := decideAgentCommand(s.db, r.PathValue("cmdId"), r.PathValue("id"), userFromContext(r).ID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no pending command with that id")
		return
	}
	if ch, found := s.agentDecisions.Load(r.PathValue("cmdId")); found {
		ch.(chan agentDecision) <- agentDecision{approve: approve, reply: strings.TrimSpace(body.Reply)}
	} else {
		log.Printf("warning: agent command %s decided with no agent waiting", r.PathValue("cmdId"))
	}
	w.WriteHeader(http.StatusNoContent)
}
