package viewer

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Limits of the messages from the browser to the agent (docs/FERNANSICHT.md).
const (
	// MaxText is the largest text message (clipboard up to 1 MiB plus JSON).
	MaxText = 1<<20 + 4096
	// MaxChunk is the largest piece of an upload, without its header.
	MaxChunk = 256 << 10
	// MaxBrowserMessage is the read limit of the browser connection.
	MaxBrowserMessage = MaxText
	// MaxAgentMessage is the read limit of the agent connection: a key
	// frame of a large screen.
	MaxAgentMessage = 16 << 20
)

// chunkType is the first byte of a binary upload piece.
const chunkType = 0x03

// clientTypes are the text messages a browser may send, and whether each
// counts as someone at work (it keeps the view from ending as idle).
var clientTypes = map[string]bool{
	"size":      false,
	"mouse":     true,
	"wheel":     true,
	"key":       true,
	"text":      true,
	"clipboard": true,
	"keyframe":  false,
	"quality":   false,
	"upload":    true,
}

// ErrMessage is a message the agent must not get.
var ErrMessage = errors.New("viewer: message refused")

// CheckClient checks one message from the browser before it goes to the
// agent and tells whether it is input. Text must be a JSON object of at
// most MaxText bytes with a known "t"; binary must be an upload piece
// (type 0x03, four bytes of upload number, at most MaxChunk of data).
func CheckClient(binary bool, data []byte) (input bool, err error) {
	if binary {
		if len(data) < 5 || len(data) > MaxChunk+5 || data[0] != chunkType {
			return false, ErrMessage
		}
		return true, nil
	}
	if len(data) > MaxText {
		return false, ErrMessage
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false, ErrMessage
	}
	var head struct {
		T string `json:"t"`
	}
	if json.Unmarshal(trimmed, &head) != nil {
		return false, ErrMessage
	}
	counts, known := clientTypes[head.T]
	if !known {
		return false, ErrMessage
	}
	return counts, nil
}
