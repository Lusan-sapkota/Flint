package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
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
	Kind          string           `json:"kind"` // "user" | "assistant" | "system" | "command"
	Content       string           `json:"content,omitempty"`
	Attachments   []timelineAttach `json:"attachments,omitempty"`
	CommandID     string           `json:"commandId,omitempty"`
	CommandText   string           `json:"commandText,omitempty"`
	CommandStatus string           `json:"commandStatus,omitempty"`
	CommandResult string           `json:"commandResult,omitempty"`
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

func buildTimeline(messages []Message, pending *Command) []timelineItem {
	toolResults := map[string]string{}
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID != nil {
			toolResults[*m.ToolCallID] = m.Content
		}
	}

	out := []timelineItem{}
	for _, m := range messages {
		switch m.Role {
		case "user":
			item := timelineItem{Kind: "user", Content: m.Content}
			for _, a := range m.Attachments {
				item.Attachments = append(item.Attachments, timelineAttach{ID: a.ID, MimeType: a.MimeType, Filename: a.Filename, IsImage: isImageMime(a.MimeType)})
			}
			out = append(out, item)
		case "system":
			out = append(out, timelineItem{Kind: "system", Content: m.Content})
		case "tool":
			continue // rendered as part of its paired "command" item, not standalone
		case "assistant":
			if m.Content != "" {
				out = append(out, timelineItem{Kind: "assistant", Content: m.Content})
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
				item.CommandResult = result
				item.CommandStatus = classifyCommandResult(result)
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
	Conversations []Conversation
	HasCurrent    bool
	CurrentID     string
	CurrentModel  string
	CurrentFolder string
	TimelineJSON  string
	Models        []OllamaModelInfo
	ModelsErr     string
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
		if full.AttachedFolder != nil {
			data.CurrentFolder = *full.AttachedFolder
		}

		pending, err := getPendingCommand(s.db, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		timeline = buildTimeline(full.Messages, pending)

		data.Title = full.Title
	} else {
		data.Title = "Flint"
	}
	data.User = user

	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	if err != nil {
		data.ModelsErr = err.Error()
	}
	data.Models = preferredOrAll(models, user.PreferredModels)

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
	ModelsErr           string
	OllamaBaseURL       string
	BraveAPIKey         string
	PreferredModelsJSON string
	QuestionsJSON       string
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request, user *User) {
	models, err := s.ollama.ListModels(r.Context(), s.ollamaURLFor(user))
	modelsErr := ""
	if err != nil {
		modelsErr = err.Error()
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
		ModelsErr:           modelsErr,
		PreferredModelsJSON: string(preferredJSON),
		QuestionsJSON:       string(questionsJSON),
	}
	if user.OllamaBaseURL != nil {
		data.OllamaBaseURL = *user.OllamaBaseURL
	}
	if user.BraveAPIKey != nil {
		data.BraveAPIKey = *user.BraveAPIKey
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
