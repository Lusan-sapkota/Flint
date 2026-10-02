package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// A cloud model's edits in one turn are staged, not proposed one by one: its later reads and edits see
// them, and the user reviews them all after its reply, as one change across files (E32).
func (s *Server) stagesEdits(user *User, model string) bool {
	return s.ollama.modelInfo(s.ollamaURLFor(user), model).RemoteHost != "" && !ablated["staging"]
}

func stagedContents(db *sql.DB, conversationID string) map[string]string {
	cmds, err := stagedEdits(db, conversationID)
	if err != nil {
		log.Printf("warning: failed to load staged edits: %v", err)
	}
	out := map[string]string{}
	for _, c := range cmds {
		var e plannedEdit
		if json.Unmarshal([]byte(*c.Edit), &e) == nil {
			out[e.Path] = e.Content
		}
	}
	return out
}

// One file's staged edits combined: the file as it is now against the last staged version.
type reviewFile struct {
	Display string `json:"path"`
	Diff    string `json:"diff"`
	Error   string `json:"error,omitempty"`
	Status  string `json:"status,omitempty"`

	path, current, after string
	hunks                []hunk
	cmds                 []Command
}

func buildReview(db *sql.DB, conversationID string) ([]*reviewFile, error) {
	cmds, err := stagedEdits(db, conversationID)
	if err != nil {
		return nil, err
	}
	byPath := map[string]*reviewFile{}
	var files []*reviewFile
	for _, c := range cmds {
		var e plannedEdit
		if err := json.Unmarshal([]byte(*c.Edit), &e); err != nil {
			return nil, err
		}
		f := byPath[e.Path]
		if f == nil {
			f = &reviewFile{Display: e.Display, path: e.Path}
			if data, err := os.ReadFile(e.Path); err == nil {
				f.current = string(data)
			}
			// Only the first staged edit of a file was planned against the disk.
			if (e.Base == "" && f.current != "") || (e.Base != "" && hashOf(f.current) != e.Base) {
				f.Error = e.Display + " changed since these edits were proposed"
			}
			byPath[e.Path] = f
			files = append(files, f)
		}
		f.after = e.Content
		f.cmds = append(f.cmds, c)
	}
	for _, f := range files {
		f.Diff, f.hunks = diffWithHunks(f.Display, f.current, f.after)
	}
	return files, nil
}

func (s *Server) handleGetReview(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	convo, err := getConversation(s.db, r.PathValue("id"), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	files, err := buildReview(s.db, convo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// Write or discard every staged edit; keep lists, per file, the changes to write (all when absent).
func (s *Server) handleDecideReview(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	convo, err := getConversation(s.db, r.PathValue("id"), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if convo == nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	var body struct {
		Write bool             `json:"write"`
		Keep  map[string][]int `json:"keep"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	defer s.lockConversation(convo.ID)()

	files, err := buildReview(s.db, convo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "no staged edits")
		return
	}
	for _, f := range files {
		status, result := s.decideReviewFile(f, body.Write, body.Keep)
		f.Status = status
		for _, c := range f.cmds {
			if err := resolveCommand(s.db, c.ID, status, result, nil); err != nil {
				log.Printf("warning: failed to resolve staged edit: %v", err)
			}
			if err := setStagedResult(s.db, convo.ID, c.ToolCallID, c.Command, result); err != nil {
				log.Printf("warning: failed to rewrite staged result: %v", err)
			}
		}
	}
	if err := touchConversation(s.db, convo.ID); err != nil {
		log.Printf("warning: failed to touch conversation: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) decideReviewFile(f *reviewFile, write bool, keep map[string][]int) (status, result string) {
	denied := fmt.Sprintf("User denied your staged edits to %s after your reply. Nothing was written; %s is unchanged.", f.Display, f.Display)
	if !write {
		return "denied", denied
	}
	if f.Error != "" {
		return "error", fmt.Sprintf("[FAILED: %s. Nothing was written. Read it again before editing it.]", f.Error)
	}
	after, note := f.after, ""
	if idx, ok := keep[f.Display]; ok && len(idx) < len(f.hunks) {
		if len(idx) == 0 {
			return "denied", denied
		}
		kept := make([]bool, len(f.hunks))
		var left []string
		for _, i := range idx {
			if i >= 0 && i < len(kept) {
				kept[i] = true
			}
		}
		for i, h := range f.hunks {
			if !kept[i] {
				left = append(left, fmt.Sprintf("change %d (old lines %d-%d)", i+1, h.A+1, h.A+h.Del))
			}
		}
		after = strings.Join(applyHunks(splitLines(f.current), f.hunks, kept), "\n")
		if strings.HasSuffix(f.after, "\n") && after != "" {
			after += "\n"
		}
		note = fmt.Sprintf(" The user wrote only some of the changes. Not written: %s. Read the file again before editing it.", strings.Join(left, ", "))
	}
	if err := writeAtomic(f.path, after); err != nil {
		return "error", fmt.Sprintf("[FAILED: %v. Nothing was written.]", err)
	}
	f.Diff, f.hunks = diffWithHunks(f.Display, f.current, after)
	return "executed", fmt.Sprintf("[exit code: 0] The user reviewed your staged edits after your reply and wrote %s (%s).%s", f.Display, diffCounts(f.Diff), note)
}
