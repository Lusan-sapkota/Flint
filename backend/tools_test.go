package main

import "testing"

func TestForbidsCommands(t *testing.T) {
	cases := map[string]bool{
		"Without running any commands: what port does Stockroom listen on?": true,
		"Please don’t run anything, just tell me":                           true,
		"What files are in the data folder?":                                false,
		"Run python3 check.py and tell me how many items it reports.":       false,
	}
	for text, want := range cases {
		msgs := []Message{{Role: "user", Content: text}, {Role: "assistant", Content: "ok"}}
		if got := forbidsCommands(msgs); got != want {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
	earlier := []Message{{Role: "user", Content: "Don't run commands"}, {Role: "assistant"}, {Role: "user", Content: "Now list the files"}}
	if forbidsCommands(earlier) {
		t.Error("only the latest user message should count")
	}
}

func TestAsksToRun(t *testing.T) {
	for text, want := range map[string]bool{
		"Can you run the command to check it?":           true,
		"Can you check how much free disk space I have?": true,
		"What is the capital of France?":                 false,
		"Write me a haiku about autumn.":                 false,
	} {
		if got := asksToRun([]Message{{Role: "user", Content: text}}); got != want {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}
