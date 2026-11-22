package appdevices

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// watchKey is a watch: its key pair and the public key as sent.
type watchKey struct {
	priv *ecdsa.PrivateKey
	text string
}

func newWatchKey(t *testing.T) watchKey {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return watchKey{k, base64.StdEncoding.EncodeToString(der)}
}

func (w watchKey) sign(t *testing.T, text string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(text))
	sig, err := ecdsa.SignASN1(rand.Reader, w.priv, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// pairedPhone makes a manager with one paired phone app of zeus.
func pairedPhone(t *testing.T) (*Manager, auth.User, accounts, Device, *time.Time) {
	t.Helper()
	cfg := &memConfig{files: map[string][]byte{}}
	user := auth.User{Name: "zeus", Created: time.Unix(1, 0).UTC(), PasswordHash: "h1"}
	users := accounts{"zeus": user}
	m := New(cfg, users, func() int { return 0 })
	now := time.Now()
	m.SetClockForTest(func() time.Time { return now })
	p, _ := m.Start(user)
	if _, err := m.Claim(p.Code, "Pixel", "titanos", key(t), ""); err != nil {
		t.Fatal(err)
	}
	phone, err := m.Confirm(user, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m, user, users, phone, &now
}

const servedSPKI = "c2VydmVkLXNwa2ktb2YtdGhlLW5hcy0zMi1ieXRlcyE"

func TestWatchCodeIsStable(t *testing.T) {
	// The app computes the same text; this pins the format.
	if got := WatchCode("id", "key", "nonce", "secret", "spki", false); got != WatchCode("id", "key", "nonce", "secret", "spki", false) || len(got) != 6 {
		t.Fatalf("code %q", got)
	}
	if WatchCode("id", "key", "nonce", "secret", "spki", false) == WatchCode("id", "key", "nonce", "secret", "spki", true) {
		t.Fatal("the public flag must count")
	}
	if WatchCommit("k", "s") == WatchCommit("k", "t") {
		t.Fatal("commit")
	}
	for in, want := range map[string]string{"482 913": "482913", "482-913": "482913", "48291": "", "4829133": "", "48a913": ""} {
		if got := normalWatchCode(in); got != want {
			t.Fatalf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestWatchPairing(t *testing.T) {
	m, user, _, phone, now := pairedPhone(t)
	w := newWatchKey(t)
	secret := randomText(24)
	p, err := m.WatchStart("Pixel Watch", w.text, WatchCommit(w.text, secret), "10.0.0.7")
	if err != nil || p.Nonce == "" || p.State != WatchStarted || !p.Expires.Equal(now.Add(WatchTTL)) {
		t.Fatalf("start %+v %v", p, err)
	}
	// Nothing to confirm before the watch revealed its secret.
	if st, err := m.WatchWait(p.ID, w.sign(t, watchWaitText+p.ID)); err != nil || st.State != WatchStarted {
		t.Fatalf("wait before reveal %+v %v", st, err)
	}
	r, err := m.WatchReveal(p.ID, secret, servedSPKI, false, []string{servedSPKI})
	if err != nil || r.State != WatchRevealed {
		t.Fatalf("reveal %+v %v", r, err)
	}
	code := WatchCode(p.ID, w.text, p.Nonce, secret, servedSPKI, false)
	// Revealing again does nothing.
	if _, err := m.WatchReveal(p.ID, secret, servedSPKI, false, nil); !errors.Is(err, ErrBadCode) {
		t.Fatalf("second reveal %v", err)
	}
	// A wrong signature on wait tells nothing.
	other := newWatchKey(t)
	if _, err := m.WatchWait(p.ID, other.sign(t, watchWaitText+p.ID)); !errors.Is(err, ErrSignature) {
		t.Fatalf("wait with another key %v", err)
	}
	// A wrong code is refused and does not end the pairing.
	wrong := "000000"
	if wrong == code {
		wrong = "000001"
	}
	if _, err := m.WatchConfirm(user, phone.ID, wrong); !errors.Is(err, ErrBadCode) {
		t.Fatalf("wrong code %v", err)
	}
	d, err := m.WatchConfirm(user, phone.ID, code[:3]+" "+code[3:])
	if err != nil || d.App != WatchApp || d.Name != "Pixel Watch" || d.Key != w.text {
		t.Fatalf("confirm %+v %v", d, err)
	}
	if list, _ := m.List(user); len(list) != 2 || list[1].ID != d.ID {
		t.Fatalf("list %+v", list)
	}
	// The code holds once.
	if _, err := m.WatchConfirm(user, phone.ID, code); !errors.Is(err, ErrBadCode) {
		t.Fatalf("code twice %v", err)
	}
	st, err := m.WatchWait(p.ID, w.sign(t, watchWaitText+p.ID))
	if err != nil || st.State != Confirmed || st.User != "zeus" || st.Device != d.ID {
		t.Fatalf("wait %+v %v", st, err)
	}
	// Told once.
	if _, err := m.WatchWait(p.ID, w.sign(t, watchWaitText+p.ID)); !errors.Is(err, ErrBadCode) {
		t.Fatalf("wait twice %v", err)
	}
	// The watch gets a session with its own key like any app.
	nonce, _ := m.Challenge("zeus", d.ID)
	if _, got, err := m.Session("zeus", d.ID, nonce, w.sign(t, sessionText+"zeus\n"+d.ID+"\n"+nonce)); err != nil || got.ID != d.ID {
		t.Fatalf("session %+v %v", got, err)
	}
	// A watch neither confirms another watch nor asks for a sibling code.
	w2 := newWatchKey(t)
	s2 := randomText(24)
	p2, _ := m.WatchStart("Second", w2.text, WatchCommit(w2.text, s2), "10.0.0.8")
	m.WatchReveal(p2.ID, s2, servedSPKI, false, nil)
	if _, err := m.WatchConfirm(user, d.ID, WatchCode(p2.ID, w2.text, p2.Nonce, s2, servedSPKI, false)); !errors.Is(err, ErrWatchConfirmer) {
		t.Fatalf("watch confirms watch %v", err)
	}
	if _, err := m.Sibling(user, d.ID, "titanos", "10.0.0.7"); !errors.Is(err, ErrInvalidApp) {
		t.Fatalf("sibling of a watch %v", err)
	}
	if _, err := m.Sibling(user, phone.ID, WatchApp, "10.0.0.7"); !errors.Is(err, ErrInvalidApp) {
		t.Fatalf("sibling code for a watch %v", err)
	}
	if _, err := m.WatchConfirm(user, "nope", "123456"); !errors.Is(err, ErrWatchConfirmer) {
		t.Fatalf("unknown confirmer %v", err)
	}
}

func TestWatchRefusals(t *testing.T) {
	m, user, users, phone, now := pairedPhone(t)
	w := newWatchKey(t)

	// Start: name, key and commitment.
	if _, err := m.WatchStart("", w.text, WatchCommit(w.text, "s"), "a"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("name %v", err)
	}
	if _, err := m.WatchStart("W", "nokey", WatchCommit(w.text, "s"), "a"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("key %v", err)
	}
	if _, err := m.WatchStart("W", w.text, "short", "a"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("commit %v", err)
	}

	// A secret that does not match the commitment ends the pairing.
	p, _ := m.WatchStart("W", w.text, WatchCommit(w.text, "right"), "a")
	if _, err := m.WatchReveal(p.ID, "wrong", servedSPKI, false, nil); !errors.Is(err, ErrBadCode) {
		t.Fatalf("wrong secret %v", err)
	}
	if _, err := m.WatchReveal(p.ID, "right", servedSPKI, false, nil); !errors.Is(err, ErrBadCode) {
		t.Fatalf("after a wrong secret %v", err)
	}

	// Swapped key: someone in between who starts with his own key cannot
	// keep the watch's commitment.
	attacker := newWatchKey(t)
	p, _ = m.WatchStart("W", attacker.text, WatchCommit(w.text, "s"), "a")
	if _, err := m.WatchReveal(p.ID, "s", servedSPKI, false, nil); !errors.Is(err, ErrBadCode) {
		t.Fatalf("swapped key %v", err)
	}

	// Another certificate than the NAS serves: someone in between.
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "s"), "a")
	if _, err := m.WatchReveal(p.ID, "s", "b3RoZXI", false, []string{servedSPKI}); !errors.Is(err, ErrWatchCertificate) {
		t.Fatalf("other certificate %v", err)
	}
	// A public certificate the watch checked itself counts, and so does
	// a NAS that cannot tell what it serves.
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "s"), "a")
	if _, err := m.WatchReveal(p.ID, "s", "b3RoZXI", true, []string{servedSPKI}); err != nil {
		t.Fatalf("public certificate %v", err)
	}
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "s"), "b")
	if _, err := m.WatchReveal(p.ID, "s", "b3RoZXI", false, nil); err != nil {
		t.Fatalf("nothing known %v", err)
	}

	// The digits differ when the certificate the watch saw differs from
	// what reached the NAS (someone changed the field on the way).
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "x"), "c")
	m.WatchReveal(p.ID, "x", servedSPKI, false, nil)
	seen := WatchCode(p.ID, w.text, p.Nonce, "x", "YXR0YWNrZXI", false)
	if seen == WatchCode(p.ID, w.text, p.Nonce, "x", servedSPKI, false) {
		t.Fatal("certificate does not count in the code")
	}
	if _, err := m.WatchConfirm(user, phone.ID, seen); !errors.Is(err, ErrBadCode) {
		t.Fatalf("changed certificate %v", err)
	}

	// Limits: per address and for the whole NAS.
	m2 := New(&memConfig{files: map[string][]byte{}}, users, func() int { return 0 })
	for i := 0; i < MaxWatchPerAddress; i++ {
		if _, err := m2.WatchStart("W", w.text, WatchCommit(w.text, "s"), "same"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m2.WatchStart("W", w.text, WatchCommit(w.text, "s"), "same"); !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("per address %v", err)
	}
	for i := 0; len(m2.watches) < MaxWatchPending; i++ {
		m2.WatchStart("W", w.text, WatchCommit(w.text, "s"), "addr"+string(rune('a'+i)))
	}
	if _, err := m2.WatchStart("W", w.text, WatchCommit(w.text, "s"), "fresh"); !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("whole NAS %v", err)
	}

	// Expiry: the code is gone after WatchTTL.
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "late"), "d")
	m.WatchReveal(p.ID, "late", servedSPKI, false, nil)
	code := WatchCode(p.ID, w.text, p.Nonce, "late", servedSPKI, false)
	*now = now.Add(WatchTTL + time.Second)
	if _, err := m.WatchConfirm(user, phone.ID, code); !errors.Is(err, ErrBadCode) {
		t.Fatalf("expired %v", err)
	}
	if _, err := m.WatchWait(p.ID, w.sign(t, watchWaitText+p.ID)); !errors.Is(err, ErrBadCode) {
		t.Fatalf("expired wait %v", err)
	}

	// A changed password ends the phone, and with it its right to confirm.
	p, _ = m.WatchStart("W", w.text, WatchCommit(w.text, "pw"), "e")
	m.WatchReveal(p.ID, "pw", servedSPKI, false, nil)
	changed := auth.User{Name: "zeus", Created: user.Created, PasswordHash: "h2"}
	users["zeus"] = changed
	if _, err := m.WatchConfirm(changed, phone.ID, WatchCode(p.ID, w.text, p.Nonce, "pw", servedSPKI, false)); !errors.Is(err, ErrWatchConfirmer) {
		t.Fatalf("after password change %v", err)
	}
}
