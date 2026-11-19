package cardweb

import (
	"bytes"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
)

// Piece is one card of a .vcf file, its bytes as they came.
type Piece struct {
	Data []byte
	// UID, FN and the first EMAIL as read ("" when the card has none or
	// cannot be read: the store says why it refuses it).
	UID, FN, Email string
}

// Key is the duplicate key of a card without UID: FN and first e-mail
// address, case aside ("" without FN).
func Key(fn, email string) string {
	fn = strings.ToLower(strings.TrimSpace(fn))
	if fn == "" {
		return ""
	}
	return fn + "\x00" + strings.ToLower(strings.TrimSpace(email))
}

// SplitVCF cuts a .vcf file into its cards at the lines BEGIN:VCARD and
// END:VCARD (case aside, nested cards of vCard 2.1 AGENT not known), each
// with the bytes as they came. Text outside a card is skipped; a card
// without END is cut at the next BEGIN or the end and fails in the store.
func SplitVCF(data []byte) []Piece {
	// 1.15 (115-7): an export may start with a UTF-8 BOM; TrimSpace keeps
	// U+FEFF, so the first BEGIN:VCARD was not found and its card was lost.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var out []Piece
	start := -1
	depth := 0
	pos := 0
	for pos < len(data) {
		end := bytes.IndexByte(data[pos:], '\n')
		next := len(data)
		if end >= 0 {
			next = pos + end + 1
		}
		line := strings.TrimSpace(string(data[pos:next]))
		switch {
		case strings.EqualFold(line, "BEGIN:VCARD"):
			if depth == 0 {
				start = pos
			} else if start >= 0 { // no END before: cut here
				out = append(out, piece(data[start:pos]))
				start = pos
				depth = 0
			}
			depth++
		case strings.EqualFold(line, "END:VCARD") && start >= 0:
			depth--
			if depth <= 0 {
				out = append(out, piece(data[start:next]))
				start, depth = -1, 0
			}
		}
		pos = next
	}
	if start >= 0 {
		out = append(out, piece(data[start:]))
	}
	return out
}

func piece(b []byte) Piece {
	p := Piece{Data: b}
	if d, err := card.ParseDoc(b); err == nil {
		if l := d.First("UID"); l != nil {
			p.UID = strings.TrimSpace(card.UnescapeText(l.Value))
		}
		if l := d.First("FN"); l != nil {
			p.FN = strings.TrimSpace(card.UnescapeText(l.Value))
		}
		if l := d.First("EMAIL"); l != nil {
			p.Email = strings.TrimSpace(card.UnescapeText(l.Value))
		}
	}
	return p
}

// WithUID gives a card a UID line (a card of the import without one);
// unchanged when it cannot be read.
func WithUID(data []byte, uid string) []byte {
	d, err := card.ParseDoc(data)
	if err != nil {
		return data
	}
	d.Set("UID", card.EscapeText(uid))
	return d.Bytes()
}
