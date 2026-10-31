package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/webauthn"
)

// Passkeys (WebAuthn): an account registers one under "My account" with
// its password (and code); afterwards it logs in with it instead of
// password and second factor. Challenges live in RAM only.

const passkeyChallengeTTL = 2 * time.Minute

type passkeyChallenge struct {
	challenge []byte
	user      string // registration only
	rpID      string
	origin    string
	expires   time.Time
	// source is who asked: the address (IPv6 by /64) of a login, the
	// account of a registration or confirmation.
	source string
	// confirm marks a challenge for reconfirmProof: it proves who sits at
	// the browser and never registers a passkey.
	confirm bool
}

const (
	// passkeyMaxWaiting bounds the challenges in RAM. When it is full,
	// new ones are refused instead of pushing out those of others.
	passkeyMaxWaiting = 4096
	// passkeyPerSource is how many challenges one source has waiting;
	// its own oldest gives way, never one of somebody else.
	passkeyPerSource = 8
	// passkeyBegins challenges a source gets per passkeyBeginWindow.
	passkeyBegins      = 20
	passkeyBeginWindow = time.Minute
)

type passkeyChallenges struct {
	mu    sync.Mutex
	items map[string]passkeyChallenge
	// begins are the recent challenges of each source, for the rate.
	begins map[string][]time.Time
}

// passkeySource is the source of a login challenge: the address, or its
// /64 for IPv6, where one device easily has many addresses.
func passkeySource(r *http.Request) string {
	ip := net.ParseIP(remoteIP(r))
	if ip == nil {
		return "ip:" + remoteIP(r)
	}
	if ip.To4() == nil {
		return "net:" + ip.Mask(net.CIDRMask(64, 128)).String()
	}
	return "ip:" + ip.String()
}

// put stores a fresh challenge and returns its ID; ok is false when the
// source asked too often or too many are waiting.
func (p *passkeyChallenges) put(c passkeyChallenge) (id string, challenge []byte, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.items == nil {
		p.items = map[string]passkeyChallenge{}
		p.begins = map[string][]time.Time{}
	}
	for source, times := range p.begins {
		recent := times[:0]
		for _, at := range times {
			if now.Sub(at) < passkeyBeginWindow {
				recent = append(recent, at)
			}
		}
		if len(recent) == 0 {
			delete(p.begins, source)
		} else {
			p.begins[source] = recent
		}
	}
	if len(p.begins[c.source]) >= passkeyBegins || len(p.begins) >= passkeyMaxWaiting {
		return "", nil, false
	}
	var oldest string
	mine := 0
	for id, item := range p.items {
		if now.After(item.expires) {
			delete(p.items, id)
		} else if item.source == c.source {
			mine++
			if oldest == "" || item.expires.Before(p.items[oldest].expires) {
				oldest = id
			}
		}
	}
	if mine >= passkeyPerSource {
		delete(p.items, oldest)
	}
	if len(p.items) >= passkeyMaxWaiting {
		return "", nil, false
	}
	p.begins[c.source] = append(p.begins[c.source], now)
	raw := make([]byte, 16)
	rand.Read(raw)
	c.challenge = make([]byte, 32)
	rand.Read(c.challenge)
	c.expires = now.Add(passkeyChallengeTTL)
	id = hex.EncodeToString(raw)
	p.items[id] = c
	return id, c.challenge, true
}

// take hands out a challenge once.
func (p *passkeyChallenges) take(id string) (passkeyChallenge, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.items[id]
	delete(p.items, id)
	return c, ok && time.Now().Before(c.expires)
}

// passkeyOrigin derives relying party ID and origin from the address the
// browser used. Browsers refuse passkeys for IP addresses and for pages
// without a trusted HTTPS certificate; localhost works over plain HTTP.
func passkeyOrigin(r *http.Request) (rpID, origin string, ok bool) {
	host := r.Host
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" || net.ParseIP(strings.Trim(name, "[]")) != nil || strings.ContainsAny(name, "/@\\ ") {
		return "", "", false
	}
	scheme := "https"
	if r.TLS == nil {
		if name != "localhost" {
			return "", "", false
		}
		scheme = "http"
	}
	return name, scheme + "://" + strings.ToLower(host), true
}

func writePasskeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrTooManyPasskeys):
		writeError(w, http.StatusConflict, "too_many_passkeys", "this account holds as many passkeys as it may")
	case errors.Is(err, auth.ErrPasskeyExists):
		writeError(w, http.StatusConflict, "passkey_exists", "this passkey is already registered")
	case errors.Is(err, auth.ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "passkey_not_found", "no such passkey")
	default:
		writeAccountError(w, err)
	}
}

type passkeyResponse struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	RPID    string    `json:"rpId"`
	Synced  bool      `json:"synced"`
	Created time.Time `json:"created"`
}

func passkeyList(user auth.User) []passkeyResponse {
	list := []passkeyResponse{}
	for _, key := range user.Passkeys {
		list = append(list, passkeyResponse{ID: key.ID, Name: key.Name, RPID: key.RPID, Synced: key.Synced, Created: key.Created})
	}
	return list
}

func (s *Server) handlePasskeys(w http.ResponseWriter, r *http.Request) {
	user, _ := s.opts.Users.Get(currentUser(r).Name)
	rpID, _, usable := passkeyOrigin(r)
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": passkeyList(user), "rpId": rpID, "usable": usable})
}

// handlePasskeyBegin starts a registration after checking password and
// second factor: a passkey is a way into the account like the password.
func (s *Server) handlePasskeyBegin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if s.sessionToken(r) == "" {
		writeError(w, http.StatusBadRequest, "session_required", "passkeys are added from a logged-in browser")
		return
	}
	rpID, origin, ok := passkeyOrigin(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey_address", "passkeys need a host name with a trusted HTTPS certificate, not an IP address")
		return
	}
	key := "user:" + user.Name
	if wait := s.limiter.RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	if _, ok := s.opts.Users.Authenticate(user.Name, req.Password); !ok {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return
	}
	if !s.opts.Users.CheckSecondFactor(user.Name, req.Code) {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return
	}
	handle, err := s.opts.Users.WebAuthnUserID(user.Name)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	current, _ := s.opts.Users.Get(user.Name)
	exclude := []map[string]string{}
	for _, key := range current.Passkeys {
		exclude = append(exclude, map[string]string{"type": "public-key", "id": key.ID})
	}
	params := []map[string]any{}
	for _, alg := range webauthn.Algorithms {
		params = append(params, map[string]any{"type": "public-key", "alg": alg})
	}
	id, challenge, ok := s.passkeys.put(passkeyChallenge{user: user.Name, rpID: rpID, origin: origin, source: "user:" + user.Name})
	if !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many passkey requests, try again later")
		return
	}
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeId": id,
		"publicKey": map[string]any{
			"challenge":          webauthn.Encoding.EncodeToString(challenge),
			"rp":                 map[string]string{"id": rpID, "name": "TitanOS " + hostname},
			"user":               map[string]string{"id": webauthn.Encoding.EncodeToString(handle), "name": user.Name, "displayName": user.Name},
			"pubKeyCredParams":   params,
			"timeout":            int(passkeyChallengeTTL / time.Millisecond),
			"excludeCredentials": exclude,
			"attestation":        "none",
			"authenticatorSelection": map[string]any{
				"residentKey":        "required",
				"requireResidentKey": true,
				"userVerification":   "required",
			},
		},
	})
}

func (s *Server) handlePasskeyFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID       string `json:"challengeId"`
		Name              string `json:"name"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	c, ok := s.passkeys.take(req.ChallengeID)
	if !ok || c.user != user.Name || c.confirm {
		writeError(w, http.StatusBadRequest, "passkey_expired", "the request expired, start again")
		return
	}
	clientData, err1 := webauthn.Encoding.DecodeString(req.ClientDataJSON)
	attestation, err2 := webauthn.Encoding.DecodeString(req.AttestationObject)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "passkey_invalid", "the passkey answer is malformed")
		return
	}
	cred, err := webauthn.Register(clientData, attestation, webauthn.Expect{Challenge: c.challenge, Origin: c.origin, RPID: c.rpID})
	if err != nil {
		slog.Warn("passkey registration refused", "user", user.Name, "err", err)
		writeError(w, http.StatusBadRequest, "passkey_invalid", "the passkey could not be verified")
		return
	}
	name := auth.PasskeyName(req.Name)
	if name == "" {
		name = "Passkey"
	}
	updated, err := s.opts.Users.AddPasskey(user.Name, auth.Passkey{
		ID: webauthn.Encoding.EncodeToString(cred.ID), Name: name, PublicKey: cred.PublicKey,
		RPID: c.rpID, SignCount: cred.SignCount, Synced: cred.Synced, Created: time.Now().UTC(),
	})
	if err != nil {
		writePasskeyError(w, err)
		return
	}
	noteTarget(r, name)
	slog.Info("passkey added", "user", user.Name, "rpId", c.rpID, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": passkeyList(updated)})
}

func (s *Server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	updated, err := s.opts.Users.RemovePasskey(user.Name, r.PathValue("id"))
	if err != nil {
		writePasskeyError(w, err)
		return
	}
	slog.Info("passkey removed", "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": passkeyList(updated)})
}

// handlePasskeyConfirmBegin hands out a challenge for one of the account's
// own passkeys of this host name, as proof for reconfirmProof.
func (s *Server) handlePasskeyConfirmBegin(w http.ResponseWriter, r *http.Request) {
	user, _ := s.opts.Users.Get(currentUser(r).Name)
	if s.sessionToken(r) == "" {
		writeError(w, http.StatusBadRequest, "session_required", "only possible from a logged-in browser")
		return
	}
	rpID, origin, ok := passkeyOrigin(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey_address", "passkeys need a host name with a trusted HTTPS certificate, not an IP address")
		return
	}
	allow := []map[string]string{}
	for _, key := range user.Passkeys {
		if key.RPID == rpID {
			allow = append(allow, map[string]string{"type": "public-key", "id": key.ID})
		}
	}
	if len(allow) == 0 {
		writeError(w, http.StatusConflict, "no_passkey", "the account has no passkey for this address")
		return
	}
	if wait := s.limiter.RetryAfter("user:" + user.Name); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	id, challenge, ok := s.passkeys.put(passkeyChallenge{user: user.Name, rpID: rpID, origin: origin, source: "user:" + user.Name, confirm: true})
	if !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many passkey requests, try again later")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeId": id,
		"publicKey": map[string]any{
			"challenge":        webauthn.Encoding.EncodeToString(challenge),
			"rpId":             rpID,
			"timeout":          int(passkeyChallengeTTL / time.Millisecond),
			"userVerification": "required",
			"allowCredentials": allow,
		},
	})
}

// handlePasskeyLoginBegin hands out a challenge for a login. Nothing about
// accounts is revealed: the browser offers the passkeys it holds.
func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	rpID, origin, ok := passkeyOrigin(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey_address", "passkeys need a host name with a trusted HTTPS certificate, not an IP address")
		return
	}
	if wait := s.limiter.RetryAfter("ip:" + remoteIP(r)); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed logins, try again later")
		return
	}
	id, challenge, ok := s.passkeys.put(passkeyChallenge{rpID: rpID, origin: origin, source: passkeySource(r)})
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]apiError{"error": {
			Code: "rate_limited", Message: "too many passkey requests, try again later", RetryAfter: int(passkeyBeginWindow.Seconds()),
		}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeId": id,
		"publicKey": map[string]any{
			"challenge":        webauthn.Encoding.EncodeToString(challenge),
			"rpId":             rpID,
			"timeout":          int(passkeyChallengeTTL / time.Millisecond),
			"userVerification": "required",
			"allowCredentials": []any{},
		},
	})
}

func (s *Server) handlePasskeyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID       string `json:"challengeId"`
		ID                string `json:"id"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ip := remoteIP(r)
	ipKey := "ip:" + ip
	if wait := s.limiter.RetryAfter(ipKey); wait > 0 {
		writeJSON(w, http.StatusTooManyRequests, map[string]apiError{"error": {
			Code: "rate_limited", Message: "too many failed logins, try again later", RetryAfter: int(math.Ceil(wait.Seconds())),
		}})
		return
	}
	if !s.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return
	}
	fail := func(name, why string) {
		s.limiter.Fail(ipKey)
		s.opts.Firewall.Failed(ip, firewall.SourceWeb)
		slog.Warn("passkey login failed", "user", name, "ip", ip, "why", why)
		s.recordEvent(r, name, "login_failed", "passkey", false)
		writeError(w, http.StatusUnauthorized, "passkey_failed", "the passkey was not accepted")
	}
	c, ok := s.passkeys.take(req.ChallengeID)
	if !ok || c.user != "" {
		writeError(w, http.StatusBadRequest, "passkey_expired", "the request expired, start again")
		return
	}
	clientData, err1 := webauthn.Encoding.DecodeString(req.ClientDataJSON)
	authData, err2 := webauthn.Encoding.DecodeString(req.AuthenticatorData)
	signature, err3 := webauthn.Encoding.DecodeString(req.Signature)
	handle, err4 := webauthn.Encoding.DecodeString(req.UserHandle)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		fail("", "malformed")
		return
	}
	user, key, found := s.opts.Users.FindPasskey(req.ID, handle)
	if !found || key.RPID != c.rpID {
		fail("", "unknown passkey")
		return
	}
	count, err := webauthn.Login(clientData, authData, signature, key.Credential(), webauthn.Expect{Challenge: c.challenge, Origin: c.origin, RPID: c.rpID})
	if err != nil {
		fail(user.Name, err.Error())
		return
	}
	if user.Disabled {
		slog.Warn("login of a locked account refused", "user", user.Name, "ip", ip)
		s.recordEvent(r, user.Name, "login_disabled", "passkey", false)
		writeError(w, http.StatusForbidden, "account_disabled", "this account is locked; ask an owner or administrator")
		return
	}
	if err := s.opts.Users.UsePasskey(user.Name, key.ID, count); err != nil {
		slog.Warn("passkey counter not saved", "user", user.Name, "err", err)
	}
	slog.Info("login", "user", user.Name, "ip", ip, "with", "passkey")
	if s.startSession(w, r, user) {
		s.recordEvent(r, user.Name, "login", "passkey", true)
		if user.Role.CanAdminister() && s.opts.Update != nil {
			s.opts.Update.CheckAfterLogin()
		}
		writeJSON(w, http.StatusOK, userOf(user))
	}
}
