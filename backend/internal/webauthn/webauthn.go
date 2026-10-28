package webauthn

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// Errors name the step that failed; the message never contains the key.
var (
	ErrClientData = errors.New("webauthn: client data does not match")
	ErrAuthData   = errors.New("webauthn: authenticator data does not match")
	ErrSignature  = errors.New("webauthn: signature is not valid")
	ErrCounter    = errors.New("webauthn: signature counter went back (cloned authenticator?)")
)

// Flags of the authenticator data.
const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified (PIN, fingerprint, face)
	flagBE = 0x08 // backup eligible (synced passkey)
	flagAT = 0x40 // attested credential data included
	flagED = 0x80 // extension data included
)

// Expect is what one ceremony has to match.
type Expect struct {
	// Challenge is the random value the NAS sent, as raw bytes.
	Challenge []byte
	// Origin is the exact origin of the page, e.g. https://nas.example.org.
	Origin string
	// RPID is the relying party ID (the host name) the credential belongs to.
	RPID string
}

// Credential is a registered passkey.
type Credential struct {
	ID        []byte
	PublicKey []byte // COSE_Key as sent by the authenticator
	SignCount uint32
	// Synced says the authenticator may copy the passkey to other devices.
	Synced bool
}

// Encoding is the base64url form without padding WebAuthn uses throughout.
var Encoding = base64.RawURLEncoding

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func checkClientData(raw []byte, kind string, want Expect) error {
	var c clientData
	if len(raw) > 4096 || json.Unmarshal(raw, &c) != nil {
		return ErrClientData
	}
	challenge, err := Encoding.DecodeString(c.Challenge)
	if err != nil || c.Type != kind || c.CrossOrigin || c.Origin != want.Origin ||
		len(want.Challenge) < 16 || subtle.ConstantTimeCompare(challenge, want.Challenge) != 1 {
		return ErrClientData
	}
	return nil
}

type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	credID    []byte
	key       []byte
}

func parseAuthData(data []byte, wantCredential bool) (authData, error) {
	if len(data) < 37 {
		return authData{}, ErrAuthData
	}
	a := authData{rpIDHash: data[:32], flags: data[32], signCount: binary.BigEndian.Uint32(data[33:37])}
	rest := data[37:]
	if a.flags&flagAT != 0 {
		if len(rest) < 18 {
			return authData{}, ErrAuthData
		}
		n := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if n == 0 || n > 1023 || len(rest) < n {
			return authData{}, ErrAuthData
		}
		a.credID, rest = rest[:n], rest[n:]
		_, after, err := decodeCBOR(rest)
		if err != nil {
			return authData{}, ErrAuthData
		}
		a.key, rest = rest[:len(rest)-len(after)], after
	}
	if a.flags&flagED != 0 {
		var err error
		if _, rest, err = decodeCBOR(rest); err != nil {
			return authData{}, ErrAuthData
		}
	}
	if len(rest) != 0 || wantCredential != (a.flags&flagAT != 0) {
		return authData{}, ErrAuthData
	}
	return a, nil
}

// checkFlags requires the RP ID hash, user presence and user verification:
// a passkey replaces password and second factor, so the authenticator must
// have checked who holds it.
func (a authData) checkFlags(rpID string) error {
	hash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(a.rpIDHash, hash[:]) || a.flags&flagUP == 0 || a.flags&flagUV == 0 {
		return ErrAuthData
	}
	return nil
}

// Register checks the answer to navigator.credentials.create(). Attestation
// is not evaluated ("none"): the NAS trusts the person who is logged in and
// adds the key, not a particular authenticator model.
func Register(clientDataJSON, attestationObject []byte, want Expect) (Credential, error) {
	if err := checkClientData(clientDataJSON, "webauthn.create", want); err != nil {
		return Credential{}, err
	}
	item, rest, err := decodeCBOR(attestationObject)
	if err != nil || len(rest) != 0 {
		return Credential{}, ErrAuthData
	}
	obj, ok := item.(map[any]any)
	if !ok {
		return Credential{}, ErrAuthData
	}
	raw, ok := obj["authData"].([]byte)
	if !ok {
		return Credential{}, ErrAuthData
	}
	a, err := parseAuthData(raw, true)
	if err != nil {
		return Credential{}, err
	}
	if err := a.checkFlags(want.RPID); err != nil {
		return Credential{}, err
	}
	if _, err := parseCOSEKey(a.key); err != nil {
		return Credential{}, err
	}
	return Credential{
		ID:        append([]byte(nil), a.credID...),
		PublicKey: append([]byte(nil), a.key...),
		SignCount: a.signCount,
		Synced:    a.flags&flagBE != 0,
	}, nil
}

// Login checks the answer to navigator.credentials.get() against a stored
// credential and returns the new signature counter.
func Login(clientDataJSON, authenticatorData, signature []byte, cred Credential, want Expect) (uint32, error) {
	if err := checkClientData(clientDataJSON, "webauthn.get", want); err != nil {
		return 0, err
	}
	a, err := parseAuthData(authenticatorData, false)
	if err != nil {
		return 0, err
	}
	if err := a.checkFlags(want.RPID); err != nil {
		return 0, err
	}
	key, err := parseCOSEKey(cred.PublicKey)
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(clientDataJSON)
	message := append(append([]byte(nil), authenticatorData...), digest[:]...)
	if !key.verify(message, signature) {
		return 0, ErrSignature
	}
	// Counters are optional; synced passkeys always send 0. Once one has
	// counted, it must keep counting up.
	if (a.signCount != 0 || cred.SignCount != 0) && a.signCount <= cred.SignCount {
		return 0, fmt.Errorf("%w: %d after %d", ErrCounter, a.signCount, cred.SignCount)
	}
	return a.signCount, nil
}
