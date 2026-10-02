package main

import (
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ponytail: walk stops at this many entries; past it only direct paths are checked.
const maxIndexedEntries = 20000

var (
	fencedCode  = regexp.MustCompile("(?s)```.*?```")
	inlineCode  = regexp.MustCompile("`([^`\n]+)`")
	barePath    = regexp.MustCompile(`(?:\.{1,2}/|/)?[\w.-]*[\w-](?:/[\w.-]+)+`)
	urlPattern  = regexp.MustCompile(`[a-z][a-z0-9+.-]*://\S+`)
	lineSuffix  = regexp.MustCompile(`:\d+(?::\d+)?$`)
	notPathChar = regexp.MustCompile(`[\s*?{}<>|=$~"'()\[\],;]`)
)

// Only names with a file extension count, so `os.Stat` and `origin/main` never do. Extensions that are
// also common member names (`console.log`, `process.env`, `s.db`) count only in a path with a slash.
var fileExtensions, slashOnlyExtensions = extensionSet("go py ipynb js mjs cjs ts tsx jsx vue svelte html htm css scss md mdx rst txt json jsonl yaml yml toml ini xml csv tsv sql sh bash zsh ps1 bat cc cpp hpp rs java kt kts swift rb php lua dart zig hs scala gradle tf proto graphql pdf png jpg jpeg gif svg webp"),
	extensionSet("mod sum lock env db log conf cfg c h r fs cs pl ml ex exs erl")

func extensionSet(list string) map[string]bool {
	set := map[string]bool{}
	for _, e := range strings.Fields(list) {
		set[e] = true
	}
	return set
}

func pathCandidates(reply string) []string {
	text := urlPattern.ReplaceAllString(fencedCode.ReplaceAllString(reply, ""), "")
	var raw []string
	for _, m := range inlineCode.FindAllStringSubmatch(text, -1) {
		raw = append(raw, m[1])
	}
	raw = append(raw, barePath.FindAllString(inlineCode.ReplaceAllString(text, ""), -1)...)

	seen := map[string]bool{}
	var out []string
	for _, c := range raw {
		c = lineSuffix.ReplaceAllString(strings.TrimRight(strings.TrimSpace(c), ".,:;!?"), "")
		if c == "" || strings.Contains(c, "://") || notPathChar.MatchString(c) || strings.HasPrefix(c, "-") {
			continue
		}
		ext := strings.TrimPrefix(path.Ext(c), ".")
		if !(fileExtensions[ext] || slashOnlyExtensions[ext] && strings.Contains(c, "/")) || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

type folderIndex struct {
	root     string
	byBase   map[string][]string
	byFold   map[string][]string
	complete bool
}

func skippedDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "vendor"
}

func indexFolder(root string) folderIndex {
	ix := folderIndex{root: root, byBase: map[string][]string{}, byFold: map[string][]string{}, complete: true}
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		if d.IsDir() && skippedDir(d.Name()) {
			return fs.SkipDir
		}
		if n++; n > maxIndexedEntries {
			ix.complete = false
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		ix.byBase[d.Name()] = append(ix.byBase[d.Name()], rel)
		ix.byFold[strings.ToLower(d.Name())] = append(ix.byFold[strings.ToLower(d.Name())], rel)
		return nil
	})
	return ix
}

// A name counts as found if it exists as written, relative to the folder, or as the tail of a path in it.
func (ix folderIndex) exists(c string) bool {
	if filepath.IsAbs(c) {
		_, err := os.Stat(c)
		return err == nil
	}
	rel := strings.TrimPrefix(path.Clean(c), "./")
	if _, err := os.Stat(filepath.Join(ix.root, filepath.FromSlash(rel))); err == nil {
		return true
	}
	for _, p := range ix.byBase[path.Base(rel)] {
		if p == rel || strings.HasSuffix(p, "/"+rel) {
			return true
		}
	}
	return !ix.complete
}

// Real paths whose file name matches ignoring case: `agents.md` for AGENTS.md, `schema.go` elsewhere.
func (ix folderIndex) suggest(c string) []string {
	matches := ix.byFold[strings.ToLower(path.Base(c))]
	if len(matches) > 3 {
		return nil
	}
	return matches
}

type pathChecker struct {
	root string
	ix   *folderIndex
}

// Indexes lazily, so a reply naming no files never walks the folder.
func (pc *pathChecker) index() folderIndex {
	if pc.ix == nil {
		built := indexFolder(pc.root)
		pc.ix = &built
	}
	return *pc.ix
}

func (pc *pathChecker) missing(reply string) []string {
	var out []string
	for _, c := range pathCandidates(reply) {
		if !pc.index().exists(c) {
			out = append(out, c)
		}
	}
	return out
}

func missingPaths(root string) func(reply string) []string {
	return (&pathChecker{root: root}).missing
}

func groundingNote(messages []Message, folder string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" || strings.TrimSpace(messages[i].Content) == "" {
			continue
		}
		pc := &pathChecker{root: folder}
		missing := pc.missing(messages[i].Content)
		if missing == nil {
			return ""
		}
		names := make([]string, len(missing))
		for j, m := range missing {
			names[j] = "`" + m + "`"
			if real := pc.index().suggest(m); real != nil && !ablated["suggest"] {
				names[j] += " (exists as `" + strings.Join(real, "`, `") + "`)"
			}
		}
		return "[Not in the folder] Your last answer named files that don't exist in the attached folder: " +
			strings.Join(names, ", ") + ". Don't present them as real: check with a command, or say you aren't sure."
	}
	return ""
}

const maxTreeChars = 2400

// Real paths, grouped by folder and shallowest first, so a cut drops the deepest ones (E29).
func fileTree(root string) string {
	byDir := map[string][]string{}
	empty := map[string]bool{}
	n, files := 0, 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		parent, _ := filepath.Rel(root, filepath.Dir(p))
		delete(empty, filepath.ToSlash(parent))
		if d.IsDir() {
			if skippedDir(d.Name()) {
				return fs.SkipDir
			}
			rel, _ := filepath.Rel(root, p)
			empty[filepath.ToSlash(rel)] = true
			return nil
		}
		if n++; n > maxIndexedEntries {
			return fs.SkipAll
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		byDir[filepath.ToSlash(parent)] = append(byDir[filepath.ToSlash(parent)], d.Name())
		files++
		return nil
	})
	for dir := range empty {
		byDir[dir] = []string{"(empty)"}
	}
	dirs := slices.Collect(maps.Keys(byDir))
	depth := func(d string) int {
		if d == "." {
			return -1
		}
		return strings.Count(d, "/")
	}
	slices.SortFunc(dirs, func(a, b string) int {
		return cmp.Or(cmp.Compare(depth(a), depth(b)), cmp.Compare(a, b))
	})
	var b strings.Builder
	listed := 0
	for _, dir := range dirs {
		label := dir + "/"
		if dir == "." {
			label = "./"
		}
		line := label + ": " + strings.Join(byDir[dir], ", ") + "\n"
		if b.Len()+len(line) > maxTreeChars {
			continue
		}
		b.WriteString(line)
		if !empty[dir] {
			listed += len(byDir[dir])
		}
	}
	if n > maxIndexedEntries {
		b.WriteString("(folder too large to list fully)\n")
	} else if listed < files {
		fmt.Fprintf(&b, "(%d more files in deeper folders not listed)\n", files-listed)
	}
	return strings.TrimRight(b.String(), "\n")
}
