package plugins

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// errBadSignature covers everything that keeps a signature from counting.
var errBadSignature = errors.New("plugins: no valid signature")

// publicKey is a minisign public key.
type publicKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

// parsePublicKey reads a minisign public key file: a comment line and a
// line with the key.
func parsePublicKey(text string) (publicKey, error) {
	var key publicKey
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "untrusted comment:") {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(raw) != 42 || string(raw[:2]) != "Ed" {
			return key, errBadSignature
		}
		copy(key.id[:], raw[2:10])
		key.key = ed25519.PublicKey(raw[10:])
		return key, nil
	}
	return key, errBadSignature
}

// verify checks a minisign signature of a message against a set of keys.
//
// The signature file holds the signature of the message and a second one
// over the first plus a comment, which keeps both from being swapped.
func verify(keys []publicKey, message []byte, signature string) error {
	lines := strings.Split(strings.ReplaceAll(signature, "\r", ""), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return errBadSignature
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 74 {
		return errBadSignature
	}
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return errBadSignature
	}
	switch string(raw[:2]) {
	case "ED":
		// The usual form: what is signed is a hash of the message.
		hash := blake2b.Sum512(message)
		message = hash[:]
	case "Ed":
	default:
		return errBadSignature
	}
	comment := strings.TrimPrefix(lines[2], "trusted comment: ")
	for _, key := range keys {
		if !bytes.Equal(key.id[:], raw[2:10]) {
			continue
		}
		if ed25519.Verify(key.key, message, raw[10:]) && ed25519.Verify(key.key, append(bytes.Clone(raw[10:]), comment...), global) {
			return nil
		}
	}
	return errBadSignature
}

// loadKeys reads the public keys TitanOS trusts for plugins.
func loadKeys(dir string) []publicKey {
	var keys []publicKey
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		if key, err := parsePublicKey(string(data)); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}
