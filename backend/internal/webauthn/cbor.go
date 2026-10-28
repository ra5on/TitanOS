// Package webauthn checks passkey registrations and logins (W3C WebAuthn
// Level 2) with the standard library only: a small CBOR reader, COSE keys
// for ES256, EdDSA and RS256, and the checks of the specification that a
// relying party without attestation needs.
package webauthn

import (
	"encoding/binary"
	"errors"
	"math"
)

var errCBOR = errors.New("webauthn: malformed CBOR")

// maxDepth and maxItems keep a hostile authenticator response small.
const (
	maxDepth = 8
	maxItems = 1024
)

// decodeCBOR reads one data item and returns it with the bytes after it.
// Supported: unsigned and negative integers (int64), byte and text strings,
// arrays ([]any), maps (map[any]any with int64 or string keys), false,
// true and null. Indefinite lengths, tags and floats are refused; WebAuthn
// authenticators do not use them in the structures read here.
func decodeCBOR(data []byte) (any, []byte, error) {
	return decodeItem(data, 0)
}

func decodeItem(data []byte, depth int) (any, []byte, error) {
	if depth > maxDepth || len(data) == 0 {
		return nil, nil, errCBOR
	}
	major, info := data[0]>>5, data[0]&0x1f
	data = data[1:]
	if major == 7 {
		switch info {
		case 20:
			return false, data, nil
		case 21:
			return true, data, nil
		case 22:
			return nil, data, nil
		}
		return nil, nil, errCBOR
	}
	n, data, err := argument(info, data)
	if err != nil {
		return nil, nil, err
	}
	switch major {
	case 0:
		if n > math.MaxInt64 {
			return nil, nil, errCBOR
		}
		return int64(n), data, nil
	case 1:
		if n > math.MaxInt64 {
			return nil, nil, errCBOR
		}
		return -1 - int64(n), data, nil
	case 2, 3:
		if n > uint64(len(data)) {
			return nil, nil, errCBOR
		}
		value := data[:n]
		if major == 3 {
			return string(value), data[n:], nil
		}
		return append([]byte(nil), value...), data[n:], nil
	case 4:
		if n > maxItems {
			return nil, nil, errCBOR
		}
		items := make([]any, 0, n)
		for range n {
			var item any
			if item, data, err = decodeItem(data, depth+1); err != nil {
				return nil, nil, err
			}
			items = append(items, item)
		}
		return items, data, nil
	case 5:
		if n > maxItems {
			return nil, nil, errCBOR
		}
		items := make(map[any]any, n)
		for range n {
			var key, value any
			if key, data, err = decodeItem(data, depth+1); err != nil {
				return nil, nil, err
			}
			switch key.(type) {
			case int64, string:
			default:
				return nil, nil, errCBOR
			}
			if _, dup := items[key]; dup {
				return nil, nil, errCBOR
			}
			if value, data, err = decodeItem(data, depth+1); err != nil {
				return nil, nil, err
			}
			items[key] = value
		}
		return items, data, nil
	}
	return nil, nil, errCBOR
}

// argument reads the length or value that follows the initial byte.
func argument(info byte, data []byte) (uint64, []byte, error) {
	switch {
	case info < 24:
		return uint64(info), data, nil
	case info == 24 && len(data) >= 1:
		return uint64(data[0]), data[1:], nil
	case info == 25 && len(data) >= 2:
		return uint64(binary.BigEndian.Uint16(data)), data[2:], nil
	case info == 26 && len(data) >= 4:
		return uint64(binary.BigEndian.Uint32(data)), data[4:], nil
	case info == 27 && len(data) >= 8:
		return binary.BigEndian.Uint64(data), data[8:], nil
	}
	return 0, nil, errCBOR
}
