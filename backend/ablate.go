package main

import (
	"log"
	"os"
	"strings"
)

// Benchmark-only (FLINT_ABLATE); the shield is deliberately never ablatable.
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

func rawHistory(messages []Message, attachmentsDir string) []OllamaMessage {
	lastImage, _, _ := protectedAnchors(messages)
	out := make([]OllamaMessage, 0, len(messages))
	for i, m := range messages {
		out = append(out, toOllamaMessage(m, attachmentsDir, i == lastImage))
	}
	return out
}
