package main

import (
	"regexp"
	"strings"
)

var asksToRunPattern = regexp.MustCompile(`(?i)\b(run|execute|check|command|terminal|shell)\b`)

// Gates noFolderNote: added to every message, it leaked into unrelated answers (E19).
func asksToRun(messages []Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return asksToRunPattern.MatchString(messages[i].Content)
		}
	}
	return false
}

// Without it, a folder-less chat asked to "run" something got fake output (E19).
const noFolderNote = "[No folder attached: you can't run commands in this chat.] If the user asks you to run or check something on their computer, don't pretend to. Give them the exact command to run themselves, and tell them they can attach a folder to this chat if they want you to run commands. Never make up a command's output."

const toolReasoningPrompt = "Before calling a tool, briefly think through what you need to do and why this specific command helps, in one or two sentences, then call the tool. Prefer one small, single-purpose command at a time over a larger one."

var runShellTool = OllamaTool{
	Type: "function",
	Function: OllamaToolFunction{
		Name:        "run_shell",
		Description: "Run a shell command in the attached project folder and return its combined stdout/stderr output. The user must approve every command before it runs, so prefer small, single-purpose commands.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the shell command to run",
				},
			},
			"required": []string{"command"},
		},
	},
}

var noCommandPhrases = []string{"without running", "without using", "without executing", "don't run", "dont run", "do not run", "don't execute", "do not execute", "no commands"}

// The nudge outweighs an explicit "without running any commands", so it's skipped then (E9, E15, E17).
func forbidsCommands(messages []Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			text := strings.ToLower(strings.ReplaceAll(messages[i].Content, "’", "'"))
			for _, p := range noCommandPhrases {
				if strings.Contains(text, p) {
					return true
				}
			}
			return false
		}
	}
	return false
}
