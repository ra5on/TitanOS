package webpush

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// browser is the receiving side: its keys and what its push service got.
type browser struct {
	key    *ecdh.PrivateKey
	auth   []byte
	mu     sync.Mutex
	got    []Message
	status int
	header http.Header
}

func newBrowser(t *testing.T) *browser {
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	return &browser{key: key, auth: auth, status: http.StatusCreated}
}

// decrypt undoes encrypt the way a browser does (RFC 8291).
func (b *browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != 4096 || idlen != 65 {
		t.Fatalf("header rs=%d idlen=%d", rs, idlen)
	}
	asPublic := body[21 : 21+idlen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := b.key.ECDH(as)
	prkKey, _ := hkdf.Extract(sha256.New, shared, b.auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(b.key.PublicKey().Bytes())+string(asPublic), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatal("cannot decrypt:", err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatal("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func (b *browser) subscription(endpoint string) Subscription {
	var s Subscription
	s.Endpoint = endpoint
	s.Keys.P256dh = b64.EncodeToString(b.key.PublicKey().Bytes())
	s.Keys.Auth = b64.EncodeToString(b.auth)
	return s
}

func fixture(t *testing.T, b *browser) (*Manager, *httptest.Server, func() *Manager) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		defer b.mu.Unlock()
		b.header = r.Header.Clone()
		if b.status < 300 {
			var msg Message
			if err := json.Unmarshal(b.decrypt(t, body), &msg); err != nil {
				t.Error(err)
			}
			b.got = append(b.got, msg)
		}
		w.WriteHeader(b.status)
	}))
	t.Cleanup(server.Close)
	store := config.Open(t.TempDir())
	open := func() *Manager {
		m := New(store)
		m.client = server.Client()
		m.checkHost = func(hostport string) bool { return strings.HasPrefix(hostport, "127.0.0.1:") }
		if err := m.Load(); err != nil {
			t.Fatal(err)
		}
		return m
	}
	return open(), server, open
}

// verifyVAPID checks the Authorization header against the public key.
func verifyVAPID(t *testing.T, header, audience string) {
	t.Helper()
	token, key, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !ok {
		t.Fatalf("header %q", header)
	}
	raw, _ := b64.DecodeString(key)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	var claims struct {
		Aud string
		Exp int64
		Sub string
	}
	data, _ := b64.DecodeString(parts[1])
	json.Unmarshal(data, &claims)
	if claims.Aud != audience || claims.Sub == "" || claims.Exp < time.Now().Unix() || claims.Exp > time.Now().Add(24*time.Hour).Unix() {
		t.Fatalf("claims %+v", claims)
	}
	sig, _ := b64.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("VAPID signature does not verify")
	}
}

func TestSubscribeSendAndExpire(t *testing.T) {
	b := newBrowser(t)
	m, server, reopen := fixture(t, b)
	public, err := m.PublicKey()
	if err != nil || len(public) < 80 {
		t.Fatalf("public key %q %v", public, err)
	}
	d, err := m.Subscribe("alex", "iPhone", b.subscription(server.URL+"/push/abc"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Devices("alex"); len(got) != 1 || got[0].Name != "iPhone" || got[0].ID != d.ID {
		t.Fatalf("devices %+v", got)
	}
	if len(m.Devices("other")) != 0 {
		t.Fatal("devices of another account")
	}
	// The key and the device survive a restart.
	m = reopen()
	if again, _ := m.PublicKey(); again != public {
		t.Fatal("key changed after a restart")
	}
	if err := m.Test(context.Background(), "alex", d.ID, Message{Title: "Test", Body: "Hallo", Urgent: true}); err != nil {
		t.Fatal(err)
	}
	if len(b.got) != 1 || b.got[0].Title != "Test" || b.got[0].Body != "Hallo" {
		t.Fatalf("got %+v", b.got)
	}
	if b.header.Get("Content-Encoding") != "aes128gcm" || b.header.Get("Urgency") != "high" || b.header.Get("TTL") == "" {
		t.Fatalf("headers %v", b.header)
	}
	u, _ := url.Parse(server.URL)
	verifyVAPID(t, b.header.Get("Authorization"), "https://"+u.Host)
	// Another account cannot test or remove the device.
	if err := m.Test(context.Background(), "other", d.ID, Message{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("test through another account")
	}
	if err := m.Remove("other", d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed by another account")
	}
	// An expired subscription is dropped.
	b.status = http.StatusGone
	if err := m.Test(context.Background(), "alex", d.ID, Message{Title: "x"}); !errors.Is(err, ErrGone) || len(m.Devices("alex")) != 0 {
		t.Fatalf("expired subscription kept: %v", err)
	}
}

func TestBroadcastOnlyToAllowedAccounts(t *testing.T) {
	b := newBrowser(t)
	m, server, _ := fixture(t, b)
	m.Subscribe("alex", "a", b.subscription(server.URL+"/a"))
	m.Subscribe("bob", "b", b.subscription(server.URL+"/b"))
	m.Allowed = func(user string) bool { return user == "alex" }
	m.Broadcast(Message{Title: "Volume", Body: "degradiert"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.got)
		b.mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.got) != 1 {
		t.Fatalf("sent %d, want only to alex", len(b.got))
	}
}

// PLAN 58: an account with the role "Benutzer" gets its own messages
// (SendTo, the agent's) but none of the NAS (Broadcast).
func TestPersonalMessagesForEveryAccount(t *testing.T) {
	b := newBrowser(t)
	m, server, _ := fixture(t, b)
	m.Subscribe("bob", "b", b.subscription(server.URL+"/b"))
	m.Allowed = func(user string) bool { return user == "alex" }
	m.Personal = func(user string) bool { return user != "gesperrt" }
	m.Broadcast(Message{Title: "Volume", Body: "degradiert"})
	m.SendTo("bob", Message{Title: "KI-Agent", Body: "fertig"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.got)
		b.mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.got) != 1 {
		t.Fatalf("sent %d, want only the agent's message to bob", len(b.got))
	}
}

// PLAN 59: each device chooses messages of TitanOS and of the agent.
func TestDeviceKinds(t *testing.T) {
	b := newBrowser(t)
	m, server, _ := fixture(t, b)
	d, _ := m.Subscribe("alex", "a", b.subscription(server.URL+"/a"))
	if _, err := m.SetKinds("bob", d.ID, map[string]bool{KindNAS: false, KindAgent: true}); !errors.Is(err, ErrNotFound) {
		t.Fatal("changed another account's device")
	}
	if got, err := m.SetKinds("alex", d.ID, map[string]bool{KindNAS: false, KindAgent: true}); err != nil || len(got.Off) != 1 {
		t.Fatalf("kinds %+v %v", got, err)
	}
	m.Broadcast(Message{Title: "NAS"})
	time.Sleep(200 * time.Millisecond)
	b.mu.Lock()
	n := len(b.got)
	b.mu.Unlock()
	if n != 0 {
		t.Fatal("NAS message to a device that switched it off")
	}
	m.SetKinds("alex", d.ID, map[string]bool{KindNAS: true, KindAgent: false})
	m.SendTo("alex", Message{Title: "Agent"})
	time.Sleep(200 * time.Millisecond)
	b.mu.Lock()
	n = len(b.got)
	b.mu.Unlock()
	if n != 0 {
		t.Fatal("agent message to a device that switched it off")
	}
	if devs := m.Devices("alex"); len(devs) != 1 || devs[0].Off[0] != KindAgent {
		t.Fatalf("devices %+v", devs)
	}
}

func TestEndpointsOnlyAtPushServices(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t)
	for _, endpoint := range []string{
		"http://web.push.apple.com/x", "https://evil.example.org/x", "https://192.168.1.1/x",
		"https://web.push.apple.com.evil.org/x", "https://user@fcm.googleapis.com/x", "https://fcm.googleapis.com:8443/x",
	} {
		if _, err := m.Subscribe("alex", "", b.subscription(endpoint)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, endpoint := range []string{"https://web.push.apple.com/QG", "https://fcm.googleapis.com/fcm/send/x", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://wns2-db5p.notify.windows.com/w/?token=x"} {
		if _, err := m.Subscribe("alex", "", b.subscription(endpoint)); err != nil {
			t.Fatalf("refused %s: %v", endpoint, err)
		}
	}
	bad := b.subscription("https://web.push.apple.com/y")
	bad.Keys.Auth = "short"
	if _, err := m.Subscribe("alex", "", bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad auth secret accepted")
	}
	bad = b.subscription("https://web.push.apple.com/y")
	bad.Keys.P256dh = b64.EncodeToString(make([]byte, 65))
	if _, err := m.Subscribe("alex", "", bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad public key accepted")
	}
	if _, err := m.Subscribe("alex", "x\nInjected", b.subscription("https://web.push.apple.com/z")); !errors.Is(err, ErrInvalid) {
		t.Fatal("control characters in the name accepted")
	}
}

func TestDeviceLimit(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	m.Load()
	b := newBrowser(t)
	for i := range maxPerUser {
		if _, err := m.Subscribe("alex", "", b.subscription("https://web.push.apple.com/"+string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Subscribe("alex", "", b.subscription("https://web.push.apple.com/zz")); !errors.Is(err, ErrTooMany) {
		t.Fatal("limit not enforced")
	}
	// Subscribing the same endpoint again renews it instead.
	if _, err := m.Subscribe("alex", "neu", b.subscription("https://web.push.apple.com/a")); err != nil {
		t.Fatal(err)
	}
}

// PLAN 82-3: a camera's message goes to the devices of the one account,
// unless the device switched camera messages off; the agent's switch does
// not touch it, an older page that sends only nas and agent keeps it, and
// a locked account gets nothing.
func TestCameraKind(t *testing.T) {
	b := newBrowser(t)
	m, server, _ := fixture(t, b)
	a, _ := m.Subscribe("alex", "a", b.subscription(server.URL+"/a"))
	m.Subscribe("bob", "b", b.subscription(server.URL+"/b"))
	m.Personal = func(user string) bool { return user != "gesperrt" }
	count := func() int {
		time.Sleep(200 * time.Millisecond)
		b.mu.Lock()
		defer b.mu.Unlock()
		n := len(b.got)
		b.got = nil
		return n
	}
	// The agent's switch off: cameras still arrive, only at alex's device.
	if _, err := m.SetKinds("alex", a.ID, map[string]bool{KindNAS: true, KindAgent: false}); err != nil {
		t.Fatal(err)
	}
	m.SendCamera("alex", Message{Title: "Person an Kamera Haustür"})
	if n := count(); n != 1 {
		t.Fatalf("sent %d, want 1 to alex's device", n)
	}
	if on, off := m.CameraDevices("alex"); on != 1 || off != 0 {
		t.Fatalf("camera devices %d/%d", on, off)
	}
	// Camera messages off: nothing; nas and agent stay as they were.
	d, err := m.SetKinds("alex", a.ID, map[string]bool{KindCamera: false})
	if err != nil || !slices.Equal(d.Off, []string{KindAgent, KindCamera}) {
		t.Fatalf("off %v %v", d.Off, err)
	}
	m.SendCamera("alex", Message{Title: "Person"})
	if n := count(); n != 0 {
		t.Fatalf("sent %d to a device that switched cameras off", n)
	}
	if on, off := m.CameraDevices("alex"); on != 0 || off != 1 {
		t.Fatalf("camera devices %d/%d", on, off)
	}
	// An older page switches nas and agent only: cameras stay off.
	if d, _ := m.SetKinds("alex", a.ID, map[string]bool{KindNAS: true, KindAgent: true}); !slices.Equal(d.Off, []string{KindCamera}) {
		t.Fatalf("an older page changed the camera switch: %v", d.Off)
	}
	// Broadcast and SendTo do not count as camera messages, nor the reverse.
	m.SendTo("alex", Message{Title: "Agent"})
	if n := count(); n != 1 {
		t.Fatalf("agent message: %d", n)
	}
	// A locked account gets nothing.
	m.Subscribe("gesperrt", "g", b.subscription(server.URL+"/g"))
	m.SendCamera("gesperrt", Message{Title: "Person"})
	if n := count(); n != 0 {
		t.Fatal("camera message to a locked account")
	}
}
