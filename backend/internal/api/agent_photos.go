package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/immich"
)

// Photos in the agent (1.07, owner's decision of 16/17 Nov 2026): the agent
// searches the account's own photos in Immich, the photo app of the
// catalog, when it is installed and the account has entered an API key of
// its own Immich user under the agent's settings. There is no key of an
// administrator for everybody: each TitanOS account sees only the photos of
// its own Immich user. Without a key or without Immich the tool does not
// exist. The first version only reads (area "photos", "Nur lesen").
//
// The key is kept encrypted (AES-256-GCM) in the account's private folder
// of the agent (.titanos/agent/<account>/photos.json); the key that opens it
// lies on the stick (/config/system/agent-photos.key), apart from the
// volume. As with T-Mail this protects a copied volume or a log, not a NAS
// in someone else's hands. It never goes to the page, into a log or an
// error text. The address of Immich is never typed in: it is the published
// port of the running catalog app on this NAS (127.0.0.1), so the key can
// go nowhere else.

const (
	agentPhotosFile    = "photos.json"
	agentPhotosKeyFile = "system/agent-photos.key"
	agentPhotosSeal    = "v1:"
	// immichPort is the port of the Immich server inside its container.
	immichPort = 2283
	// agentPhotosMax bounds the photos one search returns (and the card shows).
	agentPhotosMax     = 48
	agentPhotosDefault = 24
	// agentThumbMax bounds a picture passed through (previews are about 0.5 MB).
	agentThumbMax = 20 << 20
)

// agentPhotosStored is photos.json.
type agentPhotosStored struct {
	// Key is the sealed API key ("v1:" + base64 of nonce and ciphertext).
	Key string `json:"key"`
	// User is the Immich user of the key when it was saved (id and name).
	User     string    `json:"user,omitempty"`
	UserName string    `json:"userName,omitempty"`
	Saved    time.Time `json:"saved"`
}

// agentPhotosState is the RAM part: the key of the stick once read, the
// stored entries of the accounts once read, and the address of Immich.
type agentPhotosState struct {
	mu     sync.Mutex
	key    []byte
	stored map[string]*agentPhotosStored // nil entry: read, none stored
	found  string
	at     time.Time
	// find replaces the look for the Immich app in tests.
	find func(context.Context) string
}

var (
	errPhotosKeyMissing = errors.New("the key of the stored Immich keys is missing on the stick")
	errPhotosNoImmich   = errors.New("Immich is not running on this NAS")
	errPhotosNoKey      = errors.New("no Immich API key is set for this account")
)

// immichAddress is the address of the running Immich app of this NAS, ""
// when there is none. Looked up at most every 30 seconds.
func (s *Server) immichAddress(ctx context.Context) string {
	p := &s.agentPhotos
	p.mu.Lock()
	find := p.find
	if find == nil && time.Since(p.at) < 30*time.Second {
		found := p.found
		p.mu.Unlock()
		return found
	}
	p.mu.Unlock()
	var found string
	if find != nil {
		found = find(ctx)
	} else {
		found = s.findImmich(ctx)
	}
	p.mu.Lock()
	p.found, p.at = found, time.Now()
	p.mu.Unlock()
	return found
}

// findImmich looks for a running container of the Immich server and the
// host port of its port 2283.
func (s *Server) findImmich(ctx context.Context) string {
	env := s.agentEnv()
	if env.docker == nil || env.docker.Disabled || (env.servicesOn != nil && !env.servicesOn()["docker"]) {
		return ""
	}
	for _, c := range env.docker.Status(ctx).Containers {
		if c.State != "running" || !strings.Contains(c.Image, "immich-server") {
			continue
		}
		if c.HostNetwork {
			return "http://127.0.0.1:" + strconv.Itoa(immichPort)
		}
		for _, port := range c.Ports {
			if port.Container == immichPort && port.Protocol == "tcp" && port.Host > 0 {
				return "http://127.0.0.1:" + strconv.Itoa(port.Host)
			}
		}
	}
	return ""
}

// photosKey returns the key of the stick; create makes one at the first
// save, never while sealed keys could exist without it.
func (s *Server) photosKey(create bool) ([]byte, error) {
	p := &s.agentPhotos
	if p.key != nil {
		return p.key, nil
	}
	data, err := s.opts.Store.Read(agentPhotosKeyFile)
	if err == nil {
		key, derr := hex.DecodeString(strings.TrimSpace(string(data)))
		if derr != nil || len(key) != 32 {
			slog.Error("KI-Agent: the key of the stored Immich keys is damaged", "file", agentPhotosKeyFile)
			return nil, errPhotosKeyMissing
		}
		p.key = key
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) || !create {
		return nil, errPhotosKeyMissing
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := s.opts.Store.Write(agentPhotosKeyFile, []byte(hex.EncodeToString(key)+"\n")); err != nil {
		return nil, err
	}
	p.key = key
	return key, nil
}

// photosAAD binds a sealed key to its account and the account's creation
// time: copied into another account's folder, or kept by a later account
// of the same name, it does not open.
func (s *Server) photosAAD(user string) []byte {
	var created int64
	if s.opts.Users != nil {
		if u, ok := s.opts.Users.Get(user); ok {
			created = u.Created.UTC().UnixNano()
		}
	}
	return []byte("agent-photos/v1/" + user + "/" + strconv.FormatInt(created, 10))
}

func (s *Server) sealPhotosKey(user, plain string) (string, error) {
	s.agentPhotos.mu.Lock()
	key, err := s.photosKey(true)
	s.agentPhotos.mu.Unlock()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return agentPhotosSeal + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), s.photosAAD(user))), nil
}

func (s *Server) openPhotosKey(user, sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, agentPhotosSeal)
	if !ok {
		return "", errors.New("unknown form of a stored Immich key")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", errPhotosKeyMissing
	}
	s.agentPhotos.mu.Lock()
	key, err := s.photosKey(false)
	s.agentPhotos.mu.Unlock()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errPhotosKeyMissing
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], s.photosAAD(user))
	if err != nil {
		slog.Error("KI-Agent: a stored Immich key does not open", "account", user)
		return "", errPhotosKeyMissing
	}
	return string(plain), nil
}

// photosStored reads the account's entry (RAM after the first read); nil
// when none is stored.
func (s *Server) photosStored(user string) (*agentPhotosStored, error) {
	p := &s.agentPhotos
	p.mu.Lock()
	if st, ok := p.stored[user]; ok {
		p.mu.Unlock()
		return st, nil
	}
	p.mu.Unlock()
	var st agentPhotosStored
	err := s.agentStore().ReadPrivate(user, agentPhotosFile, &st)
	var out *agentPhotosStored
	switch {
	case errors.Is(err, agent.ErrNotFound):
	case err != nil:
		return nil, err
	case st.Key != "":
		out = &st
	}
	p.mu.Lock()
	if p.stored == nil {
		p.stored = map[string]*agentPhotosStored{}
	}
	p.stored[user] = out
	p.mu.Unlock()
	return out, nil
}

func (s *Server) forgetPhotosStored(user string) {
	s.agentPhotos.mu.Lock()
	delete(s.agentPhotos.stored, user)
	s.agentPhotos.mu.Unlock()
}

// immichFor is the client of an account: its own key and the Immich app of
// this NAS. Never for the profile of Home Assistant, which is no account.
func (s *Server) immichFor(ctx context.Context, user string) (*immich.Client, *agentPhotosStored, error) {
	if user == "" || user == agent.AssistProfile {
		return nil, nil, errPhotosNoKey
	}
	st, err := s.photosStored(user)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, errPhotosNoKey
	}
	address := s.immichAddress(ctx)
	if address == "" {
		return nil, nil, errPhotosNoImmich
	}
	key, err := s.openPhotosKey(user, st.Key)
	if err != nil {
		return nil, nil, err
	}
	return immich.New(address, key), st, nil
}

// agentPhotosView is what the page sees: never the key.
type agentPhotosView struct {
	Immich   bool   `json:"immich"`
	KeySet   bool   `json:"keySet"`
	UserName string `json:"userName,omitempty"`
	Port     int    `json:"port,omitempty"`
	// Permissions the key lacks (Immich's names), from the last check.
	Missing []string `json:"missing,omitempty"`
	Version string   `json:"version,omitempty"`
	Smart   *bool    `json:"smart,omitempty"`
	// Addresses for the Immich app on the phone (the guide "Fotos vom
	// iPhone sichern"): the home network and, if set up, Tailscale.
	Addresses []immichAddressView `json:"addresses,omitempty"`
}

// immichAddressView is an address of Immich for the phone app.
type immichAddressView struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// immichAddresses are the addresses the Immich app on a phone can use:
// the NAS in the home network and, with Tailscale, its name in the
// tailnet, each with the port of the Immich app. Plain http: the port of
// Immich is not behind titand's certificates.
func (s *Server) immichAddresses(port int) []immichAddressView {
	if port <= 0 {
		return nil
	}
	out := []immichAddressView{}
	p := ":" + strconv.Itoa(port)
	if ip := appproxy.PrimaryIPv4(); ip != "" {
		out = append(out, immichAddressView{Kind: "lan", URL: "http://" + ip + p})
	}
	if ts := s.agentEnv().tailscale; ts != nil {
		st := ts.Status()
		switch {
		case st.Name != "":
			out = append(out, immichAddressView{Kind: "tailscale", URL: "http://" + strings.TrimSuffix(st.Name, ".") + p})
		case len(st.IPs) > 0:
			out = append(out, immichAddressView{Kind: "tailscale", URL: "http://" + st.IPs[0] + p})
		}
	}
	return out
}

func immichPortOf(address string) int {
	i := strings.LastIndex(address, ":")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(address[i+1:])
	return n
}

func photosRefuseAssist(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r).Name == agent.AssistProfile {
		writeError(w, http.StatusForbidden, "forbidden", "the profile of Home Assistant has no photos")
		return true
	}
	return false
}

// GET /api/v1/agent/photos: whether Immich runs here and the account has a key.
func (s *Server) handleAgentPhotos(w http.ResponseWriter, r *http.Request) {
	if photosRefuseAssist(w, r) {
		return
	}
	user := currentUser(r).Name
	address := s.immichAddress(r.Context())
	view := agentPhotosView{Immich: address != "", Port: immichPortOf(address)}
	view.Addresses = s.immichAddresses(view.Port)
	st, err := s.photosStored(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if st != nil {
		view.KeySet, view.UserName = true, st.UserName
	}
	writeJSON(w, http.StatusOK, view)
}

// immichCheck asks Immich with a key: its user, the missing permissions,
// the version and whether smart search is on.
func immichCheck(ctx context.Context, c *immich.Client) (immich.User, agentPhotosView, error) {
	var view agentPhotosView
	me, err := c.Me(ctx)
	if err != nil {
		return me, view, err
	}
	if have, err := c.KeyPermissions(ctx); err == nil {
		view.Missing = immich.Missing(have)
	}
	view.Version, _ = c.Version(ctx)
	if f, err := c.Features(ctx); err == nil {
		smart := f.SmartSearch
		view.Smart = &smart
	}
	return me, view, nil
}

func photosCheckError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, immich.ErrUnauthorized):
		writeError(w, http.StatusBadRequest, "immich_key_rejected", "Immich did not accept the key")
	case errors.Is(err, immich.ErrForbidden):
		writeError(w, http.StatusBadRequest, "immich_key_permissions", "the key may not read the Immich user (permission user.read)")
	case errors.Is(err, errPhotosNoImmich):
		writeError(w, http.StatusConflict, "immich_missing", "Immich is not running on this NAS")
	case errors.Is(err, errPhotosNoKey):
		writeError(w, http.StatusNotFound, "immich_no_key", "no Immich key is set for this account")
	case errors.Is(err, errPhotosKeyMissing):
		writeError(w, http.StatusInternalServerError, "immich_key_unreadable", "the stored Immich key cannot be opened")
	default:
		writeError(w, http.StatusBadGateway, "immich_unreachable", "Immich does not answer")
	}
}

// PUT /api/v1/agent/photos {"key": "…"}: checks the key against Immich and
// stores it sealed. A key Immich does not take is not stored.
func (s *Server) handleAgentPhotosSet(w http.ResponseWriter, r *http.Request) {
	if photosRefuseAssist(w, r) {
		return
	}
	user := currentUser(r).Name
	var req struct {
		Key string `json:"key"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" || len(key) > 200 || strings.ContainsAny(key, " \t\r\n") {
		writeError(w, http.StatusBadRequest, "invalid_immich_key", "enter the API key of your Immich user")
		return
	}
	address := s.immichAddress(r.Context())
	if address == "" {
		photosCheckError(w, errPhotosNoImmich)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	me, view, err := immichCheck(ctx, immich.New(address, key))
	if err != nil {
		photosCheckError(w, err)
		return
	}
	sealed, err := s.sealPhotosKey(user, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "immich_key_store", "the key could not be stored")
		return
	}
	stored := agentPhotosStored{Key: sealed, User: me.ID, UserName: me.Name, Saved: time.Now().UTC()}
	if err := s.agentStore().WritePrivate(user, agentPhotosFile, stored); err != nil {
		agentStoreError(w, err)
		return
	}
	s.forgetPhotosStored(user)
	view.Immich, view.KeySet, view.UserName, view.Port = true, true, me.Name, immichPortOf(address)
	writeJSON(w, http.StatusOK, view)
}

// POST /api/v1/agent/photos/check: the stored key against Immich now.
func (s *Server) handleAgentPhotosCheck(w http.ResponseWriter, r *http.Request) {
	if photosRefuseAssist(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, st, err := s.immichFor(ctx, currentUser(r).Name)
	if err != nil {
		photosCheckError(w, err)
		return
	}
	me, view, err := immichCheck(ctx, c)
	if err != nil {
		photosCheckError(w, err)
		return
	}
	view.Immich, view.KeySet, view.UserName, view.Port = true, true, me.Name, immichPortOf(c.Base)
	if me.ID != st.User {
		// The key now belongs to another user of Immich (it was replaced
		// there): the name shown is the one of now.
		view.UserName = me.Name
	}
	writeJSON(w, http.StatusOK, view)
}

// DELETE /api/v1/agent/photos: removes the account's key.
func (s *Server) handleAgentPhotosDelete(w http.ResponseWriter, r *http.Request) {
	if photosRefuseAssist(w, r) {
		return
	}
	user := currentUser(r).Name
	if err := s.agentStore().RemovePrivate(user, agentPhotosFile); err != nil {
		agentStoreError(w, err)
		return
	}
	s.forgetPhotosStored(user)
	writeJSON(w, http.StatusOK, agentPhotosView{Immich: s.immichAddress(r.Context()) != ""})
}

// GET /api/v1/agent/photos/{id}/picture?size=thumbnail|preview: the picture
// of one of the account's own photos. The asset is read with the account's
// key first and must belong to the key's Immich user; anything else is
// "not found". Nothing of Immich's answer but the picture goes on.
func (s *Server) handleAgentPhotoPicture(w http.ResponseWriter, r *http.Request) {
	if photosRefuseAssist(w, r) {
		return
	}
	id := r.PathValue("id")
	if !immich.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	size := immich.SizeThumbnail
	if r.URL.Query().Get("size") == immich.SizePreview {
		size = immich.SizePreview
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, _, err := s.immichFor(ctx, currentUser(r).Name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	me, err := c.Me(ctx)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	asset, err := c.Asset(ctx, id)
	if err != nil || asset.OwnerID != me.ID || asset.ID != id {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	body, ctype, err := c.Thumbnail(ctx, id, size)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	defer body.Close()
	media, _, _ := mime.ParseMediaType(ctype)
	switch media {
	case "image/jpeg", "image/webp", "image/png", "image/avif":
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(body, agentThumbMax))
}

// GET /api/v1/immich/phone: whether the Immich app runs here and the
// addresses for the Immich app on a phone, for the guide "Fotos vom iPhone
// sichern". Every account backs up its own phone, so this needs no
// administration and no agent; it names no key and no account.
func (s *Server) handleImmichPhone(w http.ResponseWriter, r *http.Request) {
	address := s.immichAddress(r.Context())
	port := immichPortOf(address)
	writeJSON(w, http.StatusOK, map[string]any{"immich": address != "", "port": port, "addresses": s.immichAddresses(port)})
}

func (s *Server) registerAgentPhotos(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/immich/phone", s.requireUser(s.handleImmichPhone))
	mux.HandleFunc("GET /api/v1/agent/photos", s.requireAgent(s.handleAgentPhotos))
	mux.HandleFunc("PUT /api/v1/agent/photos", s.requireAgent(s.handleAgentPhotosSet))
	mux.HandleFunc("DELETE /api/v1/agent/photos", s.requireAgent(s.handleAgentPhotosDelete))
	mux.HandleFunc("POST /api/v1/agent/photos/check", s.requireAgent(s.handleAgentPhotosCheck))
	mux.HandleFunc("GET /api/v1/agent/photos/{id}/picture", s.requireAgent(s.handleAgentPhotoPicture))
}

// photoArgs are the arguments of photos_search.
type photoArgs struct {
	Query   string `json:"query"`
	Person  string `json:"person"`
	City    string `json:"city"`
	Country string `json:"country"`
	From    string `json:"from"`
	To      string `json:"to"`
	Limit   int    `json:"limit"`
}

// photoHit is one photo of the result: short keys, so that a full result
// stays small (the card under the answer reads it).
type photoHit struct {
	ID    string `json:"id"`
	Date  string `json:"date,omitempty"`
	Place string `json:"place,omitempty"`
	Video bool   `json:"video,omitempty"`
}

// parseDay reads a day (YYYY-MM-DD) or a month or year; end gives the end.
func parseDay(s string, end bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if end {
			switch layout {
			case "2006-01-02":
				t = t.AddDate(0, 0, 1)
			case "2006-01":
				t = t.AddDate(0, 1, 0)
			default:
				t = t.AddDate(1, 0, 0)
			}
			t = t.Add(-time.Second)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a date (YYYY-MM-DD, YYYY-MM or YYYY)", s)
}

// photosTool is photos_search, or nil when the account has no key or
// Immich does not run here (then the tool does not exist).
func (b *agentToolbox) photosTool() *agentTool {
	user := b.user.Name
	if user == agent.AssistProfile {
		return nil
	}
	st, err := b.s.photosStored(user)
	if err != nil || st == nil || b.s.immichAddress(context.Background()) == "" {
		return nil
	}
	return &agentTool{
		name: "photos_search", area: agent.AreaPhotos, kind: agent.KindRead, untrusted: true,
		description: "Search the person's own photos and videos in Immich (the photo app on this NAS) – only their own Immich account. " +
			"query: what is in the picture in plain words (\"beach at sunset\", \"birthday cake\"); person: the name of a person Immich knows by face; city/country; from/to: the time span (YYYY-MM-DD, YYYY-MM or YYYY), e.g. from 2025-06 to 2025-08 for \"summer holiday 2025\". " +
			"Returns date, place and id of each photo. The chat shows them as pictures under your answer: do not list them again as a table or with ids, say in a sentence what was found (how many, when, where).",
		params: schema(map[string]any{
			"query":   map[string]any{"type": "string", "description": "What the photos show, in plain words."},
			"person":  map[string]any{"type": "string", "description": "Name of a person Immich recognises."},
			"city":    map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
			"from":    map[string]any{"type": "string", "description": "Taken on or after (YYYY-MM-DD, YYYY-MM or YYYY)."},
			"to":      map[string]any{"type": "string", "description": "Taken on or before (YYYY-MM-DD, YYYY-MM or YYYY)."},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": agentPhotosMax},
		}),
		own: func(ctx context.Context, _ *agent.Chat, raw json.RawMessage) (agent.Outcome, error) {
			return b.s.photosSearch(ctx, user, raw)
		},
	}
}

// photosSearch runs photos_search for an account.
func (s *Server) photosSearch(ctx context.Context, user string, raw json.RawMessage) (agent.Outcome, error) {
	var a photoArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return agent.Outcome{}, errors.New("unreadable arguments")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, _, err := s.immichFor(ctx, user)
	if err != nil {
		return agent.Outcome{}, err
	}
	q := immich.Query{Text: strings.TrimSpace(a.Query), City: strings.TrimSpace(a.City), Country: strings.TrimSpace(a.Country), Size: a.Limit}
	if q.Size <= 0 {
		q.Size = agentPhotosDefault
	}
	q.Size = min(q.Size, agentPhotosMax)
	if a.From != "" {
		if q.After, err = parseDay(a.From, false); err != nil {
			return agent.Outcome{}, err
		}
	}
	if a.To != "" {
		if q.Before, err = parseDay(a.To, true); err != nil {
			return agent.Outcome{}, err
		}
	}
	var notes []string
	me, err := c.Me(ctx)
	if err != nil {
		return agent.Outcome{}, immichToolError(err)
	}
	if name := strings.TrimSpace(a.Person); name != "" {
		people, err := c.People(ctx, name)
		if err != nil {
			return agent.Outcome{}, immichToolError(err)
		}
		for _, p := range people {
			if len(q.PersonIDs) < 5 && immich.ValidID(p.ID) {
				q.PersonIDs = append(q.PersonIDs, p.ID)
			}
		}
		if len(q.PersonIDs) == 0 {
			return agent.Outcome{Text: photosJSON(map[string]any{"photos": []photoHit{}, "total": 0, "note": "Immich knows no person named " + strconv.Quote(name) + ". Faces get names in Immich itself (People)."})}, nil
		}
	}
	smart := false
	if q.Text != "" {
		if f, err := c.Features(ctx); err == nil && f.SmartSearch {
			smart = true
		} else {
			notes = append(notes, "Smart search is off in Immich (machine learning); searched by date, place and person only, the words of the query were not used.")
		}
	}
	res, err := c.Search(ctx, q, smart)
	if err != nil {
		return agent.Outcome{}, immichToolError(err)
	}
	hits := []photoHit{}
	others := 0
	for _, it := range res.Items {
		// Only the account's own photos: a partner's shared photos can be
		// part of Immich's answer.
		if it.OwnerID != me.ID || !immich.ValidID(it.ID) || it.IsTrashed {
			others++
			continue
		}
		hits = append(hits, photoHit{ID: it.ID, Date: it.TakenAt(), Place: it.Place(), Video: it.Type == "VIDEO"})
		if len(hits) == q.Size {
			break
		}
	}
	out := map[string]any{"photos": hits, "shown": len(hits), "search": map[bool]string{true: "smart", false: "metadata"}[res.Smart]}
	// The smart search ranks everything, its total says nothing; the
	// search by metadata counts the matches.
	if !res.Smart {
		out["total"] = max(res.Total-others, len(hits))
	}
	if len(notes) > 0 {
		out["note"] = strings.Join(notes, " ")
	}
	return agent.Outcome{Text: photosJSON(out)}, nil
}

func immichToolError(err error) error {
	switch {
	case errors.Is(err, immich.ErrUnauthorized):
		return errors.New("Immich did not accept the account's API key; it can be entered anew under the agent's settings › Photos")
	case errors.Is(err, immich.ErrForbidden):
		return errors.New("the Immich API key lacks a permission (needed: " + strings.Join(immich.Permissions, ", ") + ")")
	}
	return errors.New("Immich does not answer")
}

func photosJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
