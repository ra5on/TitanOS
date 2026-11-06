package viewer

import (
	"context"
	"errors"
	"time"

	"github.com/coder/websocket"
)

// DialTimeout is how long titand waits for the agent of a new container.
var DialTimeout = 15 * time.Second

// Dial connects to the agent of the view, retrying while it starts.
func (l *Lease) Dial(ctx context.Context) (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	for {
		attempt, stop := context.WithTimeout(ctx, 3*time.Second)
		conn, _, err := websocket.Dial(attempt, l.AgentURL(), nil)
		stop()
		if err == nil {
			conn.SetReadLimit(MaxAgentMessage)
			return conn, nil
		}
		select {
		case <-ctx.Done():
			// The error of Dial would name the address with the key.
			return nil, errors.New("viewer: the agent in the container did not answer")
		case <-time.After(300 * time.Millisecond):
		}
	}
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
			input, err := CheckClient(kind == websocket.MessageBinary, data)
			if err != nil {
				ends <- EndRefused
				return
			}
			if input && onInput != nil {
				onInput()
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
