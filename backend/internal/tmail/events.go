package tmail

import "sync"

// Event is one Server-Sent Event for the open web UI of an account:
// "mail" (new mail {account, folder, uid}), "status" (state of a
// mailbox) and "flags" (flags, moves or deletions {account, folder,
// uids}).
type Event struct {
	Type string
	Data any
}

type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// Subscribe returns the events of an account until cancel is called or
// the account loses T-Mail (the channel is closed then).
func (m *Manager) Subscribe(user string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h := &m.events
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[string]map[chan Event]struct{}{}
	}
	if h.subs[user] == nil {
		h.subs[user] = map[chan Event]struct{}{}
	}
	h.subs[user][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if _, ok := h.subs[user][ch]; ok {
				delete(h.subs[user], ch)
				close(ch)
			}
		})
	}
}

// publish hands an event to every open page of an account; a page that
// does not keep up misses it rather than holding titand up.
func (h *hub) publish(user string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[user] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// closeUser ends the event streams of an account.
func (h *hub) closeUser(user string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[user] {
		close(ch)
	}
	delete(h.subs, user)
}

// closeAll ends every event stream (titand stops: no open page holds up
// the end of the web server).
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.subs {
		for ch := range subs {
			close(ch)
		}
	}
	h.subs = nil
}
