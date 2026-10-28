package webauthn

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

// A minimal CBOR encoder for the test authenticator.
func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		b := []byte{major<<5 | 25, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(n))
		return b
	}
}

func cborInt(v int64) []byte {
	if v >= 0 {
		return cborHead(0, uint64(v))
	}
	return cborHead(1, uint64(-1-v))
}

func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }

func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

type authenticator struct {
	ec    *ecdsa.PrivateKey
	ed    ed25519.PrivateKey
	id    []byte
	count uint32
}

func newAuthenticator(t *testing.T, ed bool) *authenticator {
	a := &authenticator{id: []byte("credential-0123456789")}
	if ed {
		_, a.ed, _ = ed25519.GenerateKey(rand.Reader)
	} else {
		a.ec, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	return a
}

func (a *authenticator) cose() []byte {
	if a.ed != nil {
		return cborMap(cborInt(1), cborInt(1), cborInt(3), cborInt(AlgEdDSA), cborInt(-1), cborInt(6), cborInt(-2), cborBytes(a.ed.Public().(ed25519.PublicKey)))
	}
	raw, _ := a.ec.PublicKey.Bytes()
	return cborMap(cborInt(1), cborInt(2), cborInt(3), cborInt(AlgES256), cborInt(-1), cborInt(1), cborInt(-2), cborBytes(raw[1:33]), cborInt(-3), cborBytes(raw[33:]))
}

func (a *authenticator) authData(rpID string, flags byte, attested bool) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := append(hash[:], flags, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(out[33:], a.count)
	if attested {
		out = append(out, make([]byte, 16)...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.id)))
		out = append(append(out, a.id...), a.cose()...)
	}
	return out
}

func clientJSON(kind string, challenge []byte, origin string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": Encoding.EncodeToString(challenge), "origin": origin})
	return data
}

func (a *authenticator) sign(authData, clientData []byte) []byte {
	digest := sha256.Sum256(clientData)
	message := append(append([]byte(nil), authData...), digest[:]...)
	if a.ed != nil {
		return ed25519.Sign(a.ed, message)
	}
	h := sha256.Sum256(message)
	sig, _ := ecdsa.SignASN1(rand.Reader, a.ec, h[:])
	return sig
}

var want = Expect{Challenge: []byte("0123456789abcdef0123"), Origin: "https://nas.example.org", RPID: "nas.example.org"}

func register(t *testing.T, a *authenticator) Credential {
	t.Helper()
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(a.authData(want.RPID, flagUP|flagUV|flagAT|flagBE, true)))
	cred, err := Register(clientJSON("webauthn.create", want.Challenge, want.Origin), att, want)
	if err != nil {
		t.Fatal(err)
	}
	return cred
}

func TestRegisterAndLogin(t *testing.T) {
	for _, ed := range []bool{false, true} {
		a := newAuthenticator(t, ed)
		cred := register(t, a)
		if string(cred.ID) != string(a.id) || !cred.Synced {
			t.Fatalf("credential %+v", cred)
		}
		cd := clientJSON("webauthn.get", want.Challenge, want.Origin)
		ad := a.authData(want.RPID, flagUP|flagUV, false)
		if _, err := Login(cd, ad, a.sign(ad, cd), cred, want); err != nil {
			t.Fatalf("ed=%v: %v", ed, err)
		}
	}
}

func TestLoginRejects(t *testing.T) {
	a := newAuthenticator(t, false)
	cred := register(t, a)
	good := clientJSON("webauthn.get", want.Challenge, want.Origin)
	ad := a.authData(want.RPID, flagUP|flagUV, false)
	cases := map[string]func() (cd, ad, sig []byte){
		"origin": func() ([]byte, []byte, []byte) {
			c := clientJSON("webauthn.get", want.Challenge, "https://evil.example")
			return c, ad, a.sign(ad, c)
		},
		"challenge": func() ([]byte, []byte, []byte) {
			c := clientJSON("webauthn.get", []byte("another-challenge-123"), want.Origin)
			return c, ad, a.sign(ad, c)
		},
		"type": func() ([]byte, []byte, []byte) {
			c := clientJSON("webauthn.create", want.Challenge, want.Origin)
			return c, ad, a.sign(ad, c)
		},
		"rpid": func() ([]byte, []byte, []byte) {
			d := a.authData("evil.example", flagUP|flagUV, false)
			return good, d, a.sign(d, good)
		},
		"no-uv": func() ([]byte, []byte, []byte) {
			d := a.authData(want.RPID, flagUP, false)
			return good, d, a.sign(d, good)
		},
		"signature": func() ([]byte, []byte, []byte) { s := a.sign(ad, good); s[len(s)-1] ^= 1; return good, ad, s },
		"trailing": func() ([]byte, []byte, []byte) {
			d := append(append([]byte(nil), ad...), 0)
			return good, d, a.sign(d, good)
		},
	}
	for name, build := range cases {
		cd, d, sig := build()
		if _, err := Login(cd, d, sig, cred, want); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCounterMustRise(t *testing.T) {
	a := newAuthenticator(t, false)
	a.count = 5
	cred := register(t, a)
	cd := clientJSON("webauthn.get", want.Challenge, want.Origin)
	ad := a.authData(want.RPID, flagUP|flagUV, false)
	if _, err := Login(cd, ad, a.sign(ad, cd), cred, want); !errors.Is(err, ErrCounter) {
		t.Fatalf("same counter accepted: %v", err)
	}
	a.count = 6
	ad = a.authData(want.RPID, flagUP|flagUV, false)
	if n, err := Login(cd, ad, a.sign(ad, cd), cred, want); err != nil || n != 6 {
		t.Fatalf("rising counter: %d %v", n, err)
	}
}

func TestCBORLimits(t *testing.T) {
	for _, bad := range [][]byte{{0x9f}, {0x5b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, {0xa2, 0x01, 0x01, 0x01, 0x01}, {0xc0, 0x00}, {}} {
		if _, _, err := decodeCBOR(bad); err == nil {
			t.Errorf("accepted %x", bad)
		}
	}
}
