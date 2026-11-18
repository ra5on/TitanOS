package store

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// accountRE is the account name rule of auth (as in tmail).
	accountRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// collectionRE: own names; uuidRE: the names iOS gives new calendars.
	collectionRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	uuidRE       = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// MaxNameBytes bounds the decoded name of an entry; MaxDiskName its
// percent-encoded form on the volume (TECHNIK 5.2).
const (
	MaxNameBytes = 200
	MaxDiskName  = 255
)

// ValidAccount reports whether name is an account name.
func ValidAccount(name string) bool { return accountRE.MatchString(name) }

// ValidCollection reports whether id may name a collection: [a-z0-9-]{1,64}
// or a UUID as iOS makes them.
func ValidCollection(id string) bool {
	return collectionRE.MatchString(id) || uuidRE.MatchString(id)
}

// ValidName reports whether name (decoded from the URL) may name an entry
// of the kind: 1–200 bytes of UTF-8, not "." or "..", no leading dot, no
// "/", NUL or control characters, ending in .ics or .vcf, and at most 255
// bytes once encoded for the volume.
func ValidName(kind Kind, name string) bool {
	if len(name) == 0 || len(name) > MaxNameBytes || !utf8.ValidString(name) {
		return false
	}
	if name == "." || name == ".." || name[0] == '.' {
		return false
	}
	for _, r := range name {
		if r == '/' || r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	if !strings.HasSuffix(name, kind.suffix()) || len(name) == len(kind.suffix()) {
		return false
	}
	return len(encodeName(name)) <= MaxDiskName
}

// encodeName is the file name of an entry on the volume: every byte but
// [A-Za-z0-9._~-] percent-encoded.
func encodeName(name string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '~' || c == '-' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// decodeName reverses encodeName; ok is false for a file name encodeName
// never makes (left alone, never served).
func decodeName(kind Kind, file string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(file); i++ {
		c := file[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+2 >= len(file) {
			return "", false
		}
		hi, lo := unhex(file[i+1]), unhex(file[i+2])
		if hi < 0 || lo < 0 {
			return "", false
		}
		b.WriteByte(byte(hi<<4 | lo))
		i += 2
	}
	name := b.String()
	if !ValidName(kind, name) || encodeName(name) != file {
		return "", false
	}
	return name, true
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// photoName is the file of the reduced picture of a contact below photos/:
// the encoded name with .jpg for .vcf (never longer than MaxDiskName).
func photoName(name string) string {
	return strings.TrimSuffix(encodeName(name), Contacts.suffix()) + ".jpg"
}
