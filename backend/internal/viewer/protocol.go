package viewer

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// Limits of the relay (docs/FERNANSICHT.md); the message rules themselves
// are those of the agent (package proto).
const (
	// MaxText is the largest text message from the browser: a clipboard of
	// 1 MiB can take up to six times that as escaped JSON.
	MaxText = proto.MaxTextMessage
	// MaxBrowserMessage is the read limit of the browser connection.
	MaxBrowserMessage = MaxText
	// MaxAgentMessage is the read limit of the agent connection: a key
	// frame of a large screen.
	MaxAgentMessage = 16 << 20
)

// inputTypes are the messages that count as someone at work (they keep
// the view from ending as idle).
var inputTypes = map[string]bool{
	proto.MsgMouse: true, proto.MsgWheel: true, proto.MsgKey: true,
	proto.MsgText: true, proto.MsgClipboard: true, proto.MsgUpload: true,
}

// knownTypes are the text messages a browser may send at all.
var knownTypes = map[string]bool{
	proto.MsgSize: true, proto.MsgMouse: true, proto.MsgWheel: true, proto.MsgKey: true, proto.MsgText: true,
	proto.MsgClipboard: true, proto.MsgKeyframe: true, proto.MsgQuality: true, proto.MsgUpload: true,
}

// ErrMessage is a message that ends the view: not JSON, an unknown "t",
// too large, or a binary message other than an upload piece.
var ErrMessage = errors.New("viewer: message refused")

// CheckClient checks one message from the browser before it goes to the
// agent. forward is false for a known message the agent would ignore
// anyway (a value out of range, proto.ParseClient): it is dropped, the view
// goes on. input tells whether it counts as activity.
func CheckClient(binary bool, data []byte) (forward, input bool, err error) {
	if binary {
		if proto.CheckBinaryToAgent(data) != nil {
			return false, false, ErrMessage
		}
		return true, true, nil
	}
	if len(data) > MaxText {
		return false, false, ErrMessage
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false, false, ErrMessage
	}
	var head struct {
		T string `json:"t"`
	}
	if json.Unmarshal(trimmed, &head) != nil || !knownTypes[head.T] {
		return false, false, ErrMessage
	}
	if _, err := proto.ParseClient(trimmed); err != nil {
		return false, false, nil
	}
	return true, inputTypes[head.T], nil
}
