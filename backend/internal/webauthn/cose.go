package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
)

// COSE algorithm identifiers the NAS accepts, in order of preference.
const (
	AlgES256 = -7
	AlgEdDSA = -8
	AlgRS256 = -257
)

// Algorithms lists them for pubKeyCredParams.
var Algorithms = []int{AlgES256, AlgEdDSA, AlgRS256}

var errKey = errors.New("webauthn: unsupported or malformed public key")

// publicKey is a parsed COSE_Key.
type publicKey struct {
	alg int
	key crypto.PublicKey
}

// parseCOSEKey reads a COSE_Key (RFC 9053) as authenticators send it.
func parseCOSEKey(data []byte) (publicKey, error) {
	item, rest, err := decodeCBOR(data)
	if err != nil || len(rest) != 0 {
		return publicKey{}, errKey
	}
	m, ok := item.(map[any]any)
	if !ok {
		return publicKey{}, errKey
	}
	kty, _ := m[int64(1)].(int64)
	alg, _ := m[int64(3)].(int64)
	switch {
	case kty == 2 && alg == AlgES256:
		crv, _ := m[int64(-1)].(int64)
		x, _ := m[int64(-2)].([]byte)
		y, _ := m[int64(-3)].([]byte)
		if crv != 1 || len(x) != 32 || len(y) != 32 {
			return publicKey{}, errKey
		}
		raw := append(append([]byte{4}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
		if err != nil {
			return publicKey{}, errKey
		}
		return publicKey{alg: AlgES256, key: pub}, nil
	case kty == 1 && alg == AlgEdDSA:
		crv, _ := m[int64(-1)].(int64)
		x, _ := m[int64(-2)].([]byte)
		if crv != 6 || len(x) != ed25519.PublicKeySize {
			return publicKey{}, errKey
		}
		return publicKey{alg: AlgEdDSA, key: ed25519.PublicKey(x)}, nil
	case kty == 3 && alg == AlgRS256:
		n, _ := m[int64(-1)].([]byte)
		e, _ := m[int64(-2)].([]byte)
		if len(n) < 256 || len(n) > 512 || len(e) == 0 || len(e) > 4 {
			return publicKey{}, errKey
		}
		exp := int(new(big.Int).SetBytes(e).Int64())
		if exp < 3 || exp%2 == 0 {
			return publicKey{}, errKey
		}
		return publicKey{alg: AlgRS256, key: &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}}, nil
	}
	return publicKey{}, errKey
}

// verify checks a signature over message.
func (k publicKey) verify(message, signature []byte) bool {
	switch k.alg {
	case AlgES256:
		digest := sha256.Sum256(message)
		return ecdsa.VerifyASN1(k.key.(*ecdsa.PublicKey), digest[:], signature)
	case AlgEdDSA:
		return ed25519.Verify(k.key.(ed25519.PublicKey), message, signature)
	case AlgRS256:
		digest := sha256.Sum256(message)
		return rsa.VerifyPKCS1v15(k.key.(*rsa.PublicKey), crypto.SHA256, digest[:], signature) == nil
	}
	return false
}
