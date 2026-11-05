package homeassistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"regexp"
	"slices"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Several Home Assistant servers can be connected (owner request of
// 5.11.2026), each with everything of its own: address, token, releases,
// the account its buttons act for, MQTT and storage. The first connection
// stays in settings.json, so the base and the previous app version keep
// reading it; the others are kept in connections/<id>.json and listed in
// connections.json, which older versions do not know and leave alone.

// PrimaryID names the first connection, the one in settings.json.
const PrimaryID = "main"

const connectionsFile = "homeassistant/connections.json"

// MaxConnections bounds the connections, the first one included.
const MaxConnections = 8

var (
	// ErrNoConnection is a connection that does not exist.
	ErrNoConnection = errors.New("homeassistant: no such connection")
	// ErrPrimary is the first connection, which cannot be removed.
	ErrPrimary = errors.New("homeassistant: the first connection cannot be removed")
	// ErrTooMany is one connection too many.
	ErrTooMany = errors.New("homeassistant: too many connections")
	connID     = regexp.MustCompile(`^[a-z0-9]{8}$`)
)

type connectionList struct {
	Connections []string `json:"connections"`
}

// Hub holds every connection.
type Hub struct {
	store   *config.Store
	primary *Manager

	mu      sync.Mutex
	extra   []*Manager
	changed chan struct{}
	// runner carries out buttons of all connections: at most two at a time
	// and one per target, whichever Home Assistant pressed them.
	runner     *commandRunner
	runnerOnce sync.Once
}

// NewHub returns the connections with primary as the first one.
func NewHub(store *config.Store, primary *Manager) *Hub {
	h := &Hub{store: store, primary: primary, changed: make(chan struct{})}
	primary.hub = h
	return h
}

func connectionFile(id string) string { return "homeassistant/connections/" + id + ".json" }

// Load reads the further connections; the primary is loaded on its own.
// A connection whose file cannot be read is left out and logged.
func (h *Hub) Load() error {
	var list connectionList
	if err := h.store.ReadJSON(connectionsFile, &list); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var extra []*Manager
	for _, id := range list.Connections {
		if !connID.MatchString(id) || slices.ContainsFunc(extra, func(m *Manager) bool { return m.id == id }) || len(extra)+1 >= MaxConnections {
			continue
		}
		m := h.newManager(id)
		if err := m.Load(); err != nil {
			slog.Warn("Home Assistant connection not readable", "connection", id, "err", err)
			continue
		}
		extra = append(extra, m)
	}
	h.mu.Lock()
	h.extra = extra
	h.notify()
	h.mu.Unlock()
	return nil
}

func (h *Hub) newManager(id string) *Manager {
	m := NewManager(h.store)
	m.id, m.file, m.hub = id, connectionFile(id), h
	return m
}

func (h *Hub) notify() {
	close(h.changed)
	h.changed = make(chan struct{})
}

// Changed is closed when a connection is added or removed.
func (h *Hub) Changed() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.changed
}

// Primary is the first connection.
func (h *Hub) Primary() *Manager { return h.primary }

// Get returns a connection by its ID; "" is the first one.
func (h *Hub) Get(id string) *Manager {
	if id == "" || id == PrimaryID {
		return h.primary
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.extra {
		if m.id == id {
			return m
		}
	}
	return nil
}

// All lists the connections, the first one first.
func (h *Hub) All() []*Manager {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*Manager{h.primary}, h.extra...)
}

// Active lists the connections that are set up and switched on.
func (h *Hub) Active() []*Manager {
	return slices.DeleteFunc(h.All(), func(m *Manager) bool { return !m.Active() })
}

func (h *Hub) saveList(extra []*Manager) error {
	list := connectionList{Connections: []string{}}
	for _, m := range extra {
		list.Connections = append(list.Connections, m.id)
	}
	return h.store.WriteJSON(connectionsFile, list)
}

// Add makes a new, empty connection with a name. Its settings are set
// with SetSettings like those of the first one.
func (h *Hub) Add(name string) (*Manager, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.extra)+1 >= MaxConnections {
		return nil, ErrTooMany
	}
	b := make([]byte, 4)
	var id string
	for {
		_, _ = rand.Read(b)
		id = hex.EncodeToString(b)
		if !slices.ContainsFunc(h.extra, func(m *Manager) bool { return m.id == id }) {
			break
		}
	}
	m := h.newManager(id)
	m.loaded = true
	mqttDefaults(&m.settings.MQTT)
	if err := m.SetSettings(Settings{Name: name, MQTT: m.settings.MQTT}); err != nil {
		return nil, err
	}
	extra := append(slices.Clone(h.extra), m)
	if err := h.saveList(extra); err != nil {
		_ = h.store.Remove(m.file) // not listed: an orphan file would only take space
		return nil, err
	}
	h.extra = extra
	h.notify()
	return m, nil
}

// Remove takes a connection out. Its runners end with the context the
// hub's Run gave them; what it set up in Home Assistant is the caller's.
func (h *Hub) Remove(id string) error {
	if id == "" || id == PrimaryID {
		return ErrPrimary
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	at := slices.IndexFunc(h.extra, func(m *Manager) bool { return m.id == id })
	if at < 0 {
		return ErrNoConnection
	}
	extra := slices.Delete(slices.Clone(h.extra), at, at+1)
	if err := h.saveList(extra); err != nil {
		return err
	}
	removed := h.extra[at]
	h.extra = extra
	if err := h.store.Remove(removed.file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("settings of a removed Home Assistant connection left on the stick", "connection", id, "err", err)
	}
	h.notify()
	return nil
}

// Run starts start for every connection, also those added later, each
// with a context that ends when the connection is removed or ctx ends.
func (h *Hub) Run(ctx context.Context, start func(ctx context.Context, m *Manager)) {
	running := map[*Manager]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cancel := range running {
			cancel()
		}
		wg.Wait()
	}()
	for {
		changed := h.Changed()
		all := h.All()
		for m, cancel := range running {
			if !slices.Contains(all, m) {
				cancel()
				delete(running, m)
			}
		}
		for _, m := range all {
			if _, ok := running[m]; ok {
				continue
			}
			mctx, cancel := context.WithCancel(ctx)
			running[m] = cancel
			wg.Add(1)
			go func() {
				defer wg.Done()
				start(mctx, m)
			}()
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
	}
}

// commandRunnerFor is the runner of buttons: the hub's for all its
// connections, else one of the manager's own.
func (m *Manager) commandRunnerFor(commands Commander) *commandRunner {
	if m.hub == nil {
		return newCommandRunner(commands)
	}
	m.hub.runnerOnce.Do(func() { m.hub.runner = newCommandRunner(commands) })
	return m.hub.runner
}

type connectionKey struct{}

// WithConnection marks ctx as coming from a connection, e.g. a button
// pressed in its Home Assistant.
func WithConnection(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, connectionKey{}, m)
}

// ConnectionFrom is the connection WithConnection put into ctx, or nil.
func ConnectionFrom(ctx context.Context) *Manager {
	m, _ := ctx.Value(connectionKey{}).(*Manager)
	return m
}

// ActiveOn says whether connection id is set up and switched on.
func (h *Hub) ActiveOn(id string) bool {
	m := h.Get(id)
	return m != nil && m.Active()
}

// NotifyOn sends a message through a notify service of connection id.
func (h *Hub) NotifyOn(ctx context.Context, id, service, title, message string, data map[string]any) error {
	m := h.Get(id)
	if m == nil {
		return ErrNoConnection
	}
	return m.Notify(ctx, service, title, message, data)
}
