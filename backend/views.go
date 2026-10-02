package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"maps"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
)

const templatesDir = "../frontend/templates"

type pages struct {
	login    *template.Template
	signup   *template.Template
	recover  *template.Template
	chat     *template.Template
	settings *template.Template
}

var templateFuncs = template.FuncMap{
	"jsStr": template.JSEscapeString,
	"initial": func(s string) string {
		for _, r := range s {
			return string(r)
		}
		return ""
	},
	"humanSize": func(bytes int64) string {
		return humanize.Bytes(uint64(bytes))
	},
}

func parsePage(page string) *template.Template {
	return template.Must(template.New("base.html").Funcs(templateFuncs).ParseFiles(
		filepath.Join(templatesDir, "base.html"),
		filepath.Join(templatesDir, page),
	))
}

func loadPages() *pages {
	return &pages{
		login:    parsePage("login.html"),
		signup:   parsePage("signup.html"),
		recover:  parsePage("recover.html"),
		chat:     parsePage("chat.html"),
		settings: parsePage("settings.html"),
	}
}

func (s *Server) render(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template render error: %v", err)
	}
}

type baseData struct {
	Title string
	User  *User
}

type timelineItem struct {
	Kind          string           `json:"kind"` // "user" | "assistant" | "system" | "command" | "sources"
	Content       string           `json:"content,omitempty"`
	Attachments   []timelineAttach `json:"attachments,omitempty"`
	CommandID     string           `json:"commandId,omitempty"`
	CommandText   string           `json:"commandText,omitempty"`
	CommandStatus string           `json:"commandStatus,omitempty"`
	CommandResult string           `json:"commandResult,omitempty"`
	TokensPerSec  float64          `json:"tokensPerSec,omitempty"`
	Thinking      string           `json:"thinking,omitempty"`
	Query         string           `json:"query,omitempty"`
	Sources       []sourceLink     `json:"sources,omitempty"`
	Run           *AgentRun        `json:"run,omitempty"`
	recall        bool
}

type timelineAttach struct {
	ID       string `json:"id"`
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	IsImage  bool   `json:"isImage"`
}

func classifyCommandResult(result string) string {
	switch {
	case strings.HasPrefix(result, "[exit code: 0]"):
		return "success"
	case strings.HasPrefix(result, "[FAILED"):
		return "failed"
	case strings.HasPrefix(result, "[BLOCKED by safety shield"):
		return "blocked"
	case strings.HasPrefix(result, "[PRECONDITION FAILED"):
		return "precondition_failed"
	case strings.HasPrefix(result, "User denied"):
		return "denied"
	default:
		return "unknown"
	}
}

// buildTimeline also places memories saved from this chat, and `@agent`
// runs with the task that started them, at the point they happened, by
// time: they're not messages, so the model never sees them.
func buildTimeline(messages []Message, pending *Command, saved []Memory, runs []AgentRun) []timelineItem {
	toolResults := map[string]string{}
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID != nil {
			toolResults[*m.ToolCallID] = m.Content
		}
	}

	// A finished run saved its task as a user message; the run already
	// shows that task, and its results message becomes the results block.
	byMessage := map[int64]*AgentRun{}
	for i := range runs {
		if runs[i].MessageID != nil {
			byMessage[*runs[i].MessageID] = &runs[i]
		}
	}
	var lastRun *AgentRun

	out := []timelineItem{}
	flushSaved := func(before int64) {
		for len(saved) > 0 && saved[0].CreatedAt < before {
			out = append(out, timelineItem{Kind: "memorySaved", Content: saved[0].Content})
			saved = saved[1:]
		}
		for len(runs) > 0 && runs[0].CreatedAt < before {
			out = append(out, timelineItem{Kind: "user", Content: "@agent " + runs[0].Task}, timelineItem{Kind: "agentPlan", Run: &runs[0]})
			runs = runs[1:]
		}
	}
	for _, m := range messages {
		flushSaved(m.CreatedAt)
		switch m.Role {
		case "user":
			if run, ok := byMessage[m.ID]; ok {
				lastRun = run
				continue
			}
			item := timelineItem{Kind: "user", Content: m.Content}
			// The tags are stripped before saving so the model sees plain
			// text; show them again the way the user typed them.
			if n := len(out); n > 0 && out[n-1].Kind == "sources" {
				item.Content = "@web " + m.Content
			} else if n > 0 && out[n-1].Kind == "system" && out[n-1].recall {
				item.Content = "@memory " + m.Content
			}
			for _, a := range m.Attachments {
				item.Attachments = append(item.Attachments, timelineAttach{ID: a.ID, MimeType: a.MimeType, Filename: a.Filename, IsImage: isImageMime(a.MimeType)})
			}
			// A search or recall is saved just before the message that asked
			// for it; show it after.
			if n := len(out); n > 0 && (out[n-1].Kind == "sources" || out[n-1].recall) {
				out = append(out[:n-1], item, out[n-1])
			} else {
				out = append(out, item)
			}
		case "system":
			if lastRun != nil && strings.HasPrefix(m.Content, agentResultsPrefix) {
				out = append(out, timelineItem{Kind: "agentResults", Run: lastRun})
				lastRun = nil
				continue
			}
			if query, results, ok := parseSearchResults(m.Content); ok {
				view := sourcesView(query, results)
				out = append(out, timelineItem{Kind: "sources", Query: view.Query, Sources: view.Sources})
				continue
			}
			if query, body, ok := parseRecallHeader(m.Content); ok {
				out = append(out, timelineItem{Kind: "system", Content: fmt.Sprintf("Recalled memories for %q\n%s", query, body), recall: true})
				continue
			}
			out = append(out, timelineItem{Kind: "system", Content: m.Content})
		case "tool":
			continue // rendered as part of its paired "command" item, not standalone
		case "assistant":
			if m.Content != "" || m.Thinking != "" {
				item := timelineItem{Kind: "assistant", Content: m.Content, Thinking: m.Thinking}
				if m.TokensPerSec != nil {
					item.TokensPerSec = math.Round(*m.TokensPerSec*10) / 10
				}
				out = append(out, item)
			}
			if m.ToolCalls == nil || *m.ToolCalls == "" {
				continue
			}
			var calls []OllamaToolCall
			if err := json.Unmarshal([]byte(*m.ToolCalls), &calls); err != nil || len(calls) == 0 {
				continue
			}
			tc := calls[0]
			item := timelineItem{Kind: "command", CommandText: extractToolCommand(*m.ToolCalls)}
			if result, ok := toolResults[tc.ID]; ok {
				item.CommandStatus = classifyCommandResult(result)
				if item.CommandStatus != "denied" {
					item.CommandResult = result
				}
			} else if pending != nil {
				item.CommandID = pending.ID
				item.CommandText = pending.Command
				item.CommandStatus = "pending"
			} else {
				item.CommandStatus = "unknown"
			}
			out = append(out, item)
		}
	}
	flushSaved(math.MaxInt64)
	return out
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.sessionUser(r); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, s.pages.login, baseData{Title: "Log in"})
}

func (s *Server) handleSignupPage(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.sessionUser(r); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, s.pages.signup, baseData{Title: "Sign up"})
}

func (s *Server) handleRecoverPage(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.sessionUser(r); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, s.pages.recover, baseData{Title: "Recover account"})
}

type chatViewData struct {
	baseData
	Conversations       []Conversation
	HasCurrent          bool
	CurrentID           string
	CurrentModel        string
	CurrentModelMissing bool
	SelectedModel       string
	CanThink            bool
	CanSee              bool
	CurrentUpdatedAt    int64
	ContextUsed         int
	ContextMax          int
	MaxInput            int
	CharsPerToken       float64
	Condensed           int
	CurrentFolder       string
	CurrentModelHost    string
	TimelineJSON        string
	Models              []OllamaModelInfo
	ModelsErr           string
}

func (s *Server) handleChatPage(w http.ResponseWriter, r *http.Request, user *User) {
	id := r.PathValue("id")

	convos, err := listConversations(s.db, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := chatViewData{Conversations: convos}
	timeline := []timelineItem{}
	if id != "" {
		full, err := getConversation(s.db, id, user.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if full == nil {
			http.NotFound(w, r)
			return
		}
		data.HasCurrent = true
		data.CurrentID = full.ID
		data.CurrentModel = full.Model
		data.CurrentUpdatedAt = full.UpdatedAt
		data.ContextUsed = full.ContextUsed
		data.ContextMax = full.ContextMax
		// Before the first reply there's no measured use yet, but the window
		// is known, so the meter shows from the start (at 0).
		if data.ContextMax == 0 {
			data.ContextMax = s.numCtxFor(user, full.Conversation)
		}
		data.MaxInput = s.maxInputTokens(user, full, s.numCtxFor(user, full.Conversation))
		data.CharsPerToken = charsPerToken / full.TokenRatio
		data.Condensed = condensedCount(full.Messages, full.Summaries)
		if full.AttachedFolder != nil {
			data.CurrentFolder = *full.AttachedFolder
		}

		pending, err := getPendingCommand(s.db, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		saved, err := chatMemories(s.db, user.ID, full.ID)
		if err != nil {
			log.Printf("warning: loading this chat's memories: %v", err)
		}
		runs, err := listAgentRuns(s.db, full.ID, user.ID)
		if err != nil {
			log.Printf("warning: loading this chat's agent runs: %v", err)
		}
		for i := range runs {
			if runs[i].Status != "planned" {
				continue
			}
			if full.AttachedFolder != nil {
				files, _ := agentFiles(*full.AttachedFolder)
				runs[i].FolderFiles = slices.Sorted(maps.Keys(files))
			}
			runs[i].WebAvailable = user.BraveAPIKey != nil && *user.BraveAPIKey != ""
			runs[i].CanExplore = s.canExplore(r.Context(), user, full.Conversation)
			runs[i].MaxCommands = agentCommandsFor(user)
		}
		timeline = buildTimeline(full.Messages, pending, saved, runs)

		data.Title = full.Title
	} else {
		data.Title = "Flint"
	}
	data.User = user

	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	if err != nil {
		data.ModelsErr = describeOllamaError(err, s.ollamaURLFor(user))
	}
	data.Models = preferredOrAll(s.ollama.ChatModels(r.Context(), s.ollamaURLFor(user), models), user.PreferredModels)
	data.CurrentModelMissing = data.HasCurrent && err == nil && !hasModel(models, data.CurrentModel)
	// Default the New chat picker to the open chat's model, else the most
	// recently used one that's still offered (convos are newest first).
	data.SelectedModel = data.CurrentModel
	for i := 0; !hasModel(data.Models, data.SelectedModel) && i < len(convos); i++ {
		data.SelectedModel = convos[i].Model
	}
	// Unknown capabilities (an older Ollama) never trigger the "can't see
	// images" warning, only a positive report of no vision does.
	data.CanSee = true
	for _, m := range models {
		if m.Name == data.CurrentModel {
			caps := s.ollama.Capabilities(r.Context(), s.ollamaURLFor(user), m)
			data.CanThink = slices.Contains(caps, "thinking")
			data.CanSee = caps == nil || slices.Contains(caps, "vision")
			data.CurrentModelHost = remoteHostName(m.RemoteHost)
		}
	}

	timelineJSON, err := json.Marshal(timeline)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.TimelineJSON = string(timelineJSON)

	s.render(w, s.pages.chat, data)
}

type settingsQuestionView struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}

type settingsViewData struct {
	baseData
	Models              []OllamaModelInfo
	ChatModels          []OllamaModelInfo
	ModelsErr           string
	OllamaBaseURL       string
	NumCtx              int
	CloudNumCtx         int
	MaxAgents           int
	CloudMaxAgents      int
	AgentCommands       int
	BraveKeyHint        string
	PreferredModelsJSON string
	QuestionsJSON       string
	RankingModel        string
	RankingModelMissing bool
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request, user *User) {
	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	modelsErr := ""
	if err != nil {
		modelsErr = describeOllamaError(err, s.ollamaURLFor(user))
	}

	questions, err := getSecurityQuestionsForUser(s.db, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	questionViews := make([]settingsQuestionView, len(questions))
	for i, q := range questions {
		questionViews[i] = settingsQuestionView{ID: q.ID, Question: q.Question}
	}
	questionsJSON, err := json.Marshal(questionViews)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	preferredJSON, err := json.Marshal(user.PreferredModels)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := settingsViewData{
		baseData:            baseData{Title: "Settings", User: user},
		Models:              models,
		ChatModels:          s.ollama.ChatModels(r.Context(), s.ollamaURLFor(user), models),
		ModelsErr:           modelsErr,
		PreferredModelsJSON: string(preferredJSON),
		QuestionsJSON:       string(questionsJSON),
		RankingModel:        embeddingModel,
		RankingModelMissing: modelsErr == "" && !slices.ContainsFunc(models, func(m OllamaModelInfo) bool { return isRankingModel(m.Name) }),
	}
	if user.OllamaBaseURL != nil {
		data.OllamaBaseURL = *user.OllamaBaseURL
	}
	if user.NumCtx != nil {
		data.NumCtx = *user.NumCtx
	}
	if user.CloudNumCtx != nil {
		data.CloudNumCtx = *user.CloudNumCtx
	}
	if user.MaxAgents != nil {
		data.MaxAgents = *user.MaxAgents
	}
	if user.CloudMaxAgents != nil {
		data.CloudMaxAgents = *user.CloudMaxAgents
	}
	if user.AgentCommands != nil {
		data.AgentCommands = *user.AgentCommands
	}
	// Only the last four characters ever reach the page: enough to tell keys
	// apart, not enough to use one.
	if user.BraveAPIKey != nil && *user.BraveAPIKey != "" {
		key := *user.BraveAPIKey
		data.BraveKeyHint = key[max(0, len(key)-4):]
	}

	s.render(w, s.pages.settings, data)
}

func (s *Server) handleCreateConversationPage(w http.ResponseWriter, r *http.Request, user *User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	model := strings.TrimSpace(r.FormValue("model"))
	if model == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	c, err := createConversation(s.db, uuid.NewString(), user.ID, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/c/"+c.ID, http.StatusSeeOther)
}

// Falls back to every model when none of the preferred ones are installed
// anymore, so a stale preference can't leave the New chat picker empty.
func preferredOrAll(models []OllamaModelInfo, preferred []string) []OllamaModelInfo {
	var kept []OllamaModelInfo
	for _, m := range models {
		if slices.Contains(preferred, m.Name) {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return models
	}
	return kept
}

func hasModel(models []OllamaModelInfo, name string) bool {
	return slices.ContainsFunc(models, func(m OllamaModelInfo) bool { return m.Name == name })
}

// remoteHostName shows a cloud model's host as a plain name, "ollama.com".
func remoteHostName(host string) string {
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		return u.Host
	}
	return host
}
