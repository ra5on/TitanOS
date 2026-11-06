package viewer

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// DialTimeout is how long titand waits for the agent of a new container.
var DialTimeout = 15 * time.Second

// Dial waits until the agent of the view answers /healthz and connects to
// its stream with the session key in a header.
func (l *Lease) Dial(ctx context.Context) (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	header := http.Header{KeyHeader: {l.Key()}}
	for {
		if l.healthy(ctx) {
			attempt, stop := context.WithTimeout(ctx, 3*time.Second)
			conn, _, err := websocket.Dial(attempt, l.AgentURL(), &websocket.DialOptions{HTTPHeader: header, HTTPClient: agentClient})
			stop()
			if err == nil {
				conn.SetReadLimit(MaxAgentMessage)
				return conn, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("viewer: the agent in the container did not answer")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// agentClient talks to agents on the internal network only, without proxy.
var agentClient = &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

func (l *Lease) healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.HealthURL(), nil)
	if err != nil {
		return false
	}
	res, err := agentClient.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// Download asks the agent for a file the browser in the view downloaded.
func (l *Lease) Download(ctx context.Context, id string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.DownloadURL(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(KeyHeader, l.Key())
	res, err := agentClient.Do(req)
	if err != nil {
		// The error would name the agent's address.
		return nil, errors.New("viewer: the agent did not answer")
	}
	return res, nil
}

// End says why a relay ended.
type End int

const (
	// EndBrowser: the window closed or lost its connection.
	EndBrowser End = iota
	// EndAgent: the agent closed the stream (the container ended).
	EndAgent
	// EndRefused: the browser sent a message the agent must not get.
	EndRefused
	// EndContext: ctx ended (logout, lock, idle, takeover: the caller knows).
	EndContext
)

// Relay passes messages between the browser and the agent until one side
// ends or ctx ends. Messages to the agent are checked (CheckClient); each
// one that counts as input calls onInput. Messages to the browser pass
// unchanged.
//
// The copies do not stop with ctx: a cancelled read would close the
// browser's connection before the caller could say why it ends. The caller
// closes both connections after Relay returned; that ends them.
func Relay(ctx context.Context, browser, agent *websocket.Conn, onInput func()) End {
	done := ctx
	ctx = context.WithoutCancel(ctx)
	ends := make(chan End, 2)
	go func() {
		for {
			kind, data, err := agent.Read(ctx)
			if err != nil {
				ends <- EndAgent
				return
			}
			if browser.Write(ctx, kind, data) != nil {
				ends <- EndBrowser
				return
			}
		}
	}()
	go func() {
		for {
			kind, data, err := browser.Read(ctx)
			if err != nil {
				ends <- EndBrowser
				return
			}
			forward, input, err := CheckClient(kind == websocket.MessageBinary, data)
			if err != nil {
				ends <- EndRefused
				return
			}
			if input && onInput != nil {
				onInput()
			}
			if !forward {
				continue
			}
			if agent.Write(ctx, kind, data) != nil {
				ends <- EndAgent
				return
			}
		}
	}()
	select {
	case end := <-ends:
		if done.Err() != nil && end != EndRefused {
			return EndContext
		}
		return end
	case <-done.Done():
		return EndContext
	}
}
