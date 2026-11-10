package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// Known devices keep the account lock of the login from becoming a way to
// lock the owner out: anyone can make the lock of an account name kick in
// with wrong passwords, but a browser that logged into the account before
// carries a device cookie and is limited on its own instead (plus the lock
// of its address). The idea is that of the "device cookies" of OWASP.
//
// The cookie names the account and a random value, signed with a key made
// from the password hash of the account. That needs nothing stored, holds
// over restarts and ends with every change of the password.

// deviceCookieAge is how long a browser stays known after a login.
const deviceCookieAge = 400 * 24 * time.Hour

func (au authArea) deviceCookieName() string {
	if au.secureCookies {
		return "__Host-titanos_device"
	}
	return "titanos_device"
}

// deviceMAC signs name and nonce with a key from the password hash.
func deviceMAC(passwordHash, name, nonce string) []byte {
	key := sha256.Sum256([]byte("titanos-device\x00" + passwordHash))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(name + "\x00" + nonce))
	return mac.Sum(nil)
}

// knownDevice returns the limiter key of the browser if it carries a valid
// device cookie of the account; "" means an unknown device.
func (au authArea) knownDevice(r *http.Request, name string) string {
	cookie, err := r.Cookie(au.deviceCookieName())
	if err != nil {
		return ""
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || parts[0] != name || len(parts[1]) != 22 {
		return ""
	}
	sum, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ""
	}
	user, ok := au.users.Get(name)
	if !ok || user.PasswordHash == "" || !hmac.Equal(sum, deviceMAC(user.PasswordHash, name, parts[1])) {
		return ""
	}
	return "device:" + parts[1]
}

// rememberDevice marks the browser as one that logged into the account.
func (au authArea) rememberDevice(w http.ResponseWriter, name string) {
	user, ok := au.users.Get(name)
	if !ok || user.PasswordHash == "" {
		return
	}
	raw := make([]byte, 16)
	rand.Read(raw)
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     au.deviceCookieName(),
		Value:    name + "." + nonce + "." + base64.RawURLEncoding.EncodeToString(deviceMAC(user.PasswordHash, name, nonce)),
		Path:     "/",
		MaxAge:   int(deviceCookieAge.Seconds()),
		HttpOnly: true,
		Secure:   au.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}
