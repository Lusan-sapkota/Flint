package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var shellMetacharacters = regexp.MustCompile("[|&;$`<>*?~{}()\\[\\]!]")

var shellBuiltins = map[string]bool{
	"cd": true, "echo": true, "export": true, "pwd": true, "test": true,
	"true": true, "false": true, "read": true, "set": true, "unset": true,
	"alias": true, "exit": true, "source": true, ".": true, "eval": true,
	"type": true, "command": true, "printf": true, "shift": true,
	"return": true, "wait": true, "trap": true, "umask": true, "ulimit": true,
}

// pathCheckableCommands is deliberately small: commands where "the last
// plain argument is a path that must already exist" is unambiguous. Trying
// to infer this generically for arbitrary shell syntax is a much harder,
// fragile problem not worth half-solving here.
var pathCheckableCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"wc": true, "file": true, "stat": true, "readlink": true,
}

func binaryExists(name string) bool {
	if shellBuiltins[name] {
		return true
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func looksLikePath(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-")
}

// checkCommandPreconditions is a Hoare-triple-style precondition gate: cheap,
// deterministic, zero model cost, run before a command is ever offered for
// human approval. It catches commands that would fail immediately for
// reasons the model could have checked itself - a missing binary or a
// nonexistent target file - so the user doesn't burn an approval cycle on
// something guaranteed to fail.
func checkCommandPreconditions(command, cwd string) (ok bool, reason string) {
	trimmed := strings.TrimSpace(command)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return true, ""
	}

	binary := fields[0]
	if !binaryExists(binary) {
		return false, fmt.Sprintf("command %q was not found (not a shell builtin and not in PATH)", binary)
	}

	if shellMetacharacters.MatchString(trimmed) {
		return true, ""
	}

	if pathCheckableCommands[binary] && len(fields) >= 2 {
		candidate := fields[len(fields)-1]
		if looksLikePath(candidate) {
			target := candidate
			if !filepath.IsAbs(candidate) {
				target = filepath.Join(cwd, candidate)
			}
			if _, err := os.Stat(target); os.IsNotExist(err) {
				return false, fmt.Sprintf("path %q does not exist in the workspace", candidate)
			}
		}
	}

	return true, ""
}
