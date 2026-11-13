package nvr

import (
	"errors"
	"io/fs"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// "Seen" per account (PLAN 88). Until 0.80 an event was seen for the whole
// NVR: titand set Frigate's review flag. Now every account has its own:
// titand keeps it in seen.json in .titanos/nvr on the NVR's volume (next
// to Frigate's database it belongs to; never on the stick, which is
// written only on the owner's changes), written only when an account
// marks something. Frigate's flag is no longer set; the flags 0.80 set
// still count for the events that began before titand started keeping
// its own ("cutover"): what the household had seen stays seen, for
// everyone, and nothing after it can become seen for all again.
//
// Bounded: per account at most maxSeenMarks single marks (the oldest
// events go first; "all seen" before a time is one number), and marks of
// events older than seenKeep fall out (no camera keeps events longer).

// ErrSeenUnreadable: seen.json could not be read; it is never written
// over (every account's marks would be lost), so marks are refused until
// it is read.
var ErrSeenUnreadable = errors.New("nvr: what was seen could not be read from the volume")

const seenFile = "seen.json"

const (
	// maxSeenMarks bounds the single marks of one account.
	maxSeenMarks = 4000
	// seenKeep is how long a mark is kept: longer than any retention.
	seenKeep = (maxRetainDays + 35) * 24 * time.Hour
	// seenFileLimit bounds what is read.
	seenFileLimit = 16 << 20
)

// seenData is seen.json.
type seenData struct {
	// Cutover is when titand began to keep "seen" per account (Unix
	// seconds): Frigate's flag counts for events that began before it.
	Cutover  float64                 `json:"cutover"`
	Accounts map[string]*seenAccount `json:"accounts"`
}

// seenAccount is what one account has seen.
type seenAccount struct {
	// AllBefore: every event that began before it is seen ("Alle als
	// gesehen markieren"), unless marked unseen since.
	AllBefore float64 `json:"allBefore,omitempty"`
	// Seen and Unseen are events marked one by one: ID → its start.
	Seen   map[string]float64 `json:"seen,omitempty"`
	Unseen map[string]float64 `json:"unseen,omitempty"`
}

// seenRuntime is what the manager holds of seen.json (guarded by m.mu;
// changes one at a time under m.seenMu).
type seenRuntime struct {
	data     *seenData
	volume   string
	readable bool
}

// seenState returns seen.json of the NVR's volume, read once.
func (m *Manager) seenState() (*seenData, string, bool, error) {
	m.mu.Lock()
	cached, volume := m.rt.seen, m.settings.Volume
	m.mu.Unlock()
	if cached.data != nil && cached.volume == volume {
		return cached.data, volume, cached.readable, nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return nil, volume, false, err
	}
	defer root.Close()
	data := &seenData{}
	readable := true
	if err := volumedata.ReadJSON(root, seenFile, data, seenFileLimit); err != nil {
		data = &seenData{}
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("NVR: what the accounts have seen is not readable; it is not changed", "err", err)
			readable = false
		}
	}
	if data.Cutover == 0 {
		data.Cutover = float64(m.now().Unix())
	}
	if data.Accounts == nil {
		data.Accounts = map[string]*seenAccount{}
	}
	m.mu.Lock()
	if m.settings.Volume == volume {
		m.rt.seen = seenRuntime{data: data, volume: volume, readable: readable}
	}
	m.mu.Unlock()
	return data, volume, readable, nil
}

// seenJudge tells for an account whether an event is seen; legacy is
// Frigate's flag (only asked for events before the cutover).
type seenJudge struct {
	cutover float64
	account seenAccount
}

func (j seenJudge) seen(e Event, legacy bool) bool {
	if _, ok := j.account.Unseen[e.ID]; ok {
		return false
	}
	if _, ok := j.account.Seen[e.ID]; ok {
		return true
	}
	if e.Start < j.account.AllBefore {
		return true
	}
	return e.Start < j.cutover && legacy
}

// needsLegacy tells whether Frigate's flag decides any of the events.
func (j seenJudge) needsLegacy(events []Event) bool {
	return slices.ContainsFunc(events, func(e Event) bool { return e.Start < j.cutover })
}

// judge is the account's judge; an unreadable file judges by Frigate's
// flag alone (all as before the cutover).
func (m *Manager) judge(account string) seenJudge {
	data, _, readable, err := m.seenState()
	if err != nil || !readable {
		return seenJudge{cutover: float64(m.now().Unix()) + 1}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j := seenJudge{cutover: data.Cutover}
	if a := data.Accounts[account]; a != nil {
		j.account = seenAccount{AllBefore: a.AllBefore, Seen: a.Seen, Unseen: a.Unseen}
	}
	return j
}

// changeSeen edits an account's marks and writes seen.json to the volume
// it was read from.
func (m *Manager) changeSeen(account string, edit func(a *seenAccount, cutover float64)) error {
	if !accountRE.MatchString(account) {
		return invalid("account")
	}
	m.seenMu.Lock()
	defer m.seenMu.Unlock()
	data, volume, readable, err := m.seenState()
	if err != nil {
		return err
	}
	if !readable {
		return ErrSeenUnreadable
	}
	m.mu.Lock()
	next := &seenData{Cutover: data.Cutover, Accounts: make(map[string]*seenAccount, len(data.Accounts)+1)}
	for name, a := range data.Accounts {
		next.Accounts[name] = a
	}
	m.mu.Unlock()
	a := seenAccount{}
	if old := next.Accounts[account]; old != nil {
		a = seenAccount{AllBefore: old.AllBefore, Seen: cloneMarks(old.Seen), Unseen: cloneMarks(old.Unseen)}
	}
	edit(&a, data.Cutover)
	pruneMarks(&a, float64(m.now().Add(-seenKeep).Unix()))
	if a.AllBefore == 0 && len(a.Seen) == 0 && len(a.Unseen) == 0 {
		delete(next.Accounts, account)
	} else {
		next.Accounts[account] = &a
	}
	return m.writeSeen(volume, next)
}

func (m *Manager) writeSeen(volume string, next *seenData) error {
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := volumedata.WriteJSON(root, seenFile, next); err != nil {
		return err
	}
	m.mu.Lock()
	if m.settings.Volume == volume {
		m.rt.seen = seenRuntime{data: next, volume: volume, readable: true}
	}
	m.mu.Unlock()
	return nil
}

func cloneMarks(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for id, start := range in {
		out[id] = start
	}
	return out
}

// pruneMarks drops marks of events older than oldest and, beyond
// maxSeenMarks, those of the oldest events.
func pruneMarks(a *seenAccount, oldest float64) {
	type mark struct {
		id     string
		start  float64
		unseen bool
	}
	all := []mark{}
	for id, start := range a.Seen {
		all = append(all, mark{id, start, false})
	}
	for id, start := range a.Unseen {
		all = append(all, mark{id, start, true})
	}
	slices.SortFunc(all, func(x, y mark) int {
		switch {
		case x.start > y.start:
			return -1
		case x.start < y.start:
			return 1
		}
		if x.id < y.id {
			return -1
		}
		return 1
	})
	seen, unseen := map[string]float64{}, map[string]float64{}
	for i, mk := range all {
		if i >= maxSeenMarks || mk.start < oldest {
			break
		}
		if mk.unseen {
			unseen[mk.id] = mk.start
		} else {
			seen[mk.id] = mk.start
		}
	}
	a.Seen, a.Unseen = seen, unseen
	if len(a.Seen) == 0 {
		a.Seen = nil
	}
	if len(a.Unseen) == 0 {
		a.Unseen = nil
	}
}

// MarkSeen marks an event as seen or not for an account. legacy is
// Frigate's flag of the event (the NVR-wide "seen" of 0.80).
func (m *Manager) MarkSeen(account string, e Event, seen bool) error {
	return m.changeSeen(account, func(a *seenAccount, cutover float64) {
		delete(a.Seen, e.ID)
		delete(a.Unseen, e.ID)
		byDefault := seenJudge{cutover: cutover, account: *a}.seen(e, e.legacySeen)
		if byDefault == seen {
			return
		}
		if seen {
			if a.Seen == nil {
				a.Seen = map[string]float64{}
			}
			a.Seen[e.ID] = e.Start
		} else {
			if a.Unseen == nil {
				a.Unseen = map[string]float64{}
			}
			a.Unseen[e.ID] = e.Start
		}
	})
}

// MarkAllSeen marks every event that began before (Unix seconds) as seen
// for an account.
func (m *Manager) MarkAllSeen(account string, before float64) error {
	return m.changeSeen(account, func(a *seenAccount, _ float64) {
		a.AllBefore = max(a.AllBefore, before)
		for id, start := range a.Seen {
			if start < a.AllBefore {
				delete(a.Seen, id)
			}
		}
		for id, start := range a.Unseen {
			if start < a.AllBefore {
				delete(a.Unseen, id)
			}
		}
	})
}

// ForgetSeen drops the marks of a deleted account (where the volume can
// be reached).
func (m *Manager) ForgetSeen(account string) {
	m.mu.Lock()
	volume := m.settings.Volume
	m.mu.Unlock()
	if volume == "" || !slices.Contains(m.mounted(), volume) {
		return
	}
	data, _, readable, err := m.seenState()
	if err != nil || !readable {
		return
	}
	m.mu.Lock()
	_, known := data.Accounts[account]
	m.mu.Unlock()
	if !known {
		return
	}
	if err := m.changeSeen(account, func(a *seenAccount, _ float64) { *a = seenAccount{} }); err != nil {
		slog.Warn("NVR: what a deleted account had seen is not removed", "err", err)
	}
}
