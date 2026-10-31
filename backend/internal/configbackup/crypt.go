package configbackup

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

// A backup can be encrypted with a password (decided 31.10.2026): then a
// downloaded or stolen backup gives away none of the secrets of /config.
// The file starts with the manifest in the clear, so that lists and a new
// stick can show what it is, then salt, nonce and the sealed tar.gz:
//
//	TITANOS-ENC1\n<manifest JSON>\n<salt 16><nonce 24><XChaCha20-Poly1305>
//
// The key comes from the password with scrypt. The password itself lives
// on the stick (system/backup-password), so that saving runs on its own;
// it protects the backups away from the NAS, not the stick.

const encMagic = "TITANOS-ENC1\n"

// passwordFile holds the password of encrypted backups on the stick.
const passwordFile = "system/backup-password"

var (
	// ErrPasswordRequired means the backup is encrypted and no password came.
	ErrPasswordRequired = errors.New("configbackup: the backup is encrypted, a password is needed")
	// ErrWrongPassword means the password does not open the backup.
	ErrWrongPassword = errors.New("configbackup: wrong password for the backup")
)

func backupKey(password string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, 1<<15, 8, 1, chacha20poly1305.KeySize)
}

// seal encrypts a tar.gz with the password; manifest stays readable.
func seal(manifest, archive []byte, password string) ([]byte, error) {
	if bytes.ContainsAny(manifest, "\n\r") {
		var compact bytes.Buffer
		if err := json.Compact(&compact, manifest); err != nil {
			return nil, err
		}
		manifest = compact.Bytes()
	}
	salt := make([]byte, 16)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := backupKey(password, salt)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	header := append(append([]byte(encMagic), manifest...), '\n')
	out := append(append(append(header, salt...), nonce...), aead.Seal(nil, nonce, archive, header)...)
	return out, nil
}

// encrypted reports whether data starts like an encrypted backup.
func encrypted(data []byte) bool { return bytes.HasPrefix(data, []byte(encMagic)) }

// sealedManifest reads the clear manifest of an encrypted backup.
func sealedManifest(r io.Reader) (Manifest, error) {
	head := make([]byte, len(encMagic)+4096)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	if !encrypted(head) {
		return Manifest{}, ErrNotABackup
	}
	line, _, ok := bytes.Cut(head[len(encMagic):], []byte("\n"))
	if !ok {
		return Manifest{}, ErrNotABackup
	}
	var manifest Manifest
	if err := json.Unmarshal(line, &manifest); err != nil || manifest.Format != format {
		return Manifest{}, ErrNotABackup
	}
	return manifest, nil
}

// unseal returns the tar.gz of an encrypted backup.
func unseal(data []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, ErrPasswordRequired
	}
	rest := data[len(encMagic):]
	i := bytes.IndexByte(rest, '\n')
	if i < 0 || len(rest) < i+1+16+chacha20poly1305.NonceSizeX {
		return nil, ErrNotABackup
	}
	header := data[:len(encMagic)+i+1]
	rest = rest[i+1:]
	salt, nonce, sealed := rest[:16], rest[16:16+chacha20poly1305.NonceSizeX], rest[16+chacha20poly1305.NonceSizeX:]
	key, err := backupKey(password, salt)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, sealed, header)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}

// SetPassword keeps the password for encrypted backups on the stick; an
// empty one removes it.
func (m *Manager) SetPassword(password string) error {
	if password == "" {
		err := m.store.Remove(passwordFile)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(password) < 8 || len(password) > 256 {
		return ErrInvalid
	}
	return m.store.Write(passwordFile, []byte(password))
}

// PasswordSet reports whether a password for encrypted backups is kept.
func (m *Manager) PasswordSet() bool {
	data, err := m.store.Read(passwordFile)
	return err == nil && len(data) > 0
}
