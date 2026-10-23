package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The watcher keeps one WebSocket connection to Home Assistant while
// something is watched: state changes of the entities automations name,
// and events other parts of TitanOS subscribe to. State changes are kept in
// memory; automations read them like their other event sources, so a short
// change between two looks is not lost. Nothing is replayed after the
// connection was down.

// Transition is one change of an entity's state.
type Transition struct {
	// Mark orders transitions across restarts of titand: start time of the
	// process and a counter, fixed width so that strings compare in order.
	Mark   string
	Entity string
	From   string
	To     string
	Time   time.Time
}

// keepTransitions is how many changes are kept per entity.
const keepTransitions = 20

type watchState struct {
	mu               sync.Mutex
	entities         []string
	events           map[string][]func(json.RawMessage)
	transitions      map[string][]Transition
	boot             int64
	counter          int64
	connected        bool
	lastError        string
	lastConnected    time.Time
	lastDisconnected time.Time
	dropped          uint64
	// MQTT publisher state, kept here for one lock.
	mqttConnected bool
	mqttError     string
	// wake is closed when what is watched changes.
	wake chan struct{}
}

func (w *watchState) init() {
	if w.transitions == nil {
		w.transitions = map[string][]Transition{}
		w.events = map[string][]func(json.RawMessage){}
		w.boot = time.Now().UnixNano()
		w.wake = make(chan struct{})
	}
}

func (w *watchState) changed() {
	close(w.wake)
	w.wake = make(chan struct{})
}

// SetWatched sets the entities whose state changes are recorded.
func (m *Manager) SetWatched(entities []string) {
	entities = slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(entities), func(e string) bool { return !ValidEntity(e) }))))
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	if slices.Equal(entities, m.watch.entities) {
		return
	}
	m.watch.entities = entities
	m.watch.changed()
}

// OnEvent calls handle for every Home Assistant event of a type, e.g.
// mobile_app_notification_action. Handlers run on the watcher's goroutine
// and must return quickly.
func (m *Manager) OnEvent(eventType string, handle func(data json.RawMessage)) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	m.watch.events[eventType] = append(m.watch.events[eventType], handle)
	m.watch.changed()
}

// Transitions returns the recorded changes of an entity after mark, oldest
// first, and the newest mark known for it ("" if none).
func (m *Manager) Transitions(entity, after string) ([]Transition, string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	list := m.watch.transitions[entity]
	newest := ""
	if len(list) > 0 {
		newest = list[len(list)-1].Mark
	}
	var since []Transition
	for _, t := range list {
		if t.Mark > after {
			since = append(since, t)
		}
	}
	return since, newest
}

// WatchStatus says whether the watcher is connected, and the last error.
func (m *Manager) WatchStatus() (bool, string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	return m.watch.connected, m.watch.lastError
}

func (m *Manager) record(entity, from, to string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.counter++
	t := Transition{Mark: fmt.Sprintf("%020d/%020d", m.watch.boot, m.watch.counter), Entity: entity, From: from, To: to, Time: time.Now()}
	list := append(m.watch.transitions[entity], t)
	if len(list) > keepTransitions {
		m.watch.dropped++
		list = list[len(list)-keepTransitions:]
	}
	m.watch.transitions[entity] = list
}

// Run keeps the connection while something is watched, until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		m.watch.mu.Lock()
		m.watch.init()
		wake := m.watch.wake
		entities := slices.Clone(m.watch.entities)
		var events []string
		for e := range m.watch.events {
			events = append(events, e)
		}
		m.watch.mu.Unlock()
		settingsChanged := m.Changed()
		if !m.Active() || len(entities) == 0 && len(events) == 0 {
			m.setConnected(false, "")
			select {
			case <-ctx.Done():
			case <-wake:
			case <-settingsChanged:
			}
			continue
		}
		started := time.Now()
		err := m.session(ctx, entities, events, wake, settingsChanged)
		m.setConnected(false, errorText(err))
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errResubscribe) {
			backoff = 5 * time.Second
			continue
		}
		if time.Since(started) > time.Minute {
			backoff = 5 * time.Second
		}
		slog.Warn("Home Assistant connection lost", "err", err, "retry", backoff)
		select {
		case <-ctx.Done():
		case <-settingsChanged:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

func errorText(err error) string {
	if err == nil || errors.Is(err, errResubscribe) {
		return ""
	}
	return err.Error()
}

func (m *Manager) setConnected(connected bool, lastError string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	if connected && !m.watch.connected {
		m.watch.lastConnected = time.Now()
	}
	if !connected && m.watch.connected {
		m.watch.lastDisconnected = time.Now()
	}
	m.watch.connected, m.watch.lastError = connected, lastError
}

var errResubscribe = errors.New("what is watched changed")

// session is one connection: authenticate, subscribe, read until something
// changes or the connection breaks.
func (m *Manager) session(ctx context.Context, entities, events []string, wake, settingsChanged <-chan struct{}) error {
	m.mu.Lock()
	s, client := m.settings, m.client
	m.mu.Unlock()
	address := "ws" + strings.TrimPrefix(s.URL, "http") + "/api/websocket"
	// The WebSocket library wants no client timeout; the context bounds the dial.
	wsClient := *client
	wsClient.Timeout = 0
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dialCtx, address, &websocket.DialOptions{HTTPClient: &wsClient})
	cancel()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer conn.CloseNow()
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
	write := func(v any) error {
		data, _ := json.Marshal(v)
		writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return conn.Write(writeCtx, websocket.MessageText, data)
	}
	var hello struct{ Type string }
	if err := read(&hello); err != nil || hello.Type != "auth_required" {
		return fmt.Errorf("%w: no greeting: %v", ErrUnreachable, err)
	}
	if err := write(map[string]string{"type": "auth", "access_token": s.Token}); err != nil {
		return err
	}
	var auth struct{ Type, Message string }
	if err := read(&auth); err != nil {
		return err
	}
	if auth.Type != "auth_ok" {
		return fmt.Errorf("%w: the token was refused: %s", ErrUnreachable, auth.Message)
	}
	id := 0
	triggerID := -1
	eventIDs := map[int]string{}
	if len(entities) > 0 {
		id++
		triggerID = id
		if err := write(map[string]any{"id": id, "type": "subscribe_trigger", "trigger": map[string]any{"platform": "state", "entity_id": entities}}); err != nil {
			return err
		}
	}
	for _, e := range events {
		id++
		eventIDs[id] = e
		if err := write(map[string]any{"id": id, "type": "subscribe_events", "event_type": e}); err != nil {
			return err
		}
	}
	m.setConnected(true, "")

	// Reading happens on its own goroutine; this one waits for changes and
	// keeps the connection alive.
	type message struct {
		ID      int    `json:"id"`
		Type    string `json:"type"`
		Success *bool  `json:"success"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Event json.RawMessage `json:"event"`
	}
	messages := make(chan message, 64)
	readErr := make(chan error, 1)
	readCtx, stopReading := context.WithCancel(ctx)
	defer stopReading()
	go func() {
		for {
			_, data, err := conn.Read(readCtx)
			if err != nil {
				readErr <- err
				return
			}
			var msg message
			if json.Unmarshal(data, &msg) == nil {
				select {
				case messages <- msg:
				case <-readCtx.Done():
					return
				}
			}
		}
	}()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return ctx.Err()
		case <-wake:
			conn.Close(websocket.StatusNormalClosure, "")
			return errResubscribe
		case <-settingsChanged:
			conn.Close(websocket.StatusNormalClosure, "")
			return errResubscribe
		case err := <-readErr:
			return fmt.Errorf("%w: %v", ErrUnreachable, err)
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return fmt.Errorf("%w: no answer: %v", ErrUnreachable, err)
			}
		case msg := <-messages:
			switch {
			case msg.Type == "result" && msg.Success != nil && !*msg.Success:
				detail := ""
				if msg.Error != nil {
					detail = msg.Error.Message
				}
				return fmt.Errorf("%w: subscription refused: %s", ErrUnreachable, detail)
			case msg.Type == "event" && msg.ID == triggerID:
				var ev struct {
					Variables struct {
						Trigger struct {
							EntityID  string `json:"entity_id"`
							FromState *State `json:"from_state"`
							ToState   *State `json:"to_state"`
						} `json:"trigger"`
					} `json:"variables"`
				}
				if json.Unmarshal(msg.Event, &ev) == nil {
					tr := ev.Variables.Trigger
					from, to := "", ""
					if tr.FromState != nil {
						from = tr.FromState.State
					}
					if tr.ToState != nil {
						to = tr.ToState.State
					}
					// Only real state changes, not attribute updates.
					if tr.EntityID != "" && from != to {
						m.record(tr.EntityID, from, to)
					}
				}
			case msg.Type == "event":
				eventType, ok := eventIDs[msg.ID]
				if !ok {
					continue
				}
				var ev struct {
					Data json.RawMessage `json:"data"`
				}
				if json.Unmarshal(msg.Event, &ev) != nil {
					continue
				}
				m.watch.mu.Lock()
				handlers := slices.Clone(m.watch.events[eventType])
				m.watch.mu.Unlock()
				for _, handle := range handlers {
					handle(ev.Data)
				}
			}
		}
	}
}

// FireEvent fires an event in Home Assistant, e.g. titanos_answer.
func (m *Manager) FireEvent(ctx context.Context, eventType string, data any) error {
	if !ValidService(eventType) {
		return invalid("event type")
	}
	return m.do(ctx, http.MethodPost, "/api/events/"+eventType, data, nil)
}

// Match says whether a transition fits a trigger: to and from are states to
// look for (empty: any); above and below are thresholds crossed by numeric
// states (nil: none).
func Match(t Transition, from, to string, above, below *float64) bool {
	if to != "" && t.To != to || from != "" && t.From != from {
		return false
	}
	number := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}
	if above != nil {
		now, ok := number(t.To)
		before, wasNumber := number(t.From)
		if !ok || now <= *above || wasNumber && before > *above {
			return false
		}
	}
	if below != nil {
		now, ok := number(t.To)
		before, wasNumber := number(t.From)
		if !ok || now >= *below || wasNumber && before < *below {
			return false
		}
	}
	return true
}

// Diagnostics contains no entity contents or credentials. Dropped counts ring
// buffer eviction, not necessarily missed events; clients must distinguish it.
func (m *Manager) Diagnostics() map[string]any {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	return map[string]any{"connected": m.watch.connected, "lastConnected": m.watch.lastConnected, "lastDisconnected": m.watch.lastDisconnected, "evictedTransitions": m.watch.dropped, "watchedEntities": len(m.watch.entities), "replaysOutages": false}
}
