// Package card reads one vCard (3.0 or 4.0) the way the contacts store keeps
// it: the bytes stay as they came, this only checks them and gives a parsed
// view for display, search and the agent.
package card

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/emersion/go-vcard"
)

// MaxSize is the largest vCard accepted, photo included (TECHNIK 5.3).
const MaxSize = 2 << 20

var (
	// ErrTooLarge means the bytes exceed MaxSize.
	ErrTooLarge = errors.New("vcard too large")
	// ErrInvalid means the bytes are not exactly one well-formed vCard
	// with VERSION 3.0 or 4.0 and a formatted name.
	ErrInvalid = errors.New("invalid vcard")
)

// Parse checks b and returns the parsed card. A file must hold exactly one
// card; a decoder panic on hostile input is reported as ErrInvalid.
func Parse(b []byte) (c vcard.Card, err error) {
	if len(b) > MaxSize {
		return nil, ErrTooLarge
	}
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, fmt.Errorf("%w: decoder failed", ErrInvalid)
		}
	}()
	dec := vcard.NewDecoder(bytes.NewReader(b))
	c, err = dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := dec.Decode(); err != io.EOF {
		return nil, fmt.Errorf("%w: more than one card", ErrInvalid)
	}
	switch c.Value(vcard.FieldVersion) {
	case "3.0", "4.0":
	default:
		return nil, fmt.Errorf("%w: version must be 3.0 or 4.0", ErrInvalid)
	}
	if len(c[vcard.FieldFormattedName]) == 0 {
		return nil, fmt.Errorf("%w: FN missing", ErrInvalid)
	}
	return c, nil
}
