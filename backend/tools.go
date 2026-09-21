package main

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
