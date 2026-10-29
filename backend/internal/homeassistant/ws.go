package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// wsOpen connects to the WebSocket API of Home Assistant and logs in with
// the stored token. The caller closes the connection.
func (m *Manager) wsOpen(ctx context.Context) (*websocket.Conn, error) {
	m.mu.Lock()
	s, client := m.settings, m.client
	m.mu.Unlock()
	if s.URL == "" || s.Token == "" {
		return nil, ErrOff
	}
	address := "ws" + strings.TrimPrefix(s.URL, "http") + "/api/websocket"
	// The WebSocket library wants no client timeout; the context bounds the dial.
	wsClient := *client
	wsClient.Timeout = 0
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dialCtx, address, &websocket.DialOptions{HTTPClient: &wsClient})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	conn.SetReadLimit(4 << 20)
	read := func(v any) error {
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, data, err := conn.Read(readCtx)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, v)
	}
	var hello struct{ Type string }
	if err := read(&hello); err != nil || hello.Type != "auth_required" {
		conn.CloseNow()
		return nil, fmt.Errorf("%w: no greeting: %v", ErrUnreachable, err)
	}
	data, _ := json.Marshal(map[string]string{"type": "auth", "access_token": s.Token})
	writeCtx, cancelWrite := context.WithTimeout(ctx, 10*time.Second)
	err = conn.Write(writeCtx, websocket.MessageText, data)
	cancelWrite()
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	var auth struct{ Type, Message string }
	if err := read(&auth); err != nil {
		conn.CloseNow()
		return nil, err
	}
	if auth.Type != "auth_ok" {
		conn.CloseNow()
		return nil, fmt.Errorf("%w: the token was refused: %s", ErrUnreachable, auth.Message)
	}
	return conn, nil
}

// CommandError is an error Home Assistant answered a command with.
type CommandError struct {
	Code, Message string
}

func (e *CommandError) Error() string {
	return "homeassistant: " + e.Code + ": " + e.Message
}

// ErrNoSupervisor means Home Assistant runs without the Supervisor
// (Container or Core installation): it has no network storage of its own.
var ErrNoSupervisor = errors.New("homeassistant: no Supervisor (network storage needs Home Assistant OS or Supervised)")

// command sends one command over a connection of its own and returns its
// result. timeout bounds the whole exchange.
func (m *Manager) command(ctx context.Context, timeout time.Duration, cmd map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := m.wsOpen(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.CloseNow()
	msg := map[string]any{"id": 1}
	for k, v := range cmd {
		msg[k] = v
	}
	data, _ := json.Marshal(msg)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, err
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		var answer struct {
			ID      int             `json:"id"`
			Type    string          `json:"type"`
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &answer) != nil || answer.ID != 1 || answer.Type != "result" {
			continue
		}
		conn.Close(websocket.StatusNormalClosure, "")
		if !answer.Success {
			if answer.Error == nil {
				return nil, &CommandError{Code: "unknown_error"}
			}
			return nil, &CommandError{Code: answer.Error.Code, Message: answer.Error.Message}
		}
		return answer.Result, nil
	}
}

// supervisor calls the Supervisor API through Home Assistant, as its own
// web interface does (method in lower case: get, post); the token must
// belong to an administrator.
func (m *Manager) supervisor(ctx context.Context, method, endpoint string, data any) (json.RawMessage, error) {
	cmd := map[string]any{"type": "supervisor/api", "endpoint": endpoint, "method": method}
	if data != nil {
		cmd["data"] = data
	}
	result, err := m.command(ctx, 90*time.Second, cmd)
	var ce *CommandError
	// Without the Supervisor (Container, Core) the command does not exist.
	if errors.As(err, &ce) && ce.Code == "unknown_command" {
		return nil, ErrNoSupervisor
	}
	return result, err
}
