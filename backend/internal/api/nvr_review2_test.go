package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// The live socket passes only go2rtc's MSE messages from the browser to
// Frigate: a viewer cannot turn it into a WebRTC session of its own (the
// second review, 12.11.2026). WebRTC signalling goes only through /talk,
// and only that.
func TestNVRSocketsPassOnlyTheirMessages(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer up.CloseNow()
		for {
			kind, data, err := up.Read(r.Context())
			if err != nil {
				return
			}
			if err := up.Write(r.Context(), kind, append([]byte("frigate:"), data...)); err != nil {
				return
			}
			// Video goes the other way as it comes.
			if err := up.Write(r.Context(), websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
				return
			}
		}
	})
	dial := nvrLiveDial
	t.Cleanup(func() { nvrLiveDial = dial })
	upstream := newLocalServer(t, echo)
	nvrLiveDial = func(ctx context.Context, address string, header http.Header) (*websocket.Conn, error) {
		u, _ := url.Parse(address)
		return dial(ctx, "ws"+strings.TrimPrefix(upstream, "http")+u.RequestURI(), header)
	}
	send := func(conn *websocket.Conn, ctx context.Context, kind websocket.MessageType, data string) {
		t.Helper()
		if err := conn.Write(ctx, kind, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	conn, ctx := liveDial(t, ts, ts.client, "/nvr/cameras/haustuer/live")
	send(conn, ctx, websocket.MessageText, `{"type":"webrtc/offer","value":"v=0"}`)
	send(conn, ctx, websocket.MessageText, `{"type":"webrtc/candidate","value":"candidate:1"}`)
	send(conn, ctx, websocket.MessageText, `{"type":"mse","type":"webrtc/offer","value":"v=0"}`)
	send(conn, ctx, websocket.MessageText, `{"type":"mp4","value":"avc1"}`)
	send(conn, ctx, websocket.MessageText, `x`)
	send(conn, ctx, websocket.MessageBinary, "\x01\x02\x03")
	send(conn, ctx, websocket.MessageText, `{"type":"mse","value":"avc1.640029,mp4a.40.2"}`)
	if _, data, err := conn.Read(ctx); err != nil || string(data) != `frigate:{"type":"mse","value":"avc1.640029,mp4a.40.2"}` {
		t.Errorf("live: the first message that reached Frigate: %s %v", data, err)
	}
	if kind, data, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary || len(data) != 3 {
		t.Errorf("live: Frigate's video: %v %v %v", kind, data, err)
	}
	talk, tctx := liveDial(t, ts, ts.client, "/nvr/cameras/haustuer/talk")
	send(talk, tctx, websocket.MessageText, `{"type":"mse","value":"avc1"}`)
	send(talk, tctx, websocket.MessageBinary, "\x01")
	send(talk, tctx, websocket.MessageText, `{"type":"webrtc/offer","value":"v=0"}`)
	if _, data, err := talk.Read(tctx); err != nil || string(data) != `frigate:{"type":"webrtc/offer","value":"v=0"}` {
		t.Errorf("talk: the first message that reached Frigate: %s %v", data, err)
	}
}
