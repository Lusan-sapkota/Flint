package main

import (
	"fmt"
	"regexp"
)

var dangerousCommandPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	// "." and "*" too: the benchmark caught "rm -rf ~" rewritten as "cd ~ && rm -rf .".
	{regexp.MustCompile(`\brm\s+(?:-\S+\s+)*(?:-\w*(?:rf|fr)\w*|-\w*r\w*\s+-\w*f\w*|-\w*f\w*\s+-\w*r\w*)\s+(?:-\S+\s+)*(?:/|/\*|~/?|~/\*|\$HOME/?|\$HOME/\*|\.{1,2}/?|\./\*|\*)(?:[\s;&|]|$)`), "recursive force-delete targeting the filesystem root, home, or the whole current directory"},
	{regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`), "fork bomb"},
	{regexp.MustCompile(`(?i)\bsudo\b`), "privilege escalation (sudo)"},
	{regexp.MustCompile(`(?i)(curl|wget)\b[^|]*\|\s*(sh|bash|zsh)\b`), "piping a network download directly into a shell"},
	{regexp.MustCompile(`(?i)\bmkfs\b`), "filesystem formatting"},
	{regexp.MustCompile(`(?i)\bdd\b[^|]*of=/dev/`), "raw write to a block device"},
	{regexp.MustCompile(`(?i)(/etc/shadow|\.ssh/id_[a-z]+)\b`), "accessing credential or system secret files"},
}

func checkCommandShield(command string) (blocked bool, reason string) {
	for _, p := range dangerousCommandPatterns {
		if p.re.MatchString(command) {
			return true, p.reason
		}
	}
	return false, ""
}

func shieldBlockedMessage(reason string) string {
	return fmt.Sprintf(
		"[BLOCKED by safety shield: %s]\nThis command will not run, with or without approval, and no automated workaround will be accepted either. "+
			"If this action is genuinely necessary to complete the task, tell the user plainly what command they should run themselves in their own terminal, then continue once they confirm it's done. Do not attempt to route around the block.",
		reason,
	)
}
