package speech

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// A small client of the Wyoming protocol (the services' own), for the
// sample of a voice, trying speech to text and the KI-Agent. An event is a
// line of JSON {type, data_length, payload_length}, then that many bytes
// of JSON data, then that many bytes of payload (audio).

const (
	maxEventHead = 64 << 10
	maxEventData = 1 << 20
	// maxAudio bounds what is read or sent in one go: ten minutes of
	// 16 kHz, 16 bit, mono.
	maxAudio = 20 << 20
)

type event struct {
	Type    string
	Data    json.RawMessage
	Payload []byte
}

func writeEvent(w io.Writer, typ string, data any, payload []byte) error {
	head := map[string]any{"type": typ, "version": "1.5.2"}
	var body []byte
	if data != nil {
		var err error
		if body, err = json.Marshal(data); err != nil {
			return err
		}
		head["data_length"] = len(body)
	}
	if len(payload) > 0 {
		head["payload_length"] = len(payload)
	}
	line, err := json.Marshal(head)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(line)+1+len(body)+len(payload))
	buf = append(append(append(append(buf, line...), '\n'), body...), payload...)
	_, err = w.Write(buf)
	return err
}

func readEvent(r *bufio.Reader) (event, error) {
	line, err := r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return event{}, errors.New("speech: event head too long")
		}
		return event{}, err
	}
	var head struct {
		Type          string          `json:"type"`
		Data          json.RawMessage `json:"data"`
		DataLength    int             `json:"data_length"`
		PayloadLength int             `json:"payload_length"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return event{}, fmt.Errorf("speech: event head: %w", err)
	}
	if head.DataLength < 0 || head.DataLength > maxEventData || head.PayloadLength < 0 || head.PayloadLength > maxAudio {
		return event{}, errors.New("speech: event too large")
	}
	ev := event{Type: head.Type, Data: head.Data}
	if head.DataLength > 0 {
		ev.Data = make([]byte, head.DataLength)
		if _, err := io.ReadFull(r, ev.Data); err != nil {
			return event{}, err
		}
	}
	if head.PayloadLength > 0 {
		ev.Payload = make([]byte, head.PayloadLength)
		if _, err := io.ReadFull(r, ev.Payload); err != nil {
			return event{}, err
		}
	}
	return ev, nil
}

// Audio is raw PCM with what it is.
type Audio struct {
	PCM      []byte
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

func (a Audio) format() map[string]int {
	return map[string]int{"rate": a.Rate, "width": a.Width, "channels": a.Channels}
}

func dial(ctx context.Context, addr string) (net.Conn, *bufio.Reader, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(2 * time.Minute))
	}
	return conn, bufio.NewReaderSize(conn, maxEventHead), nil
}

// Synthesize lets Piper at addr speak text with a voice ("" is its default).
func Synthesize(ctx context.Context, addr, text, voice string) (Audio, error) {
	conn, r, err := dial(ctx, addr)
	if err != nil {
		return Audio{}, err
	}
	defer conn.Close()
	req := map[string]any{"text": text}
	if voice != "" {
		req["voice"] = map[string]string{"name": voice}
	}
	if err := writeEvent(conn, "synthesize", req, nil); err != nil {
		return Audio{}, err
	}
	var out Audio
	for {
		ev, err := readEvent(r)
		if err != nil {
			return Audio{}, err
		}
		switch ev.Type {
		case "audio-start":
			if err := json.Unmarshal(ev.Data, &out); err != nil {
				return Audio{}, err
			}
		case "audio-chunk":
			if len(out.PCM)+len(ev.Payload) > maxAudio {
				return Audio{}, errors.New("speech: the text is too long to speak in one go")
			}
			out.PCM = append(out.PCM, ev.Payload...)
		case "audio-stop":
			if out.Rate <= 0 || out.Width <= 0 || out.Channels <= 0 || len(out.PCM) == 0 {
				return Audio{}, errors.New("speech: the voice gave no audio")
			}
			return out, nil
		case "error":
			return Audio{}, fmt.Errorf("speech: the voice refused: %s", bytes.TrimSpace(ev.Data))
		}
	}
}

// Transcribe lets faster-whisper at addr write down audio; language is
// "" (as the service is set) or a two-letter code.
func Transcribe(ctx context.Context, addr string, audio Audio, language string) (string, error) {
	if len(audio.PCM) == 0 || len(audio.PCM) > maxAudio {
		return "", errors.New("speech: no audio or too much of it")
	}
	conn, r, err := dial(ctx, addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	req := map[string]any{}
	if language != "" && language != "auto" {
		req["language"] = language
	}
	if err := writeEvent(conn, "transcribe", req, nil); err != nil {
		return "", err
	}
	if err := writeEvent(conn, "audio-start", audio.format(), nil); err != nil {
		return "", err
	}
	for at := 0; at < len(audio.PCM); at += 8192 {
		end := min(at+8192, len(audio.PCM))
		if err := writeEvent(conn, "audio-chunk", audio.format(), audio.PCM[at:end]); err != nil {
			return "", err
		}
	}
	if err := writeEvent(conn, "audio-stop", nil, nil); err != nil {
		return "", err
	}
	for {
		ev, err := readEvent(r)
		if err != nil {
			return "", err
		}
		switch ev.Type {
		case "transcript":
			var t struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(ev.Data, &t); err != nil {
				return "", err
			}
			return t.Text, nil
		case "error":
			return "", fmt.Errorf("speech: the service refused: %s", bytes.TrimSpace(ev.Data))
		}
	}
}

// WAV wraps PCM in a WAV file a browser plays.
func (a Audio) WAV() []byte {
	var b bytes.Buffer
	le := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + len(a.PCM)))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1)) // PCM
	le(uint16(a.Channels))
	le(uint32(a.Rate))
	le(uint32(a.Rate * a.Channels * a.Width))
	le(uint16(a.Channels * a.Width))
	le(uint16(8 * a.Width))
	b.WriteString("data")
	le(uint32(len(a.PCM)))
	b.Write(a.PCM)
	return b.Bytes()
}

// ParseWAV reads a PCM WAV file (what the page records): 8 to 48 kHz, 16
// bit, one or two channels.
func ParseWAV(data []byte) (Audio, error) {
	bad := errors.New("speech: not a PCM WAV file of 16 bit")
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Audio{}, bad
	}
	var out Audio
	seen := false
	for at := 12; at+8 <= len(data); {
		id, size := string(data[at:at+4]), int(binary.LittleEndian.Uint32(data[at+4:at+8]))
		at += 8
		if size < 0 || at+size > len(data) {
			size = len(data) - at // a recorder that could not seek back leaves the size open
		}
		switch id {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(data[at:at+2]) != 1 {
				return Audio{}, bad
			}
			out.Channels = int(binary.LittleEndian.Uint16(data[at+2 : at+4]))
			out.Rate = int(binary.LittleEndian.Uint32(data[at+4 : at+8]))
			out.Width = int(binary.LittleEndian.Uint16(data[at+14:at+16])) / 8
			seen = true
		case "data":
			if !seen || out.Width != 2 || out.Channels < 1 || out.Channels > 2 || out.Rate < 8000 || out.Rate > 48000 || size == 0 || size > maxAudio {
				return Audio{}, bad
			}
			out.PCM = data[at : at+size-size%(out.Width*out.Channels)]
			return out, nil
		}
		at += size + size%2
	}
	return Audio{}, bad
}
