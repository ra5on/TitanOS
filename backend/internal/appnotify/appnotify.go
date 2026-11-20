// Package appnotify keeps the messages for the paired Android apps (PLAN
// 116, TECHNIK §5, docs/ANDROID-APP.md "Mitteilungen"): the same messages
// Web Push sends to browsers – what the bell of owners and administrators
// sends out, new mail, calendar reminders, the KI-Agent's answers and
// questions, the cameras of the NVR – kept per account so that an app can
// fetch them (list) or wait for them (stream), without Google.
//
// Only RAM: nothing is written to the stick or a volume. A restart of
// titand starts a new epoch; a marker of an older epoch gets everything
// that is kept and "reset", so the app never waits for numbers that will
// not come again. At most maxPerAccount messages per account and as many
// for the messages to all administrators, none older than keepFor.
package appnotify

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// Categories of a message, as the app files them (channels, filters).
const (
	NAS      = "nas"
	Mail     = "mail"
	Calendar = "calendar"
	Agent    = "agent"
	NVR      = "nvr"
)

const (
	// maxPerAccount bounds what is kept for one account, and for the
	// messages to all administrators.
	maxPerAccount = 200
	// keepFor is how long a message is kept.
	keepFor = 7 * 24 * time.Hour
	// maxStreams is how many streams an account may have open; a new one
	// ends the oldest.
	maxStreams = 8
	// subBuffer is how many messages wait for a slow stream before it is
	// told to read the list again.
	subBuffer = 64
	// calendarTag begins the tag of a calendar reminder's push
	// (api/calendar_reminders.go: kind "calendar_reminder", notify.PushTo).
	calendarTag = "calendar_reminder:"
)

// Item is one message as the app reads it.
type Item struct {
	// ID is the marker "<epoch>-<seq>" of this message.
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Category string    `json:"category"`
	// Severity is info, warning or error (NAS messages; else info).
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	// Tag: a newer message with the same tag replaces the older one on the
	// phone (as Web Push does).
	Tag string `json:"tag,omitempty"`
	// Link is the page of TitanOS the message is about ("/mail", "/agent?chat=…").
	Link string `json:"link,omitempty"`
	// App is the app it belongs to: titanos or agent (webpush.AppOf).
	App string `json:"app"`
	seq int64
}

type entry struct {
	Item
	// all: a message to every owner and administrator, shown to those who
	// are one when it is read.
	all bool
}

// Sub is one open stream of an account.
type Sub struct {
	C    chan Item
	user string
	// Lost is set when a message did not fit: the stream says "resync".
	Lost atomic.Bool
}

// Hub keeps the messages and hands new ones to the streams.
type Hub struct {
	// Admin tells whether an account may see the messages to all
	// administrators now (owner or administrator, not locked); nil: none.
	Admin func(user string) bool
	now   func() time.Time
	epoch int64

	mu   sync.Mutex
	seq  int64
	all  []entry
	per  map[string][]entry
	subs map[string][]*Sub
}

// New returns an empty hub with a new epoch.
func New() *Hub {
	return &Hub{now: time.Now, epoch: time.Now().UnixMilli(), per: map[string][]entry{}, subs: map[string][]*Sub{}}
}

// Epoch is the epoch of this hub as the app sees it.
func (h *Hub) Epoch() string { return strconv.FormatInt(h.epoch, 10) }

func (h *Hub) marker(seq int64) string { return fmt.Sprintf("%d-%d", h.epoch, seq) }

// Since reads a marker: the number after it and whether it belongs to
// this epoch. "" is the start (no reset); a marker of another epoch or
// one that cannot be read starts over with reset.
func (h *Hub) Since(marker string) (seq int64, reset bool) {
	if marker == "" {
		return 0, false
	}
	epoch, n, ok := strings.Cut(marker, "-")
	if !ok || epoch != h.Epoch() {
		return 0, true
	}
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v < 0 {
		return 0, true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if v > h.seq {
		return 0, true
	}
	return v, false
}

// Category files a Web Push message by its kind (webpush.Kind…).
func Category(kind string, msg webpush.Message) string {
	switch kind {
	case webpush.KindNAS:
		return NAS
	case webpush.KindMail:
		return Mail
	case webpush.KindAgent:
		return Agent
	case webpush.KindCamera:
		// Calendar reminders go to the account's devices like a camera's
		// message (notify.PushTo); they are Termine, not NVR.
		if strings.HasPrefix(msg.Tag, calendarTag) {
			return Calendar
		}
		return NVR
	}
	return NAS
}

// FromPush is webpush.Manager's mirror: user "" is a message to every
// owner and administrator (Broadcast).
func (h *Hub) FromPush(user, kind string, msg webpush.Message) {
	severity := msg.Severity
	if severity == "" {
		severity = "info"
		if msg.Urgent {
			severity = "error"
		}
	}
	h.Add(user, Item{Category: Category(kind, msg), Severity: severity, Title: msg.Title, Text: msg.Body, Tag: msg.Tag, Link: msg.URL, App: webpush.AppOf(msg)})
}

// Add keeps a message for an account ("" for every administrator) and
// hands it to the open streams.
func (h *Hub) Add(user string, it Item) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The number follows the clock (microseconds, always rising): a gap
	// between two of an account's markers tells nothing about how many
	// messages other accounts got in between.
	h.seq = max(h.seq+1, h.now().UnixMicro())
	it.seq = h.seq
	it.ID = h.marker(it.seq)
	if it.Time.IsZero() {
		it.Time = h.now().UTC()
	}
	e := entry{Item: it, all: user == ""}
	if e.all {
		h.all = h.trim(append(h.all, e))
	} else {
		h.per[user] = h.trim(append(h.per[user], e))
	}
	for name, subs := range h.subs {
		if name != user && (!e.all || !h.admin(name)) {
			continue
		}
		for _, sub := range subs {
			select {
			case sub.C <- it:
			default:
				sub.Lost.Store(true)
			}
		}
	}
}

// trim drops what is too old and what is too much; called with mu held.
func (h *Hub) trim(list []entry) []entry {
	cut := h.now().Add(-keepFor)
	i := 0
	for i < len(list) && list[i].Time.Before(cut) {
		i++
	}
	list = list[i:]
	if len(list) > maxPerAccount {
		list = list[len(list)-maxPerAccount:]
	}
	return slices.Clip(list)
}

func (h *Hub) admin(user string) bool { return h.Admin != nil && h.Admin(user) }

// List returns the account's messages after seq, oldest first, at most
// maxPerAccount (the newest), and the marker of the newest message there
// is at all.
func (h *Hub) List(user string, after int64) ([]Item, string) {
	admin := h.admin(user)
	h.mu.Lock()
	defer h.mu.Unlock()
	cut := h.now().Add(-keepFor)
	var out []Item
	add := func(list []entry) {
		for _, e := range list {
			if e.seq > after && !e.Time.Before(cut) {
				out = append(out, e.Item)
			}
		}
	}
	add(h.per[user])
	if admin {
		add(h.all)
	}
	slices.SortFunc(out, func(a, b Item) int { return int(a.seq - b.seq) })
	if len(out) > maxPerAccount {
		out = out[len(out)-maxPerAccount:]
	}
	if out == nil {
		out = []Item{}
	}
	return out, h.marker(h.seq)
}

// Latest is the marker of the newest message there is at all.
func (h *Hub) Latest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.marker(h.seq)
}

// Seq is the number of an item of this hub (0 for one of another).
func (h *Hub) Seq(it Item) int64 { return it.seq }

// Subscribe opens a stream for an account; the channel closes on cancel,
// when the account opens more than maxStreams or is forgotten.
func (h *Hub) Subscribe(user string) (*Sub, func()) {
	sub := &Sub{C: make(chan Item, subBuffer), user: user}
	h.mu.Lock()
	if list := h.subs[user]; len(list) >= maxStreams {
		close(list[0].C)
		h.subs[user] = slices.Clone(list[1:])
	}
	h.subs[user] = append(h.subs[user], sub)
	h.mu.Unlock()
	var once sync.Once
	return sub, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			list := h.subs[user]
			if i := slices.Index(list, sub); i >= 0 {
				close(sub.C)
				h.subs[user] = slices.Delete(slices.Clone(list), i, i+1)
				if len(h.subs[user]) == 0 {
					delete(h.subs, user)
				}
			}
		})
	}
}

// Forget drops what is kept for an account and ends its streams (deleted).
func (h *Hub) Forget(user string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.per, user)
	for _, sub := range h.subs[user] {
		close(sub.C)
	}
	delete(h.subs, user)
}
