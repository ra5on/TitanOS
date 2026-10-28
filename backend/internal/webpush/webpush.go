// Package webpush sends TitanOS notifications to browsers and to web apps on
// the home screen (Safari on iOS from 16.4, Chrome, Firefox, Edge) over the
// Web Push protocol: RFC 8030 for delivery, RFC 8291 for the encryption of
// the message and RFC 8292 (VAPID) to name the sender. Standard library
// only.
//
// A browser subscribes with the public VAPID key of this NAS and hands over
// an endpoint at its push service plus two keys. titand keeps the
// subscriptions and its key in /config/notify/webpush.json (written only
// when a device is added or removed) and posts encrypted messages to the
// endpoints. Endpoints must be HTTPS at a known push service: the address
// comes from a browser and must never make the NAS post somewhere else.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	file = "notify/webpush.json"
	// At most this many devices per account and in all.
	maxPerUser = 20
	maxDevices = 100
	// contact is the "sub" of the VAPID token: who runs the sender.
	contact = "https://github.com/ra5on/TitanOS"
)

var (
	ErrInvalid  = errors.New("webpush: invalid subscription")
	ErrNotFound = errors.New("webpush: no such device")
	ErrTooMany  = errors.New("webpush: too many devices")
	// ErrGone means the push service no longer knows the subscription.
	ErrGone = errors.New("webpush: subscription expired")
	b64     = base64.RawURLEncoding
	// pushHosts are the push services of the browsers; an endpoint must be
	// one of them or a name under one of them.
	pushHosts = []string{"push.apple.com", "fcm.googleapis.com", "push.services.mozilla.com", "notify.windows.com"}
)

// Subscription is what PushManager.subscribe() hands out (toJSON()).
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Device is a subscribed browser as the web UI sees it, without its keys.
type Device struct {
	ID      string    `json:"id"`
	User    string    `json:"user"`
	Name    string    `json:"name"`
	Service string    `json:"service"`
	Created time.Time `json:"created"`
}

type stored struct {
	Device
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

type state struct {
	// Key is the VAPID key of this NAS (PKCS #8, base64).
	Key     string   `json:"key"`
	Devices []stored `json:"devices"`
}

// Message is what the service worker shows.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Tag replaces an older notification with the same tag.
	Tag string `json:"tag,omitempty"`
	URL string `json:"url,omitempty"`
	// Urgent asks the push service to deliver at once (errors).
	Urgent bool `json:"-"`
}

// Manager keeps the subscriptions and sends.
type Manager struct {
	store *config.Store
	// Allowed tells whether an account may still get notifications (owner
	// or administrator, not locked); nil allows all.
	Allowed func(user string) bool
	client  *http.Client
	now     func() time.Time
	// checkHost decides which endpoint hosts are taken; tests replace it.
	checkHost func(hostport string) bool

	mu     sync.Mutex
	loaded bool
	key    *ecdsa.PrivateKey
	st     state
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{store: store, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now: time.Now, checkHost: knownHost}
}

// knownHost takes the host (with port, if any) of an endpoint.
func knownHost(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	if port != "" && port != "443" {
		return false
	}
	host = strings.ToLower(host)
	return slices.ContainsFunc(pushHosts, func(h string) bool { return host == h || strings.HasSuffix(host, "."+h) })
}

// Load reads key and devices from /config.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var st state
	if err := m.store.ReadJSON(file, &st); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if st.Key != "" {
		der, err := base64.StdEncoding.DecodeString(st.Key)
		if err != nil {
			return fmt.Errorf("webpush key: %w", err)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		key, ok := parsed.(*ecdsa.PrivateKey)
		if err != nil || !ok || key.Curve != elliptic.P256() {
			return errors.New("webpush key is not a P-256 key")
		}
		m.key = key
	}
	m.st, m.loaded = st, true
	return nil
}

// ensureKey makes the VAPID key on first use; called with mu held.
func (m *Manager) ensureKey() error {
	if m.key != nil {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	next := m.st
	next.Key = base64.StdEncoding.EncodeToString(der)
	if err := m.store.WriteJSON(file, next); err != nil {
		return err
	}
	m.key, m.st = key, next
	return nil
}

func publicBytes(key *ecdsa.PrivateKey) []byte {
	pub, _ := key.PublicKey.ECDH()
	return pub.Bytes()
}

// PublicKey is the VAPID key browsers subscribe with (applicationServerKey).
func (m *Manager) PublicKey() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return "", errors.New("webpush: not loaded")
	}
	if err := m.ensureKey(); err != nil {
		return "", err
	}
	return b64.EncodeToString(publicBytes(m.key)), nil
}

// check validates a subscription and returns its decoded keys.
func (m *Manager) check(s Subscription) (*url.URL, []byte, []byte, error) {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || len(s.Endpoint) > 1024 || !m.checkHost(u.Host) {
		return nil, nil, nil, fmt.Errorf("%w: endpoint", ErrInvalid)
	}
	pub, err := b64.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: p256dh", ErrInvalid)
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: p256dh", ErrInvalid)
	}
	auth, err := b64.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if err != nil || len(auth) != 16 {
		return nil, nil, nil, fmt.Errorf("%w: auth", ErrInvalid)
	}
	return u, pub, auth, nil
}

func deviceID(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:8])
}

// Subscribe adds or renews a device of user.
func (m *Manager) Subscribe(user, name string, s Subscription) (Device, error) {
	u, pub, auth, err := m.check(s)
	if err != nil {
		return Device{}, err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 60 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return Device{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return Device{}, errors.New("webpush: not loaded")
	}
	if err := m.ensureKey(); err != nil {
		return Device{}, err
	}
	id := deviceID(s.Endpoint)
	devices := slices.DeleteFunc(slices.Clone(m.st.Devices), func(d stored) bool { return d.ID == id })
	mine := 0
	for _, d := range devices {
		if d.User == user {
			mine++
		}
	}
	if mine >= maxPerUser || len(devices) >= maxDevices {
		return Device{}, ErrTooMany
	}
	d := stored{Device: Device{ID: id, User: user, Name: name, Service: u.Hostname(), Created: m.now().UTC()},
		Endpoint: s.Endpoint, P256dh: b64.EncodeToString(pub), Auth: b64.EncodeToString(auth)}
	next := m.st
	next.Devices = append(devices, d)
	if err := m.store.WriteJSON(file, next); err != nil {
		return Device{}, err
	}
	m.st = next
	return d.Device, nil
}

// Devices lists the devices of user.
func (m *Manager) Devices(user string) []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Device{}
	for _, d := range m.st.Devices {
		if d.User == user {
			out = append(out, d.Device)
		}
	}
	return out
}

// Remove takes a device of user away.
func (m *Manager) Remove(user, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeLocked(func(d stored) bool { return d.ID == id && d.User == user })
}

func (m *Manager) removeLocked(match func(stored) bool) error {
	if !slices.ContainsFunc(m.st.Devices, match) {
		return ErrNotFound
	}
	next := m.st
	next.Devices = slices.DeleteFunc(slices.Clone(m.st.Devices), match)
	if err := m.store.WriteJSON(file, next); err != nil {
		return err
	}
	m.st = next
	return nil
}

// Test sends a message to one device of user and waits for the answer.
func (m *Manager) Test(ctx context.Context, user, id string, msg Message) error {
	m.mu.Lock()
	var target *stored
	for _, d := range m.st.Devices {
		if d.ID == id && d.User == user {
			target = &d
			break
		}
	}
	m.mu.Unlock()
	if target == nil {
		return ErrNotFound
	}
	return m.sendTo(ctx, *target, msg)
}

// Broadcast sends a message to every device whose account may still get
// notifications, in the background, one attempt each with one retry.
func (m *Manager) Broadcast(msg Message) {
	m.mu.Lock()
	devices := slices.Clone(m.st.Devices)
	m.mu.Unlock()
	for _, d := range devices {
		if m.Allowed != nil && !m.Allowed(d.User) {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := m.sendTo(ctx, d, msg)
			if err != nil && !errors.Is(err, ErrGone) {
				time.Sleep(10 * time.Second)
				err = m.sendTo(ctx, d, msg)
			}
			if err != nil {
				slog.Warn("web push not delivered", "device", d.ID, "service", d.Service, "err", err)
			}
		}()
	}
}

// sendTo encrypts and posts one message; a subscription the push service
// no longer knows is removed.
func (m *Manager) sendTo(ctx context.Context, d stored, msg Message) error {
	m.mu.Lock()
	key := m.key
	m.mu.Unlock()
	if key == nil {
		return errors.New("webpush: no key")
	}
	u, pub, auth, err := m.check(Subscription{Endpoint: d.Endpoint, Keys: struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	}{d.P256dh, d.Auth}})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	body, err := encrypt(payload, pub, auth)
	if err != nil {
		return err
	}
	token, err := vapidToken(key, u.Scheme+"://"+u.Host, m.now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400")
	urgency := "normal"
	if msg.Urgent {
		urgency = "high"
	}
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Authorization", "vapid t="+token+", k="+b64.EncodeToString(publicBytes(key)))
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("webpush: %s not reachable", u.Hostname())
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		m.mu.Lock()
		m.removeLocked(func(s stored) bool { return s.ID == d.ID })
		m.mu.Unlock()
		return ErrGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("webpush: %s answered %d %s", u.Hostname(), resp.StatusCode, strings.TrimSpace(string(answer)))
	}
	return nil
}

// encrypt is RFC 8291 with the aes128gcm content coding of RFC 8188, one
// record.
func encrypt(plaintext, uaPublic, authSecret []byte) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := as.PublicKey().Bytes()
	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(uaPublic)+string(asPublic), 32)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, 16+4+1+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, 4096)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	// 0x02 marks the last (and only) record.
	return gcm.Seal(header, nonce, append(slices.Clone(plaintext), 0x02), nil), nil
}

// vapidToken is the signed JWT (ES256) of RFC 8292 for one push service.
func vapidToken(key *ecdsa.PrivateKey, audience string, now time.Time) (string, error) {
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": audience, "exp": now.Add(12 * time.Hour).Unix(), "sub": contact})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}
