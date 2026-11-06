package protect

import (
	"bytes"
	"io"
	"math"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SampleSize is how much of a rewritten file is read to judge it.
const SampleSize = 8 << 10

// Entropy is the Shannon entropy of data in bits per byte (0–8).
func Entropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	var counts [256]int
	for _, b := range data {
		counts[b]++
	}
	n := float64(len(data))
	h := 0.0
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// magics are the first bytes a file of an extension starts with.
var magics = map[string][][]byte{
	".pdf":  {[]byte("%PDF")},
	".zip":  {[]byte("PK\x03\x04"), []byte("PK\x05\x06")},
	".docx": {[]byte("PK\x03\x04")}, ".xlsx": {[]byte("PK\x03\x04")}, ".pptx": {[]byte("PK\x03\x04")},
	".odt": {[]byte("PK\x03\x04")}, ".ods": {[]byte("PK\x03\x04")}, ".odp": {[]byte("PK\x03\x04")}, ".epub": {[]byte("PK\x03\x04")},
	".jpg": {{0xff, 0xd8, 0xff}}, ".jpeg": {{0xff, 0xd8, 0xff}},
	".png": {[]byte("\x89PNG")},
	".gif": {[]byte("GIF8")},
	".bmp": {[]byte("BM")},
	".tif": {[]byte("II*\x00"), []byte("MM\x00*")}, ".tiff": {[]byte("II*\x00"), []byte("MM\x00*")},
	".doc": {{0xd0, 0xcf, 0x11, 0xe0}}, ".xls": {{0xd0, 0xcf, 0x11, 0xe0}}, ".ppt": {{0xd0, 0xcf, 0x11, 0xe0}}, ".msg": {{0xd0, 0xcf, 0x11, 0xe0}},
	".7z":  {[]byte("7z\xbc\xaf\x27\x1c")},
	".rar": {[]byte("Rar!")},
	".gz":  {{0x1f, 0x8b}}, ".tgz": {{0x1f, 0x8b}},
	".xz":   {[]byte("\xfd7zXZ")},
	".zst":  {{0x28, 0xb5, 0x2f, 0xfd}},
	".bz2":  {[]byte("BZh")},
	".flac": {[]byte("fLaC")},
	".ogg":  {[]byte("OggS")}, ".opus": {[]byte("OggS")},
	".mkv": {{0x1a, 0x45, 0xdf, 0xa3}}, ".webm": {{0x1a, 0x45, 0xdf, 0xa3}},
	".sqlite": {[]byte("SQLite format 3")}, ".db": {[]byte("SQLite format 3")},
	".rtf": {[]byte("{\\rtf")},
}

// offsetMagics are signatures not at the start (ISO base media: ftyp at 4).
var offsetMagics = map[string]struct {
	at  int
	sig []byte
}{
	".mp4": {4, []byte("ftyp")}, ".m4a": {4, []byte("ftyp")}, ".m4v": {4, []byte("ftyp")}, ".mov": {4, []byte("ftyp")},
	".heic": {4, []byte("ftyp")}, ".heif": {4, []byte("ftyp")}, ".avif": {4, []byte("ftyp")}, ".3gp": {4, []byte("ftyp")},
	".webp": {8, []byte("WEBP")}, ".wav": {8, []byte("WAVE")}, ".avi": {8, []byte("AVI ")},
}

// textExtensions hold text: low entropy, no signature.
var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".log": true, ".json": true, ".xml": true, ".yaml": true,
	".yml": true, ".ini": true, ".conf": true, ".cfg": true, ".toml": true, ".html": true, ".htm": true, ".css": true,
	".js": true, ".ts": true, ".go": true, ".py": true, ".sh": true, ".c": true, ".h": true, ".cpp": true, ".java": true,
	".rs": true, ".php": true, ".rb": true, ".sql": true, ".svg": true, ".ics": true, ".vcf": true, ".eml": true, ".srt": true,
}

// LooksEncrypted judges the first bytes of a file with a name: the content
// no longer matches what the extension promises and it is as random as
// encrypted data. Files whose extension promises nothing are never judged
// (an encrypted container or a download may look like that).
func LooksEncrypted(name string, sample []byte) bool {
	if len(sample) < 512 {
		return false
	}
	e := ext(path.Base(name))
	if textExtensions[e] {
		// Text, even UTF-16, stays far below 6 bits per byte.
		return Entropy(sample) > 7.2
	}
	if sigs, ok := magics[e]; ok {
		for _, sig := range sigs {
			if bytes.HasPrefix(sample, sig) {
				return false
			}
		}
		return Entropy(sample) > 7.5
	}
	if o, ok := offsetMagics[e]; ok {
		if len(sample) >= o.at+len(o.sig) && bytes.Equal(sample[o.at:o.at+len(o.sig)], o.sig) {
			return false
		}
		return Entropy(sample) > 7.5
	}
	return false
}

// Judgeable reports whether LooksEncrypted can say anything about a name,
// so the file need not be read otherwise.
func Judgeable(name string) bool {
	e := ext(path.Base(name))
	if textExtensions[e] {
		return true
	}
	_, ok := magics[e]
	_, ok2 := offsetMagics[e]
	return ok || ok2
}

// Sampler reads the start of rewritten files, at most PerSecond files a
// second; the disk is awake (it was just written to), but the guard must
// never compete with the clients for it.
type Sampler struct {
	PerSecond int
	mu        sync.Mutex
	second    time.Time
	used      int
}

// Read returns the first SampleSize bytes of a file, or false when the
// budget of this second is spent or the file cannot be read.
func (s *Sampler) Read(file string, now time.Time) ([]byte, bool) {
	s.mu.Lock()
	if sec := now.Truncate(time.Second); !sec.Equal(s.second) {
		s.second, s.used = sec, 0
	}
	limit := s.PerSecond
	if limit <= 0 {
		limit = 50
	}
	if s.used >= limit {
		s.mu.Unlock()
		return nil, false
	}
	s.used++
	s.mu.Unlock()
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOATIME, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	buf := make([]byte, SampleSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false
	}
	return buf[:n], true
}

// Hidden reports whether a path inside a share belongs to titand or Samba
// (.titanos, recycle bin, snapshots), whose writes are never judged.
func Hidden(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == ".titanos" || part == ".recycle" || part == ".snapshots" || strings.HasPrefix(part, ".titanos-") {
			return true
		}
	}
	return false
}
