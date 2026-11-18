package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// memConfig is /config in RAM, counting what reaches the stick.
type memConfig struct {
	mu      sync.Mutex
	files   map[string][]byte
	reads   atomic.Int32
	writes  atomic.Int32
	failR   atomic.Bool
	failW   atomic.Bool
	written []string
}

func (c *memConfig) Read(name string) ([]byte, error) {
	c.reads.Add(1)
	if c.failR.Load() {
		return nil, errors.New("stick away")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (c *memConfig) Write(name string, data []byte) error {
	c.writes.Add(1)
	if c.failW.Load() {
		return errors.New("stick worn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[name] = append([]byte(nil), data...)
	c.written = append(c.written, string(data))
	return nil
}

type accounts struct {
	mu    sync.Mutex
	users map[string]auth.User
}

func (a *accounts) Get(name string) (auth.User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[name]
	return u, ok
}

func (a *accounts) set(u auth.User) {
	a.mu.Lock()
	a.users[u.Name] = u
	a.mu.Unlock()
}

type memSeen struct {
	mu    sync.Mutex
	data  map[string][]byte
	saves atomic.Int32
	loads atomic.Int32
	down  atomic.Bool
}

func (s *memSeen) SaveSeen(account string, v any) error {
	s.saves.Add(1)
	if s.down.Load() {
		return errors.New("no volume")
	}
	data, _ := json.Marshal(v)
	s.mu.Lock()
	s.data[account] = data
	s.mu.Unlock()
	return nil
}

func (s *memSeen) LoadSeen(account string, v any) error {
	s.loads.Add(1)
	if s.down.Load() {
		return errors.New("no volume")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[account]
	if !ok {
		return fs.ErrNotExist
	}
	return json.Unmarshal(data, v)
}

type fixture struct {
	t       *testing.T
	m       *Manager
	cfg     *memConfig
	users   *accounts
	seen    *memSeen
	limiter *auth.Limiter
	now     time.Time
}

var created = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, cfg: &memConfig{files: map[string][]byte{}}, seen: &memSeen{data: map[string][]byte{}},
		users:   &accounts{users: map[string]auth.User{}},
		limiter: auth.NewLimiter(), now: time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)}
	f.users.set(auth.User{Name: "alex", Role: auth.RoleOwner, Created: created, PasswordHash: "h1"})
	f.users.set(auth.User{Name: "bea", Role: auth.RoleUser, Created: created, PasswordHash: "h2"})
	f.m = New(f.cfg, f.users, func() *auth.Limiter { return f.limiter }, f.seen)
	f.m.Now = func() time.Time { return f.now }
	return f
}

func (f *fixture) user(name string) auth.User {
	u, ok := f.users.Get(name)
	if !ok {
		f.t.Fatalf("no %s", name)
	}
	return u
}

func (f *fixture) create(name, device string) Created {
	f.t.Helper()
	c, err := f.m.Create(f.user(name), device)
	if err != nil {
		f.t.Fatalf("create %s: %v", device, err)
	}
	return c
}

// ask is one request of a device from an address.
func (f *fixture) ask(name, password, ip string) (Identity, error) {
	r := httptest.NewRequest("PROPFIND", "/caldav/", nil)
	r.RemoteAddr = ip + ":5000"
	if name != "" || password != "" {
		r.SetBasicAuth(name, password)
	}
	return f.m.Authenticate(r)
}

func TestPasswordShape(t *testing.T) {
	shape := regexp.MustCompile(`^[23456789abcdefghijkmnpqrstuvwxyz]{4}(-[23456789abcdefghijkmnpqrstuvwxyz]{4}){6}$`)
	seen := map[string]bool{}
	for range 200 {
		p, err := NewPassword()
		if err != nil || !shape.MatchString(p) || seen[p] {
			t.Fatalf("%q %v", p, err)
		}
		seen[p] = true
	}
	if len(alphabet) != 32 || strings.ContainsAny(alphabet, "01lo") {
		t.Fatal("alphabet")
	}
	if Normalize("K7MQ-abcd -EFGH") != "k7mqabcdefgh" {
		t.Fatal(Normalize("K7MQ-abcd -EFGH"))
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"iPhone von Alex", "Mac", strings.Repeat("ä", 64), " padded "} {
		if !ValidName(ok) {
			t.Errorf("refused %q", ok)
		}
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 65), "a\x00b", "a\nb", "a\tb", "zero\u200bwidth", "bidi\u202e", "\ufeffbom", "bad\xff"} {
		if ValidName(bad) {
			t.Errorf("took %q", bad)
		}
	}
}

func TestCreateListRevoke(t *testing.T) {
	f := newFixture(t)
	c := f.create("alex", "iPhone")
	if c.User != "alex" || !strings.HasPrefix(c.ID, "d_") || len(c.Password) != 34 {
		t.Fatalf("%+v", c)
	}
	// Stored as a hash only, bound to the account.
	stored := string(f.cfg.files[File("alex")])
	if strings.Contains(stored, c.Password) || strings.Contains(stored, Normalize(c.Password)) || !strings.Contains(stored, `"created": "2026-03-01T10:00:00Z"`) {
		t.Fatalf("stored: %s", stored)
	}
	list, err := f.m.List(f.user("alex"))
	if err != nil || len(list) != 1 || list[0].Name != "iPhone" || list[0].LastSync != nil {
		t.Fatalf("%+v %v", list, err)
	}
	if data, _ := json.Marshal(list); strings.Contains(string(data), "sha256") {
		t.Fatalf("hash listed: %s", data)
	}
	// Names are unique per account, in any case.
	if _, err := f.m.Create(f.user("alex"), "iphone"); !errors.Is(err, ErrNameTaken) {
		t.Fatal(err)
	}
	if _, err := f.m.Create(f.user("alex"), "bad\u200bname"); !errors.Is(err, ErrInvalidName) {
		t.Fatal(err)
	}
	// Upper case and missing dashes are typed on a phone: they count.
	if id, err := f.ask("alex", strings.ToUpper(strings.ReplaceAll(c.Password, "-", "")), "10.0.0.2"); err != nil || id.Device != c.ID {
		t.Fatalf("typed: %+v %v", id, err)
	}
	if err := f.m.Revoke(f.user("alex"), c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ask("alex", c.Password, "10.0.0.2"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("after revoke: %v", err)
	}
	if err := f.m.Revoke(f.user("alex"), c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// bea's devices are her own.
	if err := f.m.Revoke(f.user("bea"), c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeviceLimit(t *testing.T) {
	f := newFixture(t)
	for i := range Max {
		f.create("alex", fmt.Sprintf("Gerät %d", i))
	}
	if err := f.m.Available(f.user("alex"), "one more"); !errors.Is(err, ErrTooMany) {
		t.Fatal(err)
	}
	if _, err := f.m.Create(f.user("alex"), "one more"); !errors.Is(err, ErrTooMany) {
		t.Fatal(err)
	}
}

// The stick is read once per account and written only on a change; a
// device that asks a thousand times reads and writes nothing.
func TestNoStickAccessForRequests(t *testing.T) {
	f := newFixture(t)
	c := f.create("alex", "iPhone")
	reads, writes := f.cfg.reads.Load(), f.cfg.writes.Load()
	for i := range 1000 {
		password := c.Password
		if i%3 == 0 {
			password = "wrong-" + fmt.Sprint(i)
		}
		f.limiter = auth.NewLimiter()
		f.ask("alex", password, "10.0.0.2")
		f.ask("nobody", password, "10.0.0.3")
	}
	if f.cfg.reads.Load() != reads || f.cfg.writes.Load() != writes {
		t.Fatalf("requests read %d / wrote %d times", f.cfg.reads.Load()-reads, f.cfg.writes.Load()-writes)
	}
	// A new account password: read once more, for the new hash.
	u := f.user("alex")
	u.PasswordHash = "h1-new"
	f.users.set(u)
	f.ask("alex", c.Password, "10.0.0.2")
	f.ask("alex", c.Password, "10.0.0.2")
	if f.cfg.reads.Load() != reads+1 {
		t.Fatalf("after a password change: %d reads", f.cfg.reads.Load()-reads)
	}
}

func TestRefusalsAndCounting(t *testing.T) {
	f := newFixture(t)
	c := f.create("alex", "iPhone")
	// No credentials: 401, not counted.
	for range 20 {
		if _, err := f.ask("", "", "10.0.0.9"); !errors.Is(err, ErrNoCredentials) {
			t.Fatal(err)
		}
	}
	if f.limiter.RetryAfter("cal:10.0.0.9") > 0 {
		t.Fatal("asking without credentials was counted")
	}
	// Unknown account and wrong password: the same answer, counted.
	if _, err := f.ask("nobody", c.Password, "10.0.0.5"); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := f.ask("alex", "wrong", "10.0.0.6"); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := f.ask("alex", strings.Repeat("a", MaxPassword+1), "10.0.0.6"); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	limited := false
	for range 10 {
		if _, err := f.ask("alex", "wrong", "10.0.0.6"); err != nil {
			var l *LimitedError
			if errors.As(err, &l) && l.RetryAfter > 0 {
				limited = true
				break
			}
		}
	}
	if !limited {
		t.Fatal("wrong passwords are not slowed down")
	}
	// The keys are the calendar's own: WebDAV's are not touched.
	if f.limiter.RetryAfter("dav:10.0.0.6", "dav-user:alex") > 0 || f.limiter.RetryAfter("cal-user:alex") == 0 {
		t.Fatal("limiter keys")
	}
	// Locked: refused and counted, the device stays.
	f.limiter = auth.NewLimiter()
	u := f.user("alex")
	u.Disabled = true
	f.users.set(u)
	if _, err := f.ask("alex", c.Password, "10.0.0.2"); !errors.Is(err, ErrDenied) {
		t.Fatalf("locked: %v", err)
	}
	u.Disabled = false
	f.users.set(u)
	if _, err := f.ask("alex", c.Password, "10.0.0.2"); err != nil {
		t.Fatalf("unlocked: %v", err)
	}
	// The stick away before the first read: 503, nobody counted.
	f.cfg.failR.Store(true)
	if _, err := f.ask("bea", "x", "10.0.0.7"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stick away: %v", err)
	}
}

// A revoked iPhone keeps asking: never counted, so the account's other
// devices stay reachable; the journal hears of it once an hour.
func TestRevokedDeviceIsNotCounted(t *testing.T) {
	f := newFixture(t)
	old := f.create("alex", "altes iPhone")
	other := f.create("alex", "Mac")
	if err := f.m.Revoke(f.user("alex"), old.ID); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := f.ask("alex", old.Password, "10.0.0.2"); !errors.Is(err, ErrRevoked) {
			t.Fatal(err)
		}
	}
	if id, err := f.ask("alex", other.Password, "10.0.0.3"); err != nil || id.Name != "Mac" {
		t.Fatalf("other device: %v", err)
	}
	if len(f.m.journal) != 1 {
		t.Fatalf("journal keys: %d", len(f.m.journal))
	}
}

// Three devices, then a new account password: the old ones keep asking 30
// times, and a device made afterwards gets through at once.
func TestPasswordChangeRevokesWithoutLockingOut(t *testing.T) {
	f := newFixture(t)
	var old []Created
	for i := range 3 {
		old = append(old, f.create("alex", fmt.Sprintf("Gerät %d", i)))
	}
	u := f.user("alex")
	u.PasswordHash = "h1-new"
	f.users.set(u)
	f.m.RevokeAll("alex")
	for range 30 {
		for i, d := range old {
			if _, err := f.ask("alex", d.Password, fmt.Sprintf("10.0.0.%d", i+2)); !errors.Is(err, ErrRevoked) {
				t.Fatalf("old device: %v", err)
			}
		}
	}
	fresh := f.create("alex", "neues iPhone")
	if _, err := f.ask("alex", fresh.Password, "10.0.0.9"); err != nil {
		t.Fatalf("new device: %v", err)
	}
	var stored file
	json.Unmarshal(f.cfg.files[File("alex")], &stored)
	if len(stored.Devices) != 1 || len(stored.Revoked) != 3 {
		t.Fatalf("stored: %+v", stored)
	}
	// A failed write still revokes (in RAM).
	f.cfg.failW.Store(true)
	u.PasswordHash = "h1-newer"
	f.users.set(u)
	f.m.RevokeAll("alex")
	if _, err := f.ask("alex", fresh.Password, "10.0.0.9"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked in RAM: %v", err)
	}
}

func TestRevokedLimits(t *testing.T) {
	f := newFixture(t)
	start := f.now
	for i := range 60 {
		f.now = start.Add(time.Duration(i) * time.Hour)
		c := f.create("alex", "Gerät")
		if err := f.m.Revoke(f.user("alex"), c.ID); err != nil {
			t.Fatal(err)
		}
	}
	var stored file
	json.Unmarshal(f.cfg.files[File("alex")], &stored)
	if len(stored.Revoked) != MaxRevoked {
		t.Fatalf("%d revoked", len(stored.Revoked))
	}
	// 90 days on: they fall out with the next write.
	f.now = f.now.Add(RevokedAge + time.Hour)
	f.create("alex", "neu")
	stored = file{}
	json.Unmarshal(f.cfg.files[File("alex")], &stored)
	if len(stored.Revoked) != 0 {
		t.Fatalf("%d old revoked kept", len(stored.Revoked))
	}
}

// A new account of the same name does not read the devices of the old.
func TestEarlierAccountOfTheName(t *testing.T) {
	f := newFixture(t)
	c := f.create("bea", "iPhone")
	f.m.Forget("bea")
	f.users.set(auth.User{Name: "bea", Role: auth.RoleUser, Created: created.Add(time.Hour), PasswordHash: "h3"})
	if _, err := f.ask("bea", c.Password, "10.0.0.2"); !errors.Is(err, ErrDenied) {
		t.Fatalf("old device of the old bea: %v", err)
	}
	if list, err := f.m.List(f.user("bea")); err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}

func TestLastSynced(t *testing.T) {
	f := newFixture(t)
	c := f.create("alex", "iPhone")
	f.m.Synced("alex", c.ID, CalDAV, "10.0.0.2", false)
	if f.seen.saves.Load() != 0 {
		t.Fatal("saved without the volume open")
	}
	list, _ := f.m.List(f.user("alex"))
	if list[0].LastSync == nil || !list[0].LastSync.Equal(f.now) || list[0].LastKind != CalDAV || list[0].LastSyncBeforeRestart {
		t.Fatalf("%+v", list[0])
	}
	f.m.Synced("alex", c.ID, CardDAV, "10.0.0.2", true)
	f.now = f.now.Add(10 * time.Minute)
	f.m.Synced("alex", c.ID, CalDAV, "10.0.0.2", true)
	if f.seen.saves.Load() != 1 {
		t.Fatalf("%d saves within an hour", f.seen.saves.Load())
	}
	f.now = f.now.Add(time.Hour)
	f.m.Synced("alex", c.ID, CalDAV, "10.0.0.2", true)
	if f.seen.saves.Load() != 2 {
		t.Fatalf("%d saves after an hour", f.seen.saves.Load())
	}
	if strings.Contains(string(f.seen.data["alex"]), "10.0.0.2") {
		t.Fatal("address on the volume")
	}
	// A restart: read on the first page call.
	m := New(f.cfg, f.users, func() *auth.Limiter { return f.limiter }, f.seen)
	list, err := m.List(f.user("alex"))
	if err != nil || list[0].LastSync == nil || !list[0].LastSyncBeforeRestart || list[0].LastKind != CalDAV {
		t.Fatalf("after restart: %+v %v", list, err)
	}
	// Without the volume the page still answers.
	f.seen.down.Store(true)
	m = New(f.cfg, f.users, func() *auth.Limiter { return f.limiter }, f.seen)
	if list, err := m.List(f.user("alex")); err != nil || len(list) != 1 || list[0].LastSync != nil {
		t.Fatalf("no volume: %+v %v", list, err)
	}
}

func TestWriteChallenge(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		header string
	}{
		{ErrNoCredentials, http.StatusUnauthorized, `Basic realm="TitanOS Kalender", charset="UTF-8"`},
		{ErrDenied, http.StatusUnauthorized, `Basic realm="TitanOS Kalender", charset="UTF-8"`},
		{ErrRevoked, http.StatusUnauthorized, `Basic realm="TitanOS Kalender", charset="UTF-8"`},
		{&LimitedError{RetryAfter: 29 * time.Second}, http.StatusTooManyRequests, ""},
		{ErrUnavailable, http.StatusServiceUnavailable, ""},
	} {
		rec := httptest.NewRecorder()
		WriteChallenge(rec, c.err)
		if rec.Code != c.status || rec.Header().Get("WWW-Authenticate") != c.header {
			t.Errorf("%v: %d %q", c.err, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
		if c.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "30" {
			t.Errorf("Retry-After %q", rec.Header().Get("Retry-After"))
		}
	}
}

// Someone who knows the account name sends wrong passwords until the
// account's key is blocked: the account's devices keep syncing, the wrong
// passwords keep getting 429 without being counted (security review 1.11,
// S-2). Before, every device of the account got 429 until the stranger
// stopped.
func TestStrangerDoesNotLockOutTheDevices(t *testing.T) {
	f := newFixture(t)
	c := f.create("alex", "iPhone")
	for i := range 40 {
		// From many addresses, so that only cal-user:alex blocks.
		f.ask("alex", "wrong", fmt.Sprintf("10.1.0.%d", i))
	}
	if f.limiter.RetryAfter("cal-user:alex") == 0 {
		t.Fatal("the account key is not blocked")
	}
	if id, err := f.ask("alex", c.Password, "10.0.0.2"); err != nil || id.Name != "iPhone" {
		t.Fatalf("device locked out: %v", err)
	}
	// From the blocked address of the stranger as well.
	if _, err := f.ask("alex", c.Password, "10.1.0.39"); err != nil {
		t.Fatalf("device behind a blocked address: %v", err)
	}
	before := f.limiter.RetryAfter("cal-user:alex")
	for range 20 {
		_, err := f.ask("alex", "wrong", "10.2.0.1")
		var l *LimitedError
		if !errors.As(err, &l) {
			t.Fatalf("wrong password while blocked: %v", err)
		}
	}
	if f.limiter.RetryAfter("cal:10.2.0.1") > 0 || f.limiter.RetryAfter("cal-user:alex") > before {
		t.Fatal("refusals while blocked were counted")
	}
	// A locked account stays out while blocked.
	u := f.user("alex")
	u.Disabled = true
	f.users.set(u)
	if _, err := f.ask("alex", c.Password, "10.0.0.2"); err == nil {
		t.Fatal("locked account let in")
	}
}

// IPv6: the address counts by its /64, so a guesser cannot take a fresh
// address of the prefix for every try (as at the login).
func TestIPv6CountsByPrefix(t *testing.T) {
	f := newFixture(t)
	limited := false
	for i := range 12 {
		r := httptest.NewRequest("PROPFIND", "/caldav/", nil)
		r.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:5000", i+1)
		r.SetBasicAuth(fmt.Sprintf("nobody%d", i), "wrong")
		_, err := f.m.Authenticate(r)
		var l *LimitedError
		if errors.As(err, &l) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("fresh addresses of one /64 are not counted together")
	}
	if got := sourceKey("2001:db8:1:2::99"); got != "2001:db8:1:2::/64" {
		t.Fatalf("key %q", got)
	}
	if got := sourceKey("10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("key %q", got)
	}
}

// A name of a megabyte reaches neither the limiter nor the journal whole.
func TestLongNameIsShortened(t *testing.T) {
	long := strings.Repeat("n", 500<<10)
	if got := logName(long); len(got) > maxLogName+len("…") {
		t.Fatalf("journal name of %d bytes", len(got))
	}
	f := newFixture(t)
	if _, err := f.ask(long, "x", "10.0.0.3"); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
