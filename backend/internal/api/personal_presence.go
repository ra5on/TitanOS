package api

import (
	"net/http"
	"time"
)

// 1.39 (owner 28.11.2026): "wenn ein Chatfenster im Agent geöffnet ist,
// dass ich nicht trotzdem noch eine Benachrichtigung bekomme … aber nur wenn
// halt auch auf dem Desktop kein Fenster … geöffnet ist." Every Agent
// window that the account sees (visible and in front) says so every few
// seconds; while one of them does, a finished or waiting job sends no push
// to any device of the account – the open window shows it. The marks live
// in RAM only and end on their own: a window that closes, sleeps or loses
// its connection stops saying so.
const personalLookingFor = 45 * time.Second

// personalPresence takes {window, visible} from one Agent window. window is
// a random name the page chose for itself (one browser can hold several).
func (s *Server) personalPresence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Window  string `json:"window"`
		Visible bool   `json:"visible"`
	}
	if !readPersonalJSON(w, r, &in) {
		return
	}
	if in.Window == "" || len(in.Window) > 64 {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	s.personalLook(personalKey(currentUser(r)), in.Window, in.Visible, time.Now())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) personalLook(key, window string, visible bool, now time.Time) {
	rt := &s.personal
	rt.looking.Lock()
	defer rt.looking.Unlock()
	if rt.seen == nil {
		rt.seen = map[string]map[string]time.Time{}
	}
	windows := rt.seen[key]
	for name, at := range windows {
		if now.Sub(at) > personalLookingFor {
			delete(windows, name)
		}
	}
	if visible {
		if windows == nil {
			windows = map[string]time.Time{}
			rt.seen[key] = windows
		}
		// A page never keeps more than a handful of windows; a flood of
		// names cannot grow the map without bound.
		if _, known := windows[window]; known || len(windows) < 32 {
			windows[window] = now
		}
	} else {
		delete(windows, window)
	}
	if len(windows) == 0 {
		delete(rt.seen, key)
	}
}

// personalLooking says whether some Agent window of the account is in view.
func (s *Server) personalLooking(key string, now time.Time) bool {
	rt := &s.personal
	rt.looking.Lock()
	defer rt.looking.Unlock()
	for _, at := range rt.seen[key] {
		if now.Sub(at) <= personalLookingFor {
			return true
		}
	}
	return false
}
