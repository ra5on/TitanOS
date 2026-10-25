package auth

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"unicode/utf16"

	//lint:ignore SA1019 SMB NT hashes mandate MD4; TitanOS web passwords use Argon2id.
	"golang.org/x/crypto/md4"
)

// ntHash returns the NT hash of a password: MD4 over its UTF-16LE encoding,
// as upper-case hex. Samba checks SMB logins against it.
func ntHash(password string) string {
	units := utf16.Encode([]rune(password))
	buf := make([]byte, 2*len(units))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(buf[2*i:], unit)
	}
	h := md4.New()
	h.Write(buf)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}
