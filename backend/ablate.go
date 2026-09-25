package main

import (
	"log"
	"os"
	"strings"
)

// ablated turns scaffolding off for the benchmark (bench/), so each piece
// can be measured against running without it. It's read once from
// FLINT_ABLATE, a comma-separated list, and is empty in normal use. The
// shield is deliberately not on the list: it is never switched off.
var ablated = map[string]bool{}

var ablatable = []string{"nudge", "anchor", "summaries", "fit", "preconditions"}

func loadAblations() {
	for _, name := range strings.Split(os.Getenv("FLINT_ABLATE"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		known := false
		for _, a := range ablatable {
			known = known || a == name
		}
		if !known {
			log.Fatalf("FLINT_ABLATE: unknown %q (known: %s)", name, strings.Join(ablatable, ", "))
		}
		ablated[name] = true
	}
	if len(ablated) > 0 {
		log.Printf("WARNING: benchmark ablations active, scaffolding disabled: %s", os.Getenv("FLINT_ABLATE"))
	}
}

// rawHistory is the "fit" ablation: every message as stored, nothing
// fitted, the naive unbounded context Flint exists to replace.
func rawHistory(messages []Message, attachmentsDir string) []OllamaMessage {
	lastImage, _, _ := protectedAnchors(messages)
	out := make([]OllamaMessage, 0, len(messages))
	for i, m := range messages {
		out = append(out, toOllamaMessage(m, attachmentsDir, i == lastImage))
	}
	return out
}
