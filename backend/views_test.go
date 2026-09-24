package main

import "testing"

func TestPreferredOrAll(t *testing.T) {
	all := []OllamaModelInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}}

	if got := preferredOrAll(all, []string{"c", "a"}); len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("preferred subset: got %v", got)
	}
	if got := preferredOrAll(all, nil); len(got) != 3 {
		t.Errorf("no preference should show all, got %v", got)
	}
	if got := preferredOrAll(all, []string{"uninstalled"}); len(got) != 3 {
		t.Errorf("stale preference should fall back to all, got %v", got)
	}
}
