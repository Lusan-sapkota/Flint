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
	"regexp"
	"slices"
	"strings"
)

const (
	maxReadLines    = 400
	maxEditFileSize = 256 * 1024
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
	// Cloud only, so the local schema stays the one E30 measured. A cloud model renamed an import and
	// left the call below it, one edit per file (E31).
	cloudEditFileTool = fileTool("edit_file",
		"Change part of a file: replace old_text with new_text. old_text must be copied exactly from the file (without the line numbers read_file shows) and must appear exactly once; include a nearby line to make it unique. To change several places in one file, pass edits instead: a list of {old_text, new_text}, applied in order, each to the result of the one before; change every place that needs it, such as every call of a renamed function. To add text at the end of the file, leave old_text empty. The user reviews a diff before it is written.",
		map[string]any{"path": stringParam, "old_text": stringParam, "new_text": stringParam, "edits": map[string]any{
			"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"old_text": stringParam, "new_text": stringParam}, "required": []string{"old_text", "new_text"}},
		}}, "path")
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
	if err := checkPathShield(real); err != nil {
		return "", err
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
	return readFileOver(folder, raw, nil)
}

// staged maps a path to the content this turn's staged edits give it.
func readFileOver(folder string, raw json.RawMessage, staged map[string]string) string {
	var a struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	_ = json.Unmarshal(raw, &a)
	path, err := resolveInFolder(folder, a.Path)
	if err == nil {
		if c, ok := staged[path]; ok {
			return numberedLines(displayPath(folder, path)+" (with your staged edits)", c, a.StartLine, a.EndLine)
		}
		var content string
		if content, err = readTextFile(path); err == nil {
			return numberedLines(displayPath(folder, path), content, a.StartLine, a.EndLine)
		}
	}
	var blocked shieldError
	if errors.As(err, &blocked) {
		return shieldBlockedMessage(blocked.reason)
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
	Hunks   []hunk `json:"hunks,omitempty"`
	// The replaced region: lines kept above and below it, and its new lines, which the user may edit before approving.
	Pre      int    `json:"pre"`
	Suf      int    `json:"suf"`
	Editable string `json:"editable"`
	Adjusted string `json:"adjusted,omitempty"`
	Partial  string `json:"partial,omitempty"`
	Leftover string `json:"leftover,omitempty"`
	// Set instead for a cloud model's read, which waits for approval.
	Read json.RawMessage `json:"read,omitempty"`
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func planEdit(folder, tool string, raw json.RawMessage) (plannedEdit, error) {
	return planEditOver(folder, tool, raw, nil)
}

func planEditOver(folder, tool string, raw json.RawMessage, staged map[string]string) (plannedEdit, error) {
	type change struct {
		OldText *string `json:"old_text"`
		NewText string  `json:"new_text"`
	}
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		change
		Edits []change `json:"edits"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return plannedEdit{}, errors.New("the arguments aren't valid JSON")
	}
	path, err := resolveInFolder(folder, a.Path)
	if err != nil {
		return plannedEdit{}, err
	}
	before, base := "", ""
	if c, ok := staged[path]; ok {
		before, base = c, hashOf(c)
	} else if _, statErr := os.Stat(path); statErr == nil {
		if before, err = readTextFile(path); err != nil {
			return plannedEdit{}, err
		}
		base = hashOf(before)
	} else if tool == "edit_file" {
		return plannedEdit{}, fmt.Errorf("%s doesn't exist; use write_file to create it", a.Path)
	}

	after := a.Content
	if tool == "edit_file" {
		changes := a.Edits
		if len(changes) == 0 {
			changes = []change{a.change}
		}
		// Applied in order, each to the result of the one before, so later edits see earlier ones.
		after = before
		for i, c := range changes {
			which := "old_text"
			if len(changes) > 1 {
				which = fmt.Sprintf("edits[%d].old_text", i)
			}
			old := ""
			if c.OldText != nil {
				old = *c.OldText
			}
			switch n := strings.Count(after, old); {
			case old == "":
				after = strings.TrimRight(after, "\n") + "\n" + strings.Trim(c.NewText, "\n") + "\n"
			case n == 0:
				// The real lines save the model a separate read: unread, it guessed the old value (E30).
				msg := fmt.Sprintf("%s was not found in %s.", which, displayPath(folder, path))
				if line, text, ok := lineWithAll(after, old); ok && !ablated["edithint"] {
					msg += fmt.Sprintf(" Line %d contains every part of it: %q. Use that whole line as old_text, and as new_text the whole line with your change made.", line, text)
				}
				return plannedEdit{}, fmt.Errorf("%s Copy it exactly from these lines, without the line numbers:\n%s", msg, numberedLines(displayPath(folder, path), after, 1, 200))
			case n > 1:
				return plannedEdit{}, fmt.Errorf("%s appears %d times in %s. Include a nearby line so it matches exactly once", which, n, a.Path)
			default:
				after = strings.Replace(after, old, c.NewText, 1)
			}
		}
	}
	if after == before {
		return plannedEdit{}, fmt.Errorf("this edit makes no change to %s", a.Path)
	}
	display := displayPath(folder, path)
	oldLines, newLines := splitLines(before), splitLines(after)
	pre, suf := diffBounds(oldLines, newLines)
	diff, hunks := diffWithHunks(display, before, after)
	var olds, news []string
	for _, c := range append(a.Edits, a.change) {
		if c.OldText != nil {
			olds, news = append(olds, *c.OldText), append(news, c.NewText)
		}
	}
	return plannedEdit{Path: path, Display: display, Content: after, Base: base, Diff: diff, Hunks: hunks, Leftover: leftoverNote(display, strings.Join(olds, "\n"), strings.Join(news, "\n"), after),
		Pre: pre, Suf: suf, Editable: strings.Join(newLines[pre:len(newLines)-suf], "\n")}, nil
}

// The user's version of the edited lines replaces the model's; the lines around them stay as they are on disk.
func adjustEdit(e *plannedEdit, replacement string) error {
	if replacement == e.Editable {
		return nil
	}
	current := ""
	if data, err := os.ReadFile(e.Path); err == nil {
		current = string(data)
	}
	if hashOf(current) != e.Base && !(e.Base == "" && current == "") {
		return fmt.Errorf("%s changed since this edit was proposed", e.Display)
	}
	a := splitLines(current)
	lines := append(append(append([]string{}, a[:e.Pre]...), splitLines(replacement)...), a[len(a)-e.Suf:]...)
	after := strings.Join(lines, "\n")
	if strings.HasSuffix(e.Content, "\n") && after != "" {
		after += "\n"
	}
	e.Content, e.Adjusted = after, replacement
	e.Diff, e.Hunks = diffWithHunks(e.Display, current, after)
	return nil
}

// Only the hunks the user kept are written; the model is told which were left out.
func keepHunks(e *plannedEdit, keep []int) error {
	if len(keep) == 0 || len(keep) >= len(e.Hunks) {
		return nil
	}
	current := ""
	if data, err := os.ReadFile(e.Path); err == nil {
		current = string(data)
	}
	if hashOf(current) != e.Base && !(e.Base == "" && current == "") {
		return fmt.Errorf("%s changed since this edit was proposed", e.Display)
	}
	kept := make([]bool, len(e.Hunks))
	for _, i := range keep {
		if i < 0 || i >= len(kept) {
			return fmt.Errorf("there is no change %d", i+1)
		}
		kept[i] = true
	}
	after := strings.Join(applyHunks(splitLines(current), e.Hunks, kept), "\n")
	if strings.HasSuffix(e.Content, "\n") && after != "" {
		after += "\n"
	}
	var left []string
	for i, h := range e.Hunks {
		if !kept[i] {
			left = append(left, fmt.Sprintf("change %d (old lines %d-%d)", i+1, h.A+1, h.A+h.Del))
		}
	}
	e.Partial = fmt.Sprintf("The user wrote only some of your changes. Not written: %s. Read the file again before editing it.", strings.Join(left, ", "))
	e.Content = after
	e.Diff, e.Hunks = diffWithHunks(e.Display, current, after)
	return nil
}

var identifierRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)

// A name the edit took out that the file still uses: renaming an import, a cloud model left the call
// below it and said every reference was updated (E31).
func leftoverNote(display, old, new, after string) string {
	if ablated["leftover"] {
		return ""
	}
	kept := map[string]bool{}
	for _, w := range identifierRe.FindAllString(new, -1) {
		kept[w] = true
	}
	var notes []string
	seen := map[string]bool{}
	for _, w := range identifierRe.FindAllString(old, -1) {
		if kept[w] || seen[w] {
			continue
		}
		seen[w] = true
		var at []string
		for i, l := range splitLines(after) {
			if slices.Contains(identifierRe.FindAllString(l, -1), w) {
				at = append(at, fmt.Sprint(i+1))
			}
		}
		if len(at) > 0 {
			notes = append(notes, fmt.Sprintf("%s (line %s)", w, strings.Join(at, ", ")))
		}
	}
	if len(notes) == 0 {
		return ""
	}
	return fmt.Sprintf("Your edit removed names that %s still uses: %s. If those should change too, edit them as well.", display, strings.Join(notes, "; "))
}

// The one line holding every fragment of a missed old_text. Shown only the file, qwen2.5-3b proposed
// "4.50\nB-220" for the line "B-220,4.50" six times in a row (E30).
func lineWithAll(content, old string) (line int, text string, ok bool) {
	var parts []string
	for _, p := range strings.Split(old, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return 0, "", false
	}
	for i, l := range splitLines(content) {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(l, p)
		}
		if all {
			if ok {
				return 0, "", false
			}
			line, text, ok = i+1, l, true
		}
	}
	return line, text, ok
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Lines the two versions share at the start and at the end.
func diffBounds(a, b []string) (pre, suf int) {
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	return pre, suf
}

type diffOp struct {
	kind byte // ' ', '-' or '+'
	text string
}

// ponytail: quadratic LCS on the changed middle, one block past maxDiffCells; Myers if big rewrites need hunks.
const maxDiffCells = 1 << 20

func diffOps(a, b []string) []diffOp {
	pre, suf := diffBounds(a, b)
	am, bm := a[pre:len(a)-suf], b[pre:len(b)-suf]
	var ops []diffOp
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	n, m := len(am), len(bm)
	if n*m > maxDiffCells {
		for _, l := range am {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range bm {
			ops = append(ops, diffOp{'+', l})
		}
	} else {
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		for i, j := 0, 0; i < n || j < m; {
			switch {
			case i < n && j < m && am[i] == bm[j]:
				ops = append(ops, diffOp{' ', am[i]})
				i++
				j++
			case j < m && (i == n || lcs[i][j+1] > lcs[i+1][j]):
				ops = append(ops, diffOp{'+', bm[j]})
				j++
			default:
				ops = append(ops, diffOp{'-', am[i]})
				i++
			}
		}
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

// One reviewable change: old lines [A, A+Del) become Add. from/to bound its ops, context included.
type hunk struct {
	A              int      `json:"a"`
	Del            int      `json:"del"`
	Add            []string `json:"add"`
	B              int      `json:"-"`
	from, to, lead int
}

const diffContext = 3

// Changes closer than twice the context share a hunk, as in diff -u.
func diffHunks(ops []diffOp) []hunk {
	var hs []hunk
	start, last, ai, bi := -1, -1, 0, 0
	var cur hunk
	closeHunk := func() {
		for _, op := range ops[start : last+1] {
			if op.kind != '+' {
				cur.Del++
			}
			if op.kind != '-' {
				cur.Add = append(cur.Add, op.text)
			}
		}
		cur.from, cur.to = max(start-diffContext, 0), min(last+1+diffContext, len(ops))
		cur.lead = start - cur.from
		hs = append(hs, cur)
	}
	for k, op := range ops {
		if op.kind != ' ' {
			if start >= 0 && k-last-1 > 2*diffContext {
				closeHunk()
				start = -1
			}
			if start < 0 {
				start, cur = k, hunk{A: ai, B: bi}
			}
			last = k
		}
		if op.kind != '+' {
			ai++
		}
		if op.kind != '-' {
			bi++
		}
	}
	if start >= 0 {
		closeHunk()
	}
	return hs
}

func lineDiff(name, before, after string) string {
	d, _ := diffWithHunks(name, before, after)
	return d
}

func diffWithHunks(name, before, after string) (string, []hunk) {
	ops := diffOps(splitLines(before), splitLines(after))
	hs := diffHunks(ops)
	var d strings.Builder
	if before == "" {
		fmt.Fprintf(&d, "--- /dev/null\n+++ %s\n", name)
	} else {
		fmt.Fprintf(&d, "--- %s\n+++ %s\n", name, name)
	}
	for _, h := range hs {
		oldLen, newLen := 0, 0
		for _, op := range ops[h.from:h.to] {
			if op.kind != '+' {
				oldLen++
			}
			if op.kind != '-' {
				newLen++
			}
		}
		fmt.Fprintf(&d, "@@ -%d,%d +%d,%d @@\n", h.A-h.lead+1, oldLen, h.B-h.lead+1, newLen)
		for _, op := range ops[h.from:h.to] {
			d.WriteString(string(op.kind) + op.text + "\n")
		}
	}
	return strings.TrimSuffix(d.String(), "\n"), hs
}

// Writes only the kept hunks onto the lines they were planned against.
func applyHunks(a []string, hs []hunk, keep []bool) []string {
	var out []string
	pos := 0
	for i, h := range hs {
		out = append(out, a[pos:h.A]...)
		if keep[i] {
			out = append(out, h.Add...)
		} else {
			out = append(out, a[h.A:h.A+h.Del]...)
		}
		pos = h.A + h.Del
	}
	return append(out, a[pos:]...)
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
	if e.Read != nil {
		out := readFileOver(cmd.Cwd, e.Read, stagedContents(s.db, cmd.ConversationID))
		code := 0
		if err := resolveCommand(s.db, cmd.ID, "executed", out, &code); err != nil {
			log.Printf("warning: failed to resolve read: %v", err)
		}
		return out, "read"
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
	if err := writeAtomic(e.Path, e.Content); err != nil {
		return fail(err)
	}
	code := 0
	if err := resolveCommand(s.db, cmd.ID, "executed", e.Diff, &code); err != nil {
		log.Printf("warning: failed to resolve edit: %v", err)
	}
	// No diff for the model: shown one, qwen2.5-3b pasted it into its reply as broken markdown.
	out := fmt.Sprintf("[exit code: 0] wrote %s (%s)", e.Display, diffCounts(e.Diff))
	if e.Adjusted != "" {
		out += "\nThe user changed your proposed text before approving. In place of your new_text, these lines were written:\n" + e.Adjusted
	}
	if e.Partial != "" {
		out += "\n" + e.Partial
	}
	if e.Leftover != "" {
		out += "\n" + e.Leftover
	}
	return out, "success"
}

func writeAtomic(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".flint-tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func diffCounts(diff string) string {
	added, removed := 0, 0
	lines := strings.Split(diff, "\n")
	for _, l := range lines[min(2, len(lines)):] {
		switch {
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return fmt.Sprintf("%d %s added, %d removed", added, plural(added, "line"), removed)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
