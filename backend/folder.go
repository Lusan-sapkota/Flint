package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxAttachFileSize = 64 * 1024
const maxAttachTotalSize = 40 * 1024

func readFolderManifest(path string) (manifest string, included []string, err error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", nil, err
	}

	var body strings.Builder
	var skipped []string
	total := 0

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			skipped = append(skipped, name+"/ (subfolders aren't read; attach them directly if needed)")
			continue
		}

		full := filepath.Join(path, name)
		info, err := entry.Info()
		if err != nil {
			skipped = append(skipped, name+" (unreadable)")
			continue
		}
		if info.Size() > maxAttachFileSize {
			skipped = append(skipped, fmt.Sprintf("%s (too large: %d bytes)", name, info.Size()))
			continue
		}

		content, err := os.ReadFile(full)
		if err != nil {
			skipped = append(skipped, name+" (unreadable)")
			continue
		}
		if bytes.IndexByte(content, 0) != -1 {
			skipped = append(skipped, name+" (binary)")
			continue
		}

		if total+len(content) > maxAttachTotalSize {
			skipped = append(skipped, name+" (over the attachment size budget)")
			continue
		}

		body.WriteString("--- ")
		body.WriteString(name)
		body.WriteString(" ---\n")
		body.Write(content)
		body.WriteString("\n\n")
		total += len(content)
		included = append(included, name)
	}

	var header strings.Builder
	header.WriteString("Attached folder: ")
	header.WriteString(path)
	header.WriteString("\n\n")
	header.WriteString(body.String())
	if len(skipped) > 0 {
		header.WriteString("Not included: ")
		header.WriteString(strings.Join(skipped, ", "))
		header.WriteString("\n")
	}

	return header.String(), included, nil
}

func buildAnchorHeader(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}

	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}

	return fmt.Sprintf("[Workspace anchor]\nAttached folder: %s\nEntries: %s", path, strings.Join(names, ", "))
}
