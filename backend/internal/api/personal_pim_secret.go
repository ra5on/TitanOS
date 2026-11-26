package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"io/fs"
)

// A backup of the connection profile alone cannot disclose the token. Its
// separate 0600 wrapping key and associated data belong to one account incarnation.
func (s *Server) modelCipher(u auth.User, create bool) (cipher.AEAD, error) {
	path := modelProfilePath(u) + ".key"
	key, err := s.opts.Store.Read(path)
	if errors.Is(err, fs.ErrNotExist) && create {
		key = make([]byte, 32)
		_, _ = rand.Read(key)
		if err = s.opts.Store.Write(path, key); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("model wrapping key unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func (s *Server) sealModelKey(u auth.User, raw string, create bool) (string, error) {
	a, err := s.modelCipher(u, create)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nil, nil, []byte(raw), []byte(modelProfilePath(u)))), nil
}
func (s *Server) openModelKey(u auth.User, sealed string) (string, error) {
	a, err := s.modelCipher(u, false)
	if err != nil {
		return "", err
	}
	b, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	raw, err := a.Open(nil, nil, b, []byte(modelProfilePath(u)))
	return string(raw), err
}
