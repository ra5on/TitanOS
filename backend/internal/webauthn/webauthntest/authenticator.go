// Package webauthntest is a software passkey for tests: it answers
// navigator.credentials.create() and get() like a platform authenticator.
package webauthntest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
)

var enc = base64.RawURLEncoding

// Authenticator holds one ES256 credential.
type Authenticator struct {
	Key    *ecdsa.PrivateKey
	ID     []byte
	Handle []byte
	Count  uint32
}

func New() *Authenticator {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 16)
	rand.Read(id)
	return &Authenticator{Key: key, ID: id}
}

func head(major byte, n int) []byte {
	if n < 24 {
		return []byte{major<<5 | byte(n)}
	}
	return []byte{major<<5 | 24, byte(n)}
}
func cint(v int) []byte {
	if v >= 0 {
		return head(0, v)
	}
	return head(1, -1-v)
}
func cbytes(b []byte) []byte { return append(head(2, len(b)), b...) }
func ctext(s string) []byte  { return append(head(3, len(s)), s...) }

func (a *Authenticator) cose() []byte {
	raw, _ := a.Key.PublicKey.Bytes()
	out := head(5, 5)
	for _, p := range [][]byte{cint(1), cint(2), cint(3), cint(-7), cint(-1), cint(1), cint(-2), cbytes(raw[1:33]), cint(-3), cbytes(raw[33:])} {
		out = append(out, p...)
	}
	return out
}

func (a *Authenticator) authData(rpID string, attested bool) []byte {
	hash := sha256.Sum256([]byte(rpID))
	flags := byte(0x01 | 0x04)
	if attested {
		flags |= 0x40
	}
	out := append(hash[:], flags)
	out = binary.BigEndian.AppendUint32(out, a.Count)
	if attested {
		out = append(out, make([]byte, 16)...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.ID)))
		out = append(append(out, a.ID...), a.cose()...)
	}
	return out
}

func clientData(kind, challenge, origin string) []byte {
	data, _ := json.Marshal(map[string]string{"type": kind, "challenge": challenge, "origin": origin})
	return data
}

// Create answers a registration; challenge is base64url as the NAS sent it.
// It returns clientDataJSON and attestationObject, base64url encoded.
func (a *Authenticator) Create(challenge, origin, rpID string) (string, string) {
	att := head(5, 3)
	for _, p := range [][]byte{ctext("fmt"), ctext("none"), ctext("attStmt"), head(5, 0), ctext("authData"), cbytes(a.authData(rpID, true))} {
		att = append(att, p...)
	}
	return enc.EncodeToString(clientData("webauthn.create", challenge, origin)), enc.EncodeToString(att)
}

// Get answers a login: credential ID, clientDataJSON, authenticatorData,
// signature and user handle, all base64url encoded.
func (a *Authenticator) Get(challenge, origin, rpID string) (id, cd, ad, sig, handle string) {
	c := clientData("webauthn.get", challenge, origin)
	d := a.authData(rpID, false)
	digest := sha256.Sum256(c)
	h := sha256.Sum256(append(append([]byte(nil), d...), digest[:]...))
	s, _ := ecdsa.SignASN1(rand.Reader, a.Key, h[:])
	return enc.EncodeToString(a.ID), enc.EncodeToString(c), enc.EncodeToString(d), enc.EncodeToString(s), enc.EncodeToString(a.Handle)
}
