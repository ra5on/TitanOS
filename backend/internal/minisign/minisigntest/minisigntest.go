// Package minisigntest makes minisign keys and signatures the way the tool
// does, for tests.
package minisigntest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"

	"golang.org/x/crypto/blake2b"
)

// Signer is a key pair.
type Signer struct {
	id      [8]byte
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

// New makes a fresh key pair.
func New() Signer {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	s := Signer{public: public, private: private}
	rand.Read(s.id[:])
	return s
}

// PublicFile is the content of the .pub file.
func (s Signer) PublicFile() string {
	return "untrusted comment: test key\n" + base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), s.id[:]...), s.public...)) + "\n"
}

// Sign returns the content of the .minisig file for message (prehashed).
func (s Signer) Sign(message []byte) string {
	hash := blake2b.Sum512(message)
	signature := ed25519.Sign(s.private, hash[:])
	comment := "timestamp:1 file:manifest.json"
	global := ed25519.Sign(s.private, append(append([]byte{}, signature...), comment...))
	return "untrusted comment: signature\n" + base64.StdEncoding.EncodeToString(append(append([]byte("ED"), s.id[:]...), signature...)) +
		"\ntrusted comment: " + comment + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}
