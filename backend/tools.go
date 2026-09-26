package main

import "strings"

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

// The nudge presupposes a command ("...then call the tool") and outweighs a
// user's explicit "without running any commands" (E9, E15, E17), so it's
// left off when the latest user message forbids commands.
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
