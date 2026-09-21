package main

import "regexp"

// dangerousCommandPatterns is a hard floor beneath human approval, not a
// replacement for it: shell is Turing-complete, so this can be evaded and
// makes no formal safety guarantee. It exists to catch the categorically
// catastrophic cases - system-wide destruction, privilege escalation,
// remote code execution, credential exposure - regardless of whether a
// human approves without fully reading the command.
var dangerousCommandPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`rm\s+.*-[a-zA-Z]*rf[a-zA-Z]*\s+(/|/\*|~|\$HOME)(\s|$)`), "recursive force-delete targeting the filesystem root or home directory"},
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
