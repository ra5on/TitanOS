package notify

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// The daily summary (docs/NOTIFICATIONS.md): instead of one message per
// event, the ways out (e-mail, ntfy, Telegram, Discord, Home Assistant and
// web push) get one summary a day. Errors can still go out at once. The
// bell is not affected. Waiting messages live in RAM only: a restart of
// the NAS before the summary loses them (the bell starts empty, too).

var digestTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// maxPending caps the waiting messages; the summary says how many more.
const maxPending = 200

// Digest is the setting of the daily summary.
type Digest struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"`
	// ErrorsNow sends errors at once, outside the summary.
	ErrorsNow bool `json:"errorsNow"`
}

// collect keeps a message for the summary; false means send it now.
func (m *Manager) collect(settings Settings, message Message) bool {
	d := settings.Digest
	if !d.Enabled || d.ErrorsNow && message.Severity == Error {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, message)
	if len(m.pending) > maxPending {
		m.pending = m.pending[len(m.pending)-maxPending:]
	}
	return true
}

// Pending tells how many messages wait for the summary.
func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// digestDue tells whether the time of the summary has come today and it
// has not gone out since.
func digestDue(d Digest, now, sent time.Time) bool {
	var hour, minute int
	_, _ = fmt.Sscanf(d.Time, "%d:%d", &hour, &minute) // a value that cannot be read leaves the zero value, which the code handles
	at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	return d.Enabled && !now.Before(at) && sent.Before(at)
}

// sendDigest sends the waiting messages as one; nothing waiting, nothing
// sent.
func (m *Manager) sendDigest(now time.Time) {
	m.mu.Lock()
	settings := m.settings
	list := m.pending
	m.pending, m.digestSent = nil, now
	m.mu.Unlock()
	if len(list) == 0 {
		return
	}
	de := settings.Language == "de"
	title := fmt.Sprintf("%d messages today", len(list))
	if de {
		title = fmt.Sprintf("%d Meldungen heute", len(list))
	}
	var b strings.Builder
	errors, warnings := 0, 0
	for _, message := range list {
		mark := "•"
		switch message.Severity {
		case Error:
			mark, errors = "✖", errors+1
		case Warning:
			mark, warnings = "⚠", warnings+1
		}
		fmt.Fprintf(&b, "%s %s %s\n", message.Time.Format("15:04"), mark, message.Title)
	}
	severity := Info
	if errors > 0 {
		severity = Error
	} else if warnings > 0 {
		severity = Warning
	}
	summary := Message{Time: now, Severity: severity, Kind: "digest", Title: title, Text: b.String()}
	_ = m.deliver(settings, summary, nil) // without a single channel deliver sends in the background and returns nil
	if m.Push != nil {
		hostname, _ := os.Hostname()
		m.Push(summary, "["+hostname+"] "+title)
	}
}

// RunDigest sends the summary at its time every day until ctx ends.
// Location gives the time zone of the NAS.
func (m *Manager) RunDigest(ctx context.Context, location func() *time.Location) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := m.now().In(location())
		m.mu.Lock()
		d, sent := m.settings.Digest, m.digestSent
		if sent.IsZero() {
			// After a start only the next time counts, not one that passed.
			m.digestSent, sent = now, now
		}
		waiting := len(m.pending)
		m.mu.Unlock()
		// Switched off with messages waiting: they go out now.
		if digestDue(d, now, sent) || !d.Enabled && waiting > 0 {
			m.sendDigest(now)
		}
	}
}
