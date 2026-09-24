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

func TestCleanGeneratedTitle(t *testing.T) {
	cases := map[string]string{
		"Go HTTP 404 Static Files":           "Go HTTP 404 Static Files",
		"  \"Autumn Rain Haiku.\"  ":         "Autumn Rain Haiku",
		"Title: Battery Drain\nExplanation…": "Battery Drain",
		"**Python KeyError**":                "Python KeyError",
		"   ":                                "",
	}
	for in, want := range cases {
		if got := cleanGeneratedTitle(in); got != want {
			t.Errorf("cleanGeneratedTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
