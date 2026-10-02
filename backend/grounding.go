package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	complete bool
}

func indexFolder(root string) folderIndex {
	ix := folderIndex{root: root, byBase: map[string][]string{}, complete: true}
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
			return fs.SkipDir
		}
		if n++; n > maxIndexedEntries {
			ix.complete = false
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		ix.byBase[d.Name()] = append(ix.byBase[d.Name()], filepath.ToSlash(rel))
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

// Indexes lazily, so a reply naming no files never walks the folder.
func missingPaths(root string) func(reply string) []string {
	var ix *folderIndex
	return func(reply string) []string {
		var missing []string
		for _, c := range pathCandidates(reply) {
			if ix == nil {
				built := indexFolder(root)
				ix = &built
			}
			if !ix.exists(c) {
				missing = append(missing, c)
			}
		}
		return missing
	}
}
