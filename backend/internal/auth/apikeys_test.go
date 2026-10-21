package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func newKeys(t *testing.T) (*APIKeys, *Users, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	users := NewUsers(store)
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateOwner("alex", "a long test password"); err != nil {
		t.Fatal(err)
	}
	keys := NewAPIKeys(store, users)
	if err := keys.Load(); err != nil {
		t.Fatal(err)
	}
	if err := keys.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	return keys, users, store
}

func TestAPIKeyLifecycle(t *testing.T) {
	keys, users, store := newKeys(t)
	token, info, err := keys.Create("alex", NewAPIKey{Name: "ollama", Scopes: []string{ScopeStorage, ScopeStatus}, ExpiresDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, APIKeyPrefix+info.ID+"_") || info.Expires == nil {
		t.Fatalf("token %q, info %+v", token, info)
	}
	// Scopes come back in the fixed order.
	if strings.Join(info.Scopes, ",") != "status,storage" {
		t.Fatalf("scopes: %v", info.Scopes)
	}
	data, err := store.Read("users/alex/api-keys.json")
	if err != nil || strings.Contains(string(data), token[len(APIKeyPrefix)+9:]) {
		t.Fatalf("stored secret in the clear or not stored: %v %s", err, data)
	}

	user, key, err := keys.Authenticate(token, "192.168.1.20")
	if err != nil || user.Name != "alex" || key.ID != info.ID || !key.Has(ScopeStorage) || key.Has(ScopeDocker) {
		t.Fatalf("authenticate: %v %+v %+v", err, user, key)
	}
	if list := keys.List("alex"); len(list) != 1 || list[0].LastUsed == nil || list[0].LastIP != "192.168.1.20" {
		t.Fatalf("last use not kept: %+v", list)
	}

	// The keys are read again after a restart of titand.
	again := NewAPIKeys(store, users)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := again.Authenticate(token, "10.0.0.1"); err != nil {
		t.Fatalf("after load: %v", err)
	}

	for _, wrong := range []string{"", "titanos_ak_", token + "x", token[:len(token)-1], APIKeyPrefix + "00000000_" + token[len(APIKeyPrefix)+9:]} {
		if _, _, err := keys.Authenticate(wrong, "10.0.0.1"); !errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("%q: %v", wrong, err)
		}
	}

	if err := keys.Remove("alex", info.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.Authenticate(token, "10.0.0.1"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("removed key works: %v", err)
	}
	if _, err := store.Read("users/alex/api-keys.json"); err == nil {
		t.Fatal("empty key file left behind")
	}
}

func TestAPIKeyLimits(t *testing.T) {
	keys, users, _ := newKeys(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	keys.now = func() time.Time { return now }

	token, _, err := keys.Create("alex", NewAPIKey{Name: "kurz", Scopes: []string{ScopeStatus}, ExpiresDays: 30, Networks: []string{"172.16.0.0/12", "192.168.1.5"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.Authenticate(token, "172.17.0.2"); err != nil {
		t.Fatalf("allowed network: %v", err)
	}
	if _, _, err := keys.Authenticate(token, "192.168.1.5"); err != nil {
		t.Fatalf("allowed address: %v", err)
	}
	if _, _, err := keys.Authenticate(token, "192.168.1.6"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("other network: %v", err)
	}

	now = now.AddDate(0, 0, 30)
	if _, _, err := keys.Authenticate(token, "172.17.0.2"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expired key works: %v", err)
	}
	if list := keys.List("alex"); !list[0].Expired {
		t.Fatal("expired key not marked")
	}
	now = now.AddDate(0, 0, -29)

	if err := keys.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.Authenticate(token, "172.17.0.2"); !errors.Is(err, ErrAPIOff) {
		t.Fatalf("switched off: %v", err)
	}
	keys.SetEnabled(true)

	// A key is worth no more than the account behind it.
	if _, err := users.Create("sam", "a long test password", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	samToken, _, err := keys.Create("sam", NewAPIKey{Name: "agent", Scopes: []string{ScopeStatus}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.SetRole("sam", RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.Authenticate(samToken, "10.0.0.1"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("key of a demoted account works: %v", err)
	}
	if _, _, err := keys.Create("sam", NewAPIKey{Name: "agent2", Scopes: []string{ScopeStatus}}); !errors.Is(err, ErrKeyNotAllowed) {
		t.Fatalf("user got a key: %v", err)
	}

	// An account made again under the same name does not inherit old keys.
	users.SetRole("sam", RoleAdmin)
	if err := users.Delete("sam"); err != nil {
		t.Fatal(err)
	}
	keys.Forget("sam")
	users.Create("sam", "a long test password", RoleAdmin)
	if _, _, err := keys.Authenticate(samToken, "10.0.0.1"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("key of a deleted account works for its successor: %v", err)
	}
	if keys.Count("sam") != 0 {
		t.Fatal("successor lists old keys")
	}
}

func TestAPIKeyValidation(t *testing.T) {
	keys, _, _ := newKeys(t)
	for _, req := range []NewAPIKey{
		{Name: "", Scopes: []string{ScopeStatus}},
		{Name: " vorn", Scopes: []string{ScopeStatus}},
		{Name: strings.Repeat("x", 41), Scopes: []string{ScopeStatus}},
		{Name: "a\nb", Scopes: []string{ScopeStatus}},
	} {
		if _, _, err := keys.Create("alex", req); !errors.Is(err, ErrInvalidKeyName) {
			t.Fatalf("%q: %v", req.Name, err)
		}
	}
	for _, scopes := range [][]string{nil, {"root"}, {ScopeStatus, "docker:act"}} {
		if _, _, err := keys.Create("alex", NewAPIKey{Name: "x", Scopes: scopes}); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("%v: %v", scopes, err)
		}
	}
	if _, _, err := keys.Create("alex", NewAPIKey{Name: "x", Scopes: []string{ScopeStatus}, Networks: []string{"zuhause"}}); !errors.Is(err, ErrInvalidNetwork) {
		t.Fatalf("network: %v", err)
	}
	if _, _, err := keys.Create("alex", NewAPIKey{Name: "x", Scopes: []string{ScopeStatus}, ExpiresDays: -1}); !errors.Is(err, ErrInvalidExpiry) {
		t.Fatalf("expiry: %v", err)
	}
	if _, _, err := keys.Create("alex", NewAPIKey{Name: "Agent", Scopes: []string{ScopeStatus, ScopeStatus}}); err != nil {
		t.Fatalf("doubled scope: %v", err)
	}
	if _, _, err := keys.Create("alex", NewAPIKey{Name: "agent", Scopes: []string{ScopeStatus}}); !errors.Is(err, ErrInvalidKeyName) {
		t.Fatalf("same name twice: %v", err)
	}
	for i := 1; i < MaxAPIKeys; i++ {
		if _, _, err := keys.Create("alex", NewAPIKey{Name: "k" + strings.Repeat("x", i), Scopes: []string{ScopeStatus}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := keys.Create("alex", NewAPIKey{Name: "one more", Scopes: []string{ScopeStatus}}); !errors.Is(err, ErrTooManyAPIKeys) {
		t.Fatalf("limit: %v", err)
	}
}
