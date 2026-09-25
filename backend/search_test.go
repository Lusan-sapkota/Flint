package main

import (
	"path/filepath"
	"testing"
)

func TestSearchConversations(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, u := range []string{"alice", "bob"} {
		if _, err := createUser(db, u, u, u+"@x.io", "h"); err != nil {
			t.Fatal(err)
		}
	}
	mine, _ := createConversation(db, "c1", "alice", "m")
	theirs, _ := createConversation(db, "c2", "bob", "m")
	insertMessage(db, mine.ID, "user", "how do I reverse a linked list")
	insertMessage(db, mine.ID, "system", "folder manifest mentioning kubernetes")
	insertMessage(db, theirs.ID, "user", "reverse a string please")

	got, err := searchConversations(db, "alice", "revers")
	if err != nil || len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("prefix match should find only alice's chat, got %+v err=%v", got, err)
	}
	if got, _ := searchConversations(db, "alice", "kubernetes"); len(got) != 0 {
		t.Errorf("system messages must not be searched, got %+v", got)
	}
	if _, err := searchConversations(db, "alice", `"unbalanced AND (`); err != nil {
		t.Errorf("FTS syntax in the query must not error: %v", err)
	}
	renameConversation(db, "c1", "alice", "Data structures 100%")
	if got, _ := searchConversations(db, "alice", "structures 100%"); len(got) != 1 {
		t.Errorf("title match should find the chat, got %+v", got)
	}
	truncateConversation(db, "c1", 0, 0)
	if got, _ := searchConversations(db, "alice", "linked"); len(got) != 0 {
		t.Errorf("deleted messages should drop out of the index, got %+v", got)
	}
}
