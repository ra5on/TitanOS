package api

import (
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"testing"
	"time"
)

func TestPersonalSelectionIsPrivateTemporaryAndSingleUse(t *testing.T) {
	var store personalContextStore
	now := time.Now()
	owner := auth.User{Name: "mia", Created: now}
	id := store.put(owner, personalContext{Tool: "agent.example"}, now)
	for _, other := range []auth.User{{Name: "tom", Created: now}, {Name: "mia", Created: now.Add(time.Hour)}} {
		if _, ok := store.take(other, id, now); ok {
			t.Fatal("handed selection to a different account")
		}
	}
	if selection, ok := store.take(owner, id, now); !ok || selection.Tool != "agent.example" {
		t.Fatal("missing own selection")
	}
	if _, ok := store.take(owner, id, now); ok {
		t.Fatal("reused selection")
	}
	id = store.put(owner, personalContext{}, now)
	if _, ok := store.take(owner, id, now.Add(5*time.Minute)); ok {
		t.Fatal("handed out expired selection")
	}
}
