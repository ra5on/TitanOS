package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/netip"
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

func TestIPv64AccountKeysAndLog(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	me, err := c.Me(ctx)
	if err != nil || me.Plan.Name != "Free" || me.Auth.KeyID == nil || *me.Auth.KeyID != 1 || me.Usage["vpn_traffic_gb"].Limited == nil {
		t.Fatalf("me: %+v %v", me, err)
	}
	noSecrets(t, me, "owner@example.org")
	page, err := c.Logs(ctx, 50, 0)
	if err != nil || len(page.Entries) != 1 || page.NextBefore == nil || *page.NextBefore != 20 || page.RetentionDays != 7 {
		t.Fatalf("logs: %+v %v", page, err)
	}
	page, _ = c.Logs(ctx, 50, *page.NextBefore)
	if page.NextBefore != nil || page.Entries[0].ID != 10 {
		t.Fatalf("second page: %+v", page)
	}
	if _, err := c.Logs(ctx, 501, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal("limit 501 accepted")
	}
	key, token, err := c.CreateAPIKey(ctx, "backup", 86400)
	if err != nil || token != "brand-new-full-key" || key.Name != "backup" {
		t.Fatalf("create key: %+v %q %v", key, token, err)
	}
	noSecrets(t, key, "brand-new-full-key")
	if _, _, err := c.CreateAPIKey(ctx, "x", MaxIPv64KeyLifetime+1); !errors.Is(err, ErrInvalid) {
		t.Fatal("too long a lifetime accepted")
	}
	var apiErr *APIError
	if err := c.DeleteAPIKey(ctx, 1); !errors.As(err, &apiErr) || apiErr.Code != "cannot_delete_current_key" || apiErr.Status != 409 {
		t.Fatalf("own key: %v", err)
	}
	if err := c.DeleteAPIKey(ctx, 2); err != nil {
		t.Fatal(err)
	}
}

func TestIPv64DomainsAndRecordsV2(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	d, err := c.Domain(ctx, "nas.ipv64.de")
	if err != nil || len(d.Records) != 1 || d.Records[0].Token == "" {
		t.Fatalf("domain: %+v %v", d, err)
	}
	noSecrets(t, d, "update-nas.ipv64.de")
	if _, err := c.CreateOwnDomain(ctx, "example.org"); err != nil {
		t.Fatal(err)
	}
	if !fake.Called("POST /domains") {
		t.Fatal("no POST /domains")
	}
	if _, err := c.ChangeDomain(ctx, "nas.ipv64.de", IPv64DomainChange{Ignore: ptr("bogus")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad ignore accepted")
	}
	if err := CheckIPv6Prefix("10.0.0.0/8"); !errors.Is(err, ErrInvalid) {
		t.Fatal("IPv4 as ipv6_prefix")
	}
	old, err := c.DomainUpdateToken(ctx, "nas.ipv64.de")
	if err != nil || old != "update-nas.ipv64.de" {
		t.Fatal(old, err)
	}
	pub, fresh, err := c.RotateDomainToken(ctx, "nas.ipv64.de")
	if err != nil || fresh != "rotated-nas.ipv64.de" {
		t.Fatal(fresh, err)
	}
	noSecrets(t, pub, "rotated-")
	rec := d.Records[0]
	changed, err := c.ChangeRecord(ctx, rec.Token, rec, ChangeRecordFields{Content: ptr("203.0.113.11")})
	if err != nil || changed.Content != "203.0.113.11" {
		t.Fatal(changed, err)
	}
	if _, err := c.ChangeRecord(ctx, rec.Token, rec, ChangeRecordFields{Content: ptr("not an ip")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad content accepted")
	}
	result, err := c.UpdatePrefix(ctx, "nas.ipv64.de", "photos", netip.MustParseAddr("198.51.100.1"), netip.Addr{})
	if err != nil || result.Results[0].Result != "created" {
		t.Fatal(result, err)
	}
	reset, err := c.ResetDomain(ctx, "nas.ipv64.de")
	if err != nil || len(reset.Records) != 0 {
		t.Fatal(reset, err)
	}
	if err := c.DeleteDomain(ctx, "example.org"); err != nil {
		t.Fatal(err)
	}
}

func TestIPv64HealthchecksV2(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	push, err := c.CreateCheck(ctx, HealthcheckInput{Name: ptr("backup"), PeriodSeconds: ptr(3600), GraceSeconds: ptr(600), Integrations: ptr([]int{0})})
	if err != nil || push.Type != "push" || push.Token() == "" || push.ID != HealthcheckID(push.Token()) {
		t.Fatalf("push: %+v %v", push, err)
	}
	noSecrets(t, push, push.Token(), "ipv64.net/health")
	for _, bad := range []HealthcheckInput{
		{Name: ptr("x"), Type: ptr("ping-pong")},
		{Name: ptr("x"), Type: ptr("tcp"), Target: ptr("example.org")},
		{Name: ptr("x"), Type: ptr("http"), URL: ptr("ftp://x")},
		{Name: ptr("x"), PeriodSeconds: ptr(90)},
		{Name: ptr("x"), Type: ptr("keyword"), URL: ptr("https://x.example")},
		{Name: ptr("x"), Type: ptr("icmp"), Target: ptr("a"), FailuresBeforeAlarm: ptr(61)},
	} {
		if _, err := c.CreateCheck(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v accepted", bad)
		}
	}
	for _, good := range []HealthcheckInput{
		{Name: ptr("ping"), Type: ptr("icmp"), Target: ptr("198.51.100.1"), IntervalSeconds: ptr(120)},
		{Name: ptr("ssh"), Type: ptr("tcp"), Target: ptr("nas.example.org"), Port: ptr(22)},
		{Name: ptr("web"), Type: ptr("http"), URL: ptr("https://nas.example.org"), VerifyTLS: ptr(true)},
		{Name: ptr("word"), Type: ptr("keyword"), URL: ptr("https://nas.example.org"), Keyword: ptr("TitanOS")},
	} {
		if _, err := c.CreateCheck(ctx, good); err != nil {
			t.Fatalf("%s: %v", *good.Name, err)
		}
	}
	list, err := c.Checks(ctx)
	if err != nil || len(list) != 5 {
		t.Fatal(len(list), err)
	}
	token := push.Token()
	if h, err := c.CheckAction(ctx, token, "pause"); err != nil || h.Status != "paused" {
		t.Fatal(h.Status, err)
	}
	rotated, err := c.CheckAction(ctx, token, "rotate-token")
	if err != nil || rotated.Token() == token {
		t.Fatal("token not rotated", err)
	}
	token = rotated.Token()
	if _, err := c.CheckAction(ctx, token, "explode"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown action")
	}
	detail, err := c.Check(ctx, token)
	if err != nil || len(detail.Recent) != 1 {
		t.Fatal(detail, err)
	}
	events, err := c.Events(ctx, token, 50, 0)
	if err != nil || len(events.Events) != 1 || events.Events[0].Status != "down" {
		t.Fatal(events, err)
	}
	if inc, err := c.Incidents(ctx, token, 20); err != nil || len(inc) != 1 || inc[0].Status != "closed" {
		t.Fatal(inc, err)
	}
	for _, rng := range IPv64StatsRanges {
		stats, err := c.Stats(ctx, token, rng)
		if err != nil || stats.Range != rng || len(stats.Series) != 2 || stats.AvailabilityPercent == nil {
			t.Fatal(rng, stats, err)
		}
	}
	if _, err := c.Stats(ctx, token, "1y"); !errors.Is(err, ErrInvalid) {
		t.Fatal("range 1y accepted")
	}
	if _, err := c.ChangeCheck(ctx, token, HealthcheckInput{Name: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
}

func TestIPv64IntegrationsNeverWithSecrets(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	if _, err := c.CreateIntegration(ctx, IntegrationInput{Type: "telegram", Name: "phone"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("telegram without fields accepted")
	}
	if _, err := c.CreateIntegration(ctx, IntegrationInput{Type: "gotify", Name: "g", ServerURL: "https://g.example", AppToken: "a", Priority: ptr(11)}); !errors.Is(err, ErrInvalid) {
		t.Fatal("gotify priority 11 accepted")
	}
	if _, err := c.CreateIntegration(ctx, IntegrationInput{Type: "sms", Name: "s", Number: "0171", CountryCode: "0043"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("sms outside Germany")
	}
	i, err := c.CreateIntegration(ctx, IntegrationInput{Type: "telegram", Name: "phone", ChatID: "42", APIToken: "bot-secret"})
	if err != nil {
		t.Fatal(err)
	}
	noSecrets(t, i, "bot-secret", "1234…abcd")
	list, err := c.FullIntegrations(ctx)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	noSecrets(t, list, "1234…abcd")
	if _, err := c.ChangeIntegration(ctx, i.ID, ptr("renamed"), ptr(true)); err != nil {
		t.Fatal(err)
	}
	if err := c.TestIntegration(ctx, i.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteIntegration(ctx, i.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIPv64FailoverPagesV2(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	check, err := c.CreateCheck(ctx, HealthcheckInput{Name: ptr("nas"), PeriodSeconds: ptr(1800), GraceSeconds: ptr(600)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreatePolicy(ctx, PolicyInput{Mode: ptr("sometimes")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad mode")
	}
	p, err := c.CreatePolicy(ctx, PolicyInput{Name: ptr("nas"), Mode: ptr("auto_failover_and_fallback"), Linking: ptr("or")})
	if err != nil {
		t.Fatal(err)
	}
	p, err = c.AddMonitor(ctx, p.ID, check.Token(), true)
	if err != nil || len(p.Monitors) != 1 || p.Monitors[0].Healthcheck != check.ID || !p.Monitors[0].Negate {
		t.Fatalf("monitor: %+v %v", p.Monitors, err)
	}
	noSecrets(t, p, check.Token())
	page, err := c.CreateOfflinePage(ctx, OfflinePageInput{Name: ptr("Wartung"), Domain: ptr("nas.ipv64.de"), Phone: ptr("+49301234")})
	if err != nil || page.PreviewURL == "" {
		t.Fatal(page, err)
	}
	if _, err := c.CreateOfflinePage(ctx, OfflinePageInput{Phone: ptr("call me")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad phone")
	}
	for _, bad := range []RuleInput{{Record: "rec1"}, {Record: "rec1", TargetIP: "1.2.3.4", OfflinePage: "default"}, {Record: "rec1", TargetIP: "nope"}} {
		if _, err := c.AddRule(ctx, p.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v accepted", bad)
		}
	}
	d, _ := c.Domain(ctx, "nas.ipv64.de")
	if p, err = c.AddRule(ctx, p.ID, RuleInput{Record: d.Records[0].Token, OfflinePage: page.ID}); err != nil || len(p.Rules) != 1 {
		t.Fatal(err)
	}
	if p, err = c.PolicyAction(ctx, p.ID, "failover"); err != nil || p.Status != "failover_requested" {
		t.Fatal(p.Status, err)
	}
	// An icon: PNG, small enough; then one too large.
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	if page, err = c.UploadIcon(ctx, page.ID, buf.Bytes()); err != nil || page.IconURL == nil {
		t.Fatal(page, err)
	}
	buf.Reset()
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1025, 4)))
	if _, err := c.UploadIcon(ctx, page.ID, buf.Bytes()); !errors.Is(err, ErrInvalid) {
		t.Fatal("1025 pixels wide accepted")
	}
	if _, err := CheckIcon([]byte("GIF89a")); !errors.Is(err, ErrInvalid) {
		t.Fatal("GIF accepted")
	}
	if _, err := c.CreateStatusPage(ctx, StatusPageInput{Slug: ptr("no")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("short slug")
	}
	sp, err := c.CreateStatusPage(ctx, StatusPageInput{Name: ptr("NAS"), Slug: ptr("meinnas"), Password: ptr("pw"), Design: ptr(2), Healthchecks: ptr([]string{check.Token()})})
	if err != nil || !sp.PasswordProtected || sp.Healthchecks[0] != check.ID || sp.URL != "https://ipv64.net/status/meinnas" {
		t.Fatalf("status page: %+v %v", sp, err)
	}
	noSecrets(t, sp, check.Token(), `"pw"`)
	if _, err := c.ChangeStatusPage(ctx, sp.ID, StatusPageInput{Design: ptr(5)}); !errors.Is(err, ErrInvalid) {
		t.Fatal("design 5")
	}
}

func TestIPv64CDNAndRouterV2(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()
	cdn, err := c.CreateCDN(ctx, "nas.ipv64.de")
	if err != nil || cdn.ID == "" {
		t.Fatal(cdn, err)
	}
	if _, err := c.ChangeCDN(ctx, cdn.ID, CDNInput{Frontend: ptr("ftp")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad frontend")
	}
	if _, err := c.CreateMapping(ctx, cdn.ID, MappingInput{Record: "r", TargetPort: 22, ListenPort: 40000}); !errors.Is(err, ErrInvalid) {
		t.Fatal("listen port below 50000")
	}
	records, err := c.CDNRecords(ctx, cdn.ID)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	c.CDN(ctx, cdn.ID) // server assigned
	if _, err := c.CDNRecord(ctx, cdn.ID, records[0].Token, "half"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad mode")
	}
	if r, err := c.CDNRecord(ctx, cdn.ID, records[0].Token, "full"); err != nil || r.State != "cdn_proxied" {
		t.Fatal(r, err)
	}
	if _, status, err := c.CDNAction(ctx, cdn.ID, "proxy/enable"); err != nil || status != http.StatusAccepted {
		t.Fatal(status, err)
	}
	if _, _, err := c.CDNAction(ctx, cdn.ID, "self-destruct"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad action")
	}
	detail, err := c.ChangeCDN(ctx, cdn.ID, CDNInput{Frontend: ptr("https_redirect"), BackendPort: ptr(2283), Websocket: ptr(true)})
	if err != nil || detail.Proxy.Backend.Port != 2283 {
		t.Fatal(detail.Proxy, err)
	}
	m, err := c.CreateMapping(ctx, cdn.ID, MappingInput{Record: records[0].Token, TargetPort: 25565, Transport: "tcp"})
	if err != nil || m.ListenPort < 50000 {
		t.Fatal(m, err)
	}
	if _, err := c.AddAccess(ctx, cdn.ID, "not-an-ip"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad address")
	}
	if _, err := c.AddAccess(ctx, cdn.ID, "198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	router, err := c.CreateRouter(ctx, "home")
	if err != nil || len(router.Gateways) != 1 {
		t.Fatal(router, err)
	}
	for _, bad := range []RouteInput{{Destination: "10.0.0.0/8", Gateway: "fd00::1", Peer: "p"}, {Destination: "nope", Gateway: "10.0.0.1", Peer: "p"}} {
		if _, err := c.CreateRoute(ctx, router.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v accepted", bad)
		}
	}
	if _, err := c.CreateRouterRule(ctx, router.ID, RouterRuleInput{Chain: "forward", Action: "accept", Source: "10.0.0.0/8", Destination: "fd00::/8"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("mixed families")
	}
	if _, err := c.CreateRouterRule(ctx, router.ID, RouterRuleInput{Chain: "forward", Action: "drop", Source: "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChangeGateway(ctx, router.Gateways[0].ID, GatewayInput{IPv4: ptr("fd00::1/64")}); !errors.Is(err, ErrInvalid) {
		t.Fatal("IPv6 as ipv4")
	}
	peer, err := c.CreatePeer(ctx, router.Gateways[0].ID, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetPeerPublicKey(ctx, peer.ID, "short"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad key")
	}
	config, err := c.PeerConfig(ctx, peer.ID)
	if err != nil || !strings.Contains(config, "[Peer]") {
		t.Fatal(config, err)
	}
	token, err := c.ListToken(ctx)
	if err != nil || token.Base == "" {
		t.Fatal(token, err)
	}
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
	full, err2 := c.FullRecords(ctx, "nas.ipv64.de")
	if err != nil || err2 != nil || len(all) != 2 || len(full) != 2 || all[0].ID != full[0].ID || full[0].Token == "" {
		t.Fatalf("records: %+v / %+v %v %v", all, full, err, err2)
	}
	noSecrets(t, all, full[0].Token)
	if _, err := c.FullRecords(ctx, "not a host"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad domain accepted")
	}
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
	if _, err := c.CreateCheck(ctx, HealthcheckInput{Name: ptr("web"), Type: ptr("http"), URL: ptr("https://nas.example.org")}); err != nil {
		t.Fatal(err)
	}
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

func TestIPv64ListsChangesAndDeletes(t *testing.T) {
	fake := ipv64fake.New(t)
	c := NewIPv64(ipv64fake.Key, "", Options{BaseURL: fake.Server.URL})
	ctx := context.Background()

	p, err := c.CreatePolicy(ctx, PolicyInput{Name: ptr("nas"), Mode: ptr("manual"), Linking: ptr("and")})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := c.Policies(ctx); err != nil || len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("policies: %+v %v", list, err)
	}
	if changed, err := c.ChangePolicy(ctx, p.ID, PolicyInput{Name: ptr("renamed")}); err != nil || changed.Name != "renamed" {
		t.Fatalf("change policy: %+v %v", changed, err)
	}
	if _, err := c.PolicyAction(ctx, p.ID, "explode"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown policy action")
	}
	if err := c.DeletePolicy(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.Policies(ctx); len(list) != 0 {
		t.Fatalf("deleted policy listed: %+v", list)
	}

	page, err := c.CreateOfflinePage(ctx, OfflinePageInput{Name: ptr("Wartung")})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := c.OfflinePages(ctx); err != nil || len(list) != 1 {
		t.Fatalf("offline pages: %+v %v", list, err)
	}
	if changed, err := c.ChangeOfflinePage(ctx, page.ID, OfflinePageInput{Name: ptr("Pause")}); err != nil || changed.Name != "Pause" {
		t.Fatalf("change offline page: %+v %v", changed, err)
	}
	if err := c.DeleteOfflinePage(ctx, page.ID); err != nil {
		t.Fatal(err)
	}

	sp, err := c.CreateStatusPage(ctx, StatusPageInput{Name: ptr("NAS"), Slug: ptr("meinnas")})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := c.StatusPages(ctx); err != nil || len(list) != 1 {
		t.Fatalf("status pages: %+v %v", list, err)
	}
	if err := c.DeleteStatusPage(ctx, sp.ID); err != nil {
		t.Fatal(err)
	}

	cdn, err := c.CreateCDN(ctx, "nas.ipv64.de")
	if err != nil {
		t.Fatal(err)
	}
	if list, err := c.CDNs(ctx); err != nil || len(list) != 1 {
		t.Fatalf("CDNs: %+v %v", list, err)
	}
	if _, err := c.CDN(ctx, "../me"); !errors.Is(err, ErrInvalid) {
		t.Fatal("path in a CDN token accepted")
	}
	if _, err := c.DeleteCDN(ctx, cdn.ID); err != nil {
		t.Fatal(err)
	}

	router, err := c.CreateRouter(ctx, "home")
	if err != nil {
		t.Fatal(err)
	}
	if list, err := c.Routers(ctx); err != nil || len(list) != 1 {
		t.Fatalf("routers: %+v %v", list, err)
	}
	if renamed, err := c.RenameRouter(ctx, router.ID, "office"); err != nil || renamed.Name != "office" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	if _, err := c.RenameRouter(ctx, router.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty name accepted")
	}
	peer, err := c.CreatePeer(ctx, router.Gateways[0].ID, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Peer(ctx, peer.ID); err != nil || got.ID != peer.ID {
		t.Fatalf("peer: %+v %v", got, err)
	}
	if err := c.DeletePeer(ctx, peer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Peer(ctx, "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatal("path in a peer token accepted")
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
