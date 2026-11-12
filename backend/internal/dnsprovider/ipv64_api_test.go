package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider/ipv64fake"
)

func ptr[T any](v T) *T { return &v }

// noSecrets fails if a value TitanOS must keep to itself shows up in what
// would go to the web UI.
func noSecrets(t *testing.T, v any, secrets ...string) {
	t.Helper()
	data, _ := json.Marshal(v)
	for _, s := range secrets {
		if strings.Contains(string(data), s) {
			t.Fatalf("secret %q in %s", s, data)
		}
	}
}

func TestIPv64Account(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	me, err := c.Me(ctx)
	if err != nil || me.Plan.Name != "Free" || me.Auth.KeyID == nil || *me.Auth.KeyID != 1 || me.Usage["vpn_traffic_gb"].Limited == nil {
		t.Fatalf("me: %+v %v", me, err)
	}
	noSecrets(t, me, "owner@example.org")
	if _, err := c.ChangeDomain(ctx, "nas.ipv64.de", IPv64DomainChange{Ignore: ptr("bogus")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad ignore accepted")
	}
}

// Integrations are never read with their secrets.
func TestIPv64IntegrationsNeverWithSecrets(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	list, err := c.FullIntegrations(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	noSecrets(t, list, "1234…abcd")
}

func TestIPv64TokenBucketFollowsHeaders(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newTokenBucket(300, 10)
	b.now = func() time.Time { return now }
	if b.reserve() != 0 {
		t.Fatal("full bucket waits")
	}
	h := http.Header{}
	h.Set("RateLimit-Remaining", "0")
	b.observe(h, 200)
	if wait := b.reserve(); wait <= 0 || wait > time.Second {
		t.Fatalf("empty bucket: %v", wait)
	}
	h.Set("Retry-After", "20")
	b.observe(h, http.StatusTooManyRequests)
	if wait := b.reserve(); wait < 19*time.Second {
		t.Fatalf("Retry-After not held: %v", wait)
	}
	now = now.Add(time.Minute)
	if wait := b.reserve(); wait != 0 {
		t.Fatalf("after a minute: %v", wait)
	}
	if err := b.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.hold = now.Add(2 * time.Minute)
	var apiErr *APIError
	if err := b.wait(context.Background()); !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("long hold: %v", err)
	}
}

// The smaller views the DynDNS page and the heartbeat read come from the
// same requests as the full API.
func TestIPv64ViewsOfTheFullAPI(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()

	account, err := c.Account(ctx)
	if err != nil || account.Username != "owner" || account.Plan != "Free" || account.KeyName != "titanos" {
		t.Fatalf("account: %+v %v", account, err)
	}
	if u := account.Usage["dyndns_domains"]; u.Used != 1 || u.Limit == nil || *u.Limit != 3 {
		t.Fatalf("usage: %+v", account.Usage)
	}
	noSecrets(t, account, "owner@example.org")

	integrations, err := c.Integrations(ctx)
	if err != nil || len(integrations) != 1 || integrations[0].ID != 0 || integrations[0].Type != "account_email" {
		t.Fatalf("integrations: %+v %v", integrations, err)
	}

	added, err := c.AddRecord(ctx, "nas.ipv64.de", "TXT", "_acme-challenge", "challenge")
	if err != nil || added.ID == 0 || added.Type != "TXT" || added.Prefix != "_acme-challenge" {
		t.Fatalf("add record: %+v %v", added, err)
	}
	txt, err := c.Records(ctx, "nas.ipv64.de", "TXT", "_acme-challenge")
	if err != nil || len(txt) != 1 || txt[0].Content != "challenge" {
		t.Fatalf("TXT records: %+v %v", txt, err)
	}
	all, err := c.Records(ctx, "nas.ipv64.de", "", "")
	if err != nil || len(all) != 2 || all[0].ID == 0 {
		t.Fatalf("records: %+v %v", all, err)
	}
	noSecrets(t, all, "record_token", "rec-")
	if err := c.DeleteRecord(ctx, added.ID); err != nil {
		t.Fatal(err)
	}

	push, err := c.CreateHealthcheck(ctx, "heartbeat", time.Hour, 10*time.Minute, []int{0})
	if err != nil || push.Token() == "" || push.ID != HealthcheckID(push.Token()) || push.Period != 3600 || push.Grace != 600 || len(push.Integrations) != 1 {
		t.Fatalf("create: %+v %v", push, err)
	}
	noSecrets(t, push, push.Token())
	if _, err := c.CreateHealthcheck(ctx, "x", 90*time.Second, time.Minute, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("90 seconds accepted")
	}
	// A check of another type at the account is not offered as heartbeat.
	fake.Checks["http-token"] = map[string]any{"healthcheck_token": "http-token", "name": "web", "type": "http", "status": "up"}
	checks, err := c.Healthchecks(ctx)
	if err != nil || len(checks) != 1 || checks[0].Token() != push.Token() || checks[0].Name != "heartbeat" {
		t.Fatalf("push checks only: %+v %v", checks, err)
	}
	if err := c.PauseHealthcheck(ctx, push.Token()); err != nil {
		t.Fatal(err)
	}
	if h, err := c.Healthcheck(ctx, push.Token()); err != nil || h.Status != "paused" || h.ID != push.ID {
		t.Fatalf("paused: %+v %v", h, err)
	}
	if err := c.ResumeHealthcheck(ctx, push.Token()); err != nil {
		t.Fatal(err)
	}
	if err := c.TestNotification(ctx, push.Token()); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteHealthcheck(ctx, push.Token()); err != nil {
		t.Fatal(err)
	}
	if checks, _ := c.Healthchecks(ctx); len(checks) != 0 {
		t.Fatalf("deleted check still listed: %+v", checks)
	}
}

func TestIPv64RateLimitIsPerAccount(t *testing.T) {
	a, again, b := ipv64LimiterFor("key-a"), ipv64LimiterFor("key-a"), ipv64LimiterFor("key-b")
	if a != again || a == b {
		t.Fatal("one bucket per API key expected")
	}
	// A 429 of one account does not hold back the other.
	h := http.Header{}
	h.Set("Retry-After", "30")
	a.observe(h, http.StatusTooManyRequests)
	if b.reserve() != 0 {
		t.Fatal("the other account waits")
	}
	if a.reserve() < 29*time.Second {
		t.Fatal("the limited account does not wait")
	}
}
