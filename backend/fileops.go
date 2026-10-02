package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxReadLines    = 400
	maxEditFileSize = 256 * 1024
	maxDiffInResult = 4000
)

func fileTool(name, desc string, props map[string]any, required ...string) OllamaTool {
	return OllamaTool{Type: "function", Function: OllamaToolFunction{Name: name, Description: desc, Parameters: map[string]any{
		"type": "object", "properties": props, "required": required,
	}}}
}

var (
	stringParam = map[string]any{"type": "string"}
	intParam    = map[string]any{"type": "integer"}

	readFileTool = fileTool("read_file",
		"Read a file in the attached folder. Returns its lines, each prefixed with its line number. Runs without asking the user.",
		map[string]any{"path": stringParam, "start_line": intParam, "end_line": intParam}, "path")
	writeFileTool = fileTool("write_file",
		"Create a new file, or replace a whole file, with the given content. The user reviews a diff before it is written.",
		map[string]any{"path": stringParam, "content": stringParam}, "path", "content")
	// Text, not line numbers: with line ranges qwen2.5-3b dropped indentation, 14/30 vs 19/30 (E30).
	editFileTool = fileTool("edit_file",
		"Change part of a file: replace old_text with new_text. old_text must be copied exactly from the file (without the line numbers read_file shows) and must appear exactly once; include a nearby line to make it unique. To add text after a line, put that line in old_text and repeat it at the start of new_text. To add text at the end of the file, leave old_text empty. The user reviews a diff before it is written.",
		map[string]any{"path": stringParam, "old_text": stringParam, "new_text": stringParam}, "path", "old_text", "new_text")
)

// Symlinks are resolved before the check, so a link inside the folder can't reach outside it.
// A path that doesn't exist yet is checked through its parent folder, which must exist.
func resolveInFolder(folder, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("no path given")
	}
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return "", err
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	real, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, os.ErrNotExist) {
		dir, derr := filepath.EvalSymlinks(filepath.Dir(abs))
		if derr != nil {
			return "", fmt.Errorf("folder %s doesn't exist", filepath.Dir(p))
		}
		real, err = filepath.Join(dir, filepath.Base(abs)), nil
	}
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the attached folder", p)
	}
	return real, nil
}

func displayPath(folder, path string) string {
	root, err := filepath.EvalSymlinks(folder)
	if rel, rerr := filepath.Rel(root, path); err == nil && rerr == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func readTextFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s doesn't exist", filepath.Base(path))
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a folder, not a file", filepath.Base(path))
	}
	if info.Size() > maxEditFileSize {
		return "", fmt.Errorf("%s is %d KB, over the %d KB limit", filepath.Base(path), info.Size()/1024, maxEditFileSize/1024)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) != -1 {
		return "", fmt.Errorf("%s is a binary file", filepath.Base(path))
	}
	return string(data), nil
}

func readFileResult(folder string, raw json.RawMessage) string {
	var a struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	_ = json.Unmarshal(raw, &a)
	path, err := resolveInFolder(folder, a.Path)
	if err == nil {
		var content string
		if content, err = readTextFile(path); err == nil {
			return numberedLines(displayPath(folder, path), content, a.StartLine, a.EndLine)
		}
	}
	return fmt.Sprintf("[FAILED to read: %v]", err)
}

func numberedLines(name, content string, start, end int) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	// A small model passed start_line 1000000000000000; reading from the top serves it better than an error.
	if start < 1 || start > len(lines) {
		start = 1
	}
	if end < start || end > len(lines) {
		end = len(lines)
	}
	end = min(end, start+maxReadLines-1)
	var b strings.Builder
	fmt.Fprintf(&b, "[read %s: lines %d-%d of %d]\n", name, start, end, len(lines))
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%4d\t%s\n", i, lines[i-1])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "[%d more lines: read again with start_line %d]\n", len(lines)-end, end+1)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// What an approved edit will write. Base is the file's hash when proposed, so a file changed since
// is refused rather than overwritten with an edit planned against the old text.
type plannedEdit struct {
	Path    string `json:"path"`
	Display string `json:"display"`
	Content string `json:"content"`
	Base    string `json:"base"`
	Diff    string `json:"diff"`
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func planEdit(folder, tool string, raw json.RawMessage) (plannedEdit, error) {
	var a struct {
		Path    string  `json:"path"`
		Content string  `json:"content"`
		OldText *string `json:"old_text"`
		NewText string  `json:"new_text"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return plannedEdit{}, errors.New("the arguments aren't valid JSON")
	}
	path, err := resolveInFolder(folder, a.Path)
	if err != nil {
		return plannedEdit{}, err
	}
	before, base := "", ""
	if _, statErr := os.Stat(path); statErr == nil {
		if before, err = readTextFile(path); err != nil {
			return plannedEdit{}, err
		}
		base = hashOf(before)
	} else if tool == "edit_file" {
		return plannedEdit{}, fmt.Errorf("%s doesn't exist; use write_file to create it", a.Path)
	}

	after := a.Content
	if tool == "edit_file" {
		old := ""
		if a.OldText != nil {
			old = *a.OldText
		}
		switch n := strings.Count(before, old); {
		case old == "":
			after = strings.TrimRight(before, "\n") + "\n" + strings.Trim(a.NewText, "\n") + "\n"
		case n == 0:
			// The real lines save the model a separate read: unread, it guessed the old value (E30).
			return plannedEdit{}, fmt.Errorf("old_text was not found in %s. Copy it exactly from these lines, without the line numbers:\n%s", displayPath(folder, path), numberedLines(displayPath(folder, path), before, 1, 200))
		case n > 1:
			return plannedEdit{}, fmt.Errorf("old_text appears %d times in %s. Include a nearby line so it matches exactly once", n, a.Path)
		default:
			after = strings.Replace(before, old, a.NewText, 1)
		}
	}
	if after == before {
		return plannedEdit{}, fmt.Errorf("this edit makes no change to %s", a.Path)
	}
	display := displayPath(folder, path)
	return plannedEdit{Path: path, Display: display, Content: after, Base: base, Diff: lineDiff(display, before, after)}, nil
}

// ponytail: one hunk between the common first and last lines; a real LCS diff if scattered whole-file rewrites need it.
func lineDiff(name, before, after string) string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	a, b := split(before), split(after)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	const context = 3
	from, toA, toB := max(pre-context, 0), min(len(a)-suf+context, len(a)), min(len(b)-suf+context, len(b))
	var d strings.Builder
	if before == "" {
		fmt.Fprintf(&d, "--- /dev/null\n+++ %s\n", name)
	} else {
		fmt.Fprintf(&d, "--- %s\n+++ %s\n", name, name)
	}
	fmt.Fprintf(&d, "@@ -%d,%d +%d,%d @@\n", from+1, toA-from, from+1, toB-from)
	for _, l := range a[from:pre] {
		d.WriteString(" " + l + "\n")
	}
	for _, l := range a[pre : len(a)-suf] {
		d.WriteString("-" + l + "\n")
	}
	for _, l := range b[pre : len(b)-suf] {
		d.WriteString("+" + l + "\n")
	}
	for _, l := range a[len(a)-suf : toA] {
		d.WriteString(" " + l + "\n")
	}
	return strings.TrimSuffix(d.String(), "\n")
}

func (s *Server) applyEdit(cmd *Command) (resultText, displayStatus string) {
	var e plannedEdit
	fail := func(err error) (string, string) {
		code := 1
		msg := fmt.Sprintf("[FAILED: %v]", err)
		if rerr := resolveCommand(s.db, cmd.ID, "error", msg, &code); rerr != nil {
			log.Printf("warning: failed to resolve edit: %v", rerr)
		}
		return msg, "failed"
	}
	if err := json.Unmarshal([]byte(*cmd.Edit), &e); err != nil {
		return fail(err)
	}
	current := ""
	if data, err := os.ReadFile(e.Path); err == nil {
		current = string(data)
	} else if e.Base != "" {
		return fail(fmt.Errorf("%s was removed since this edit was proposed", e.Display))
	}
	if (e.Base == "" && current != "") || (e.Base != "" && hashOf(current) != e.Base) {
		return fail(fmt.Errorf("%s changed since this edit was proposed. Read it again before editing", e.Display))
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(e.Path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := e.Path + ".flint-tmp"
	if err := os.WriteFile(tmp, []byte(e.Content), mode); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, e.Path); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	code := 0
	if err := resolveCommand(s.db, cmd.ID, "executed", e.Diff, &code); err != nil {
		log.Printf("warning: failed to resolve edit: %v", err)
	}
	diff := e.Diff
	if len(diff) > maxDiffInResult {
		diff = diff[:maxDiffInResult] + "\n...[diff cut]"
	}
	return fmt.Sprintf("[exit code: 0] wrote %s\n%s", e.Display, diff), "success"
}
