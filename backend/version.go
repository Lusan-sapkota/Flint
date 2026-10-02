package main

import "runtime/debug"

// Set by release builds (-ldflags -X); a local go build in the repo still gets its commit from Go's build info.
var version, commit = "dev", ""

func buildCommit() string {
	c := commit
	if bi, ok := debug.ReadBuildInfo(); ok && c == "" {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				c = s.Value
			}
		}
	}
	return c[:min(len(c), 7)]
}
