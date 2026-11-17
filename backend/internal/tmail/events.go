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

// maxStreams is how many event streams an account may have open; a new
// one ends the oldest (security review of 1.05).
const maxStreams = 8

type hub struct {
	mu   sync.Mutex
	subs map[string][]chan Event
}

// Subscribe returns the events of an account until cancel is called, the
// account loses T-Mail or it opens more than maxStreams (the channel is
// closed then).
func (m *Manager) Subscribe(user string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h := &m.events
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[string][]chan Event{}
	}
	if list := h.subs[user]; len(list) >= maxStreams {
		close(list[0])
		h.subs[user] = list[1:]
	}
	h.subs[user] = append(h.subs[user], ch)
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			list := h.subs[user]
			for i, c := range list {
				if c == ch {
					close(ch)
					h.subs[user] = append(list[:i:i], list[i+1:]...)
					if len(h.subs[user]) == 0 {
						delete(h.subs, user)
					}
					return
				}
			}
		})
	}
}

// publish hands an event to every open page of an account; a page that
// does not keep up misses it rather than holding titand up.
func (h *hub) publish(user string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[user] {
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
	for _, ch := range h.subs[user] {
		close(ch)
	}
	delete(h.subs, user)
}

// closeAll ends every event stream (titand stops: no open page holds up
// the end of the web server).
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, list := range h.subs {
		for _, ch := range list {
			close(ch)
		}
	}
	h.subs = nil
}
