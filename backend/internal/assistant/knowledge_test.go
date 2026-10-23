package assistant

import (
	"testing"
)

func TestKnowledgeIsolationRevisionAndVolumeLoss(t *testing.T) {
	dir := t.TempDir()
	available := true
	k := NewKnowledge(func() (string, bool) { return dir, available })
	e, err := k.Put("alex", KnowledgeEntry{Kind: "fact", Title: "name", Content: "private"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := k.List("other")
	if err != nil || len(other) != 0 {
		t.Fatalf("other account: %v %v", other, err)
	}
	if _, err = k.Put("other", e); err == nil {
		t.Fatal("edited another account's entry")
	}
	old := e
	e.Content = "updated"
	e, err = k.Put("alex", e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.Put("alex", old); err == nil {
		t.Fatal("stale update")
	}
	if err = k.Delete("alex", old.ID, old.Revision); err == nil {
		t.Fatal("stale delete")
	}
	available = false
	if _, err = k.List("alex"); err == nil {
		t.Fatal("RAM fallback")
	}
	available = true
	list, err := k.List("alex")
	if err != nil || len(list) != 1 || list[0].Content != "updated" {
		t.Fatalf("%v %v", list, err)
	}
	if err = k.Delete("alex", e.ID, e.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestDeletingChatsPreservesExplicitKnowledge(t *testing.T) {
	dir := t.TempDir()
	where := func() (string, bool) { return dir, true }
	k := NewKnowledge(where)
	if _, err := k.Put("alex", KnowledgeEntry{Kind: "fact", Title: "Keep", Content: "explicit"}); err != nil {
		t.Fatal(err)
	}
	chats := NewChats(where)
	if _, err := chats.New("alex", "Remove"); err != nil {
		t.Fatal(err)
	}
	if err := chats.ForgetChats("alex"); err != nil {
		t.Fatal(err)
	}
	list, err := k.List("alex")
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	remaining, err := chats.List("alex", "")
	if err != nil || len(remaining) != 0 {
		t.Fatal(remaining, err)
	}
}
