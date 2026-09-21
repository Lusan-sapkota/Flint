package main

import "encoding/json"

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

func buildHistory(messages []Message) []OllamaMessage {
	history := make([]OllamaMessage, 0, len(messages))
	for _, m := range messages {
		om := OllamaMessage{Role: m.Role, Content: m.Content}
		if m.ToolCalls != nil {
			_ = json.Unmarshal([]byte(*m.ToolCalls), &om.ToolCalls)
		}
		if m.ToolCallID != nil {
			om.ToolCallID = *m.ToolCallID
		}
		history = append(history, om)
	}
	return history
}
