package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestCanonicalAccountHashMatchesLegacyWire(t *testing.T) {
	// The known version-1 JSON for this minimal account, including its zeroed
	// Linux UID, is independent of newer account or cluster implementations.
	raw := []byte(`{"name":"bea","role":"admin","passwordHash":"fixture","created":"2025-01-02T03:04:05Z","uid":0}`)
	var user auth.User
	if err := json.Unmarshal(raw, &user); err != nil {
		t.Fatal(err)
	}
	user.UID, user.AccessRevision, user.LocalAccessStamp = 1234, 9, "node-local"
	digest := sha256.Sum256(raw)
	if accountHash(user) != hex.EncodeToString(digest[:]) {
		t.Fatal("grant metadata changed the legacy account hash")
	}
}

// Deliver two records whose final role, password and canonical hash are identical,
// through the real TLS cluster sync, including a node that missed the intermediate
// states. Then model a former peer which strips the newer fields from its records.
func TestClusterReplicatesRegrantWithSameAccountHash(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "test-password-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bea": "test-password-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	if err := b.m.Join(t.Context(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	initial := b.m.versions()["alex"]
	u, _ := a.users.Get("alex")
	// Import has no asynchronous replication callback. This models a rights
	// cycle completed before the cluster's next reconciliation.
	u.AccessRevision += 2
	if _, _, err := a.users.Import(u); err != nil {
		t.Fatal(err)
	}
	a.m.syncAccounts(t.Context())
	got, _ := b.users.Get("alex")
	v := b.m.versions()["alex"]
	if v.Hash != initial.Hash || !v.newer(initial) || v.GrantRevision != "2" || got.AccessRevision != 2 {
		t.Fatal("peer missed the authority change", initial, v, got.AccessRevision)
	}
	before := got.LocalAccessStamp
	a.m.syncAccounts(t.Context())
	if after, _ := b.users.Get("alex"); after.LocalAccessStamp != before || b.m.versions()["alex"] != v {
		t.Fatal("repeated synchronization changed authority")
	}

	rec, ok := a.m.record("alex")
	if !ok || rec.User.LocalAccessStamp != "" {
		t.Fatal("local authority escaped in the wire record")
	}
	rec.Version.Time++
	rec.Version.GrantRevision, rec.User.AccessRevision = "", 0
	if err := b.m.takeAccounts([]accountRecord{rec}); err != nil {
		t.Fatal(err)
	}
	legacy, _ := b.users.Get("alex")
	if legacy.AccessRevision != 0 || legacy.LocalAccessStamp == before || legacy.LocalAccessStamp == "" {
		t.Fatal("legacy record failed to expire the previous local consent")
	}
	next := rec
	next.Version.Time = max(time.Now().UnixNano(), rec.Version.Time+1)
	if err := b.m.takeAccounts([]accountRecord{next, next}); err != nil {
		t.Fatal(err)
	}
	changed, _ := b.users.Get("alex")
	if changed.LocalAccessStamp == legacy.LocalAccessStamp || accountHash(changed) != rec.Version.Hash {
		t.Fatal("newer legacy assertion with identical role failed to expire consent")
	}
	if err := b.users.Load(); err != nil {
		t.Fatal(err)
	}
	restarted, _ := b.users.Get("alex")
	if changed.LocalAccessStamp != restarted.LocalAccessStamp {
		t.Fatal("grant reverted after reloading account")
	}
}
