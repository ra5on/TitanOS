package appdevices

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type memConfig struct {
	mu     sync.Mutex
	files  map[string][]byte
	writes int
	fail   bool
}

func (c *memConfig) Read(name string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

func (c *memConfig) Write(name string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("stick away")
	}
	c.files[name] = data
	c.writes++
	return nil
}

type accounts map[string]auth.User

func (a accounts) Get(name string) (auth.User, bool) { u, ok := a[name]; return u, ok }

func key(t *testing.T) string {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return base64.StdEncoding.EncodeToString(der)
}

func TestNormalCodeAndNames(t *testing.T) {
	if normalCode(" k7q4-9xmt ") != "K7Q49XMT" || normalCode("abcDEF-ghijklmnopqrstuvwxyz012") != "abcDEF-ghijklmnopqrstuvwxyz012" {
		t.Fatal("normalCode")
	}
	for _, bad := range []string{"", " x", strings.Repeat("ä", 65), "a\u200bb", "a\nb"} {
		if ValidName(bad) {
			t.Fatalf("name %q accepted", bad)
		}
	}
	if !ValidName("Pixel 10 Pro XL von Zeus") {
		t.Fatal("good name refused")
	}
	if _, err := ParseKey("AAAA"); err == nil {
		t.Fatal("bad key accepted")
	}
	rsaLike := base64.StdEncoding.EncodeToString([]byte("not der"))
	if _, err := ParseKey(rsaLike); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestPairingWritesOnlyOnConfirm(t *testing.T) {
	cfg := &memConfig{files: map[string][]byte{}}
	user := auth.User{Name: "zeus", Created: time.Unix(1, 0).UTC(), PasswordHash: "h1"}
	m := New(cfg, accounts{"zeus": user}, func() int { return 0 })
	p, _ := m.Start(user)
	if _, err := m.Claim(p.Manual, "Pixel", "titanos", key(t)); err != nil {
		t.Fatal(err)
	}
	if cfg.writes != 0 {
		t.Fatal("written before confirm")
	}
	d, err := m.Confirm(user, p.ID)
	if err != nil || cfg.writes != 1 || d.Name != "Pixel" {
		t.Fatalf("confirm: %v %d", err, cfg.writes)
	}
	// A failed write leaves no device behind.
	cfg.fail = true
	p2, _ := m.Start(user)
	m.Claim(p2.Code, "Tablet", "agent", key(t))
	if _, err := m.Confirm(user, p2.ID); err == nil {
		t.Fatal("confirmed without writing")
	}
	if list, _ := m.List(user); len(list) != 1 {
		t.Fatalf("list %v", list)
	}
}

func TestOpenPairingsAndExpiry(t *testing.T) {
	cfg := &memConfig{files: map[string][]byte{}}
	user := auth.User{Name: "zeus", Created: time.Unix(1, 0).UTC()}
	m := New(cfg, accounts{"zeus": user}, func() int { return 0 })
	now := time.Now()
	m.SetClockForTest(func() time.Time { return now })
	for range MaxPending {
		if _, err := m.Start(user); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(user); !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("fourth: %v", err)
	}
	p, _ := m.Start(auth.User{Name: "other"})
	now = now.Add(PairTTL + time.Second)
	if _, err := m.Claim(p.Code, "Pixel", "titanos", key(t)); !errors.Is(err, ErrBadCode) {
		t.Fatalf("expired code: %v", err)
	}
	if _, err := m.Start(user); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestCredentialsEndDevices(t *testing.T) {
	cfg := &memConfig{files: map[string][]byte{}}
	user := auth.User{Name: "zeus", Created: time.Unix(1, 0).UTC(), PasswordHash: "h1"}
	users := accounts{"zeus": user}
	m := New(cfg, users, func() int { return auth.StayForever })
	p, _ := m.Start(user)
	m.Claim(p.Code, "Pixel", "titanos", key(t))
	m.Confirm(user, p.ID)
	// A new titand with a changed password hash reads the file: no apps.
	changed := user
	changed.PasswordHash = "h2"
	fresh := New(cfg, accounts{"zeus": changed}, func() int { return auth.StayForever })
	if list, _ := fresh.List(changed); len(list) != 0 {
		t.Fatalf("apps of other credentials: %v", list)
	}
	if list, _ := m.List(user); len(list) != 1 || m.Expires(list[0]) != nil {
		t.Fatalf("no end: %v", list)
	}
}
