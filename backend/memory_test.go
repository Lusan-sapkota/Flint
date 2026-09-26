package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMemoryCommand(t *testing.T) {
	cases := []struct {
		in       string
		save, ok bool
		rest     string
	}{
		{"@memory save staging is on 9123", true, true, "staging is on 9123"},
		{"@Memory SAVE", true, true, ""},
		{"  @memory astra deadline ", false, true, "astra deadline"},
		{"@memory", false, true, ""},
		{"@memoryx hi", false, false, ""},
		{"tell me about @memory", false, false, ""},
		{"@memory saved games", false, true, "saved games"},
	}
	for _, c := range cases {
		save, rest, ok := parseMemoryCommand(c.in)
		if save != c.save || ok != c.ok || rest != c.rest {
			t.Errorf("%q: got save=%v rest=%q ok=%v", c.in, save, rest, ok)
		}
	}
}

func TestMemoriesAreScopedSearchableAndFit(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, u := range []string{"alice", "bob"} {
		createUser(db, u, u, u+"@x.io", "h")
	}
	folder := "/work/astra"
	createMemory(db, "alice", &folder, nil, "Project Astra ships on Friday the 3rd")
	createMemory(db, "alice", nil, nil, "Prefers short answers")
	createMemory(db, "bob", &folder, nil, "Bob's astra note")

	got, err := searchMemories(db, "alice", "astra deadline")
	if err != nil || len(got) != 1 || !strings.Contains(got[0].Content, "Friday") {
		t.Fatalf("any-word search should find alice's astra memory only, got %+v err=%v", got, err)
	}
	if got, _ := searchMemories(db, "alice", `"( OR`); len(got) != 0 {
		t.Errorf("punctuation-only query should match nothing, got %+v", got)
	}
	inFolder, _ := folderMemories(db, "alice", folder)
	if len(inFolder) != 1 {
		t.Errorf("folder memories should be per user and per folder, got %+v", inFolder)
	}
	if ok, _ := deleteMemory(db, got[0].ID, "bob"); ok {
		t.Error("another account must not be able to delete a memory")
	}
	if ok, _ := updateMemory(db, got[0].ID, "alice", "Project Astra moved to Monday"); !ok {
		t.Fatal("owner update failed")
	}
	if got, _ := searchMemories(db, "alice", "Friday"); len(got) != 0 {
		t.Error("the index should follow updates")
	}

	kept, left := fitMemories([]Memory{{Content: strings.Repeat("a", 400)}, {Content: strings.Repeat("b", 400)}}, 150)
	if len(kept) != 1 || left != 1 {
		t.Errorf("expected one memory kept and one left out, got %d kept %d left", len(kept), left)
	}
}

func TestChatMemoriesOutliveTheirChat(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createUser(db, "alice", "alice", "a@x.io", "h")
	createConversation(db, "c1", "alice", "m")
	chat := "c1"
	createMemory(db, "alice", nil, &chat, "from the chat")
	createMemory(db, "alice", nil, nil, "from settings")

	if got, _ := chatMemories(db, "alice", "c1"); len(got) != 1 || got[0].Content != "from the chat" {
		t.Fatalf("want only the chat's memory, got %+v", got)
	}
	deleteConversation(db, "c1", "alice")
	if all, _ := listMemories(db, "alice"); len(all) != 2 {
		t.Fatalf("deleting the chat must keep its memories, got %d", len(all))
	}
}
