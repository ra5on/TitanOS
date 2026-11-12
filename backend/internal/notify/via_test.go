package notify

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// The subject of a camera's message holds a recognised person's name or
// a plate ("Anna · Haustür"): it goes to the ways chosen, never into the
// system log, which ends up in the diagnosis bundle and on a volume.
func TestNotifyViaLogsNoSubject(t *testing.T) {
	m, _ := newManager(t)
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	m.NotifyVia(Info, "nvr_event_face", "Anna Beispiel · Haustür", Ways{Bell: true, Ref: "haustuer"})
	m.NotifyVia(Info, "nvr_event_plate", "M-AB 1234 · Einfahrt", Ways{})
	text := log.String()
	for _, secret := range []string{"Anna", "M-AB 1234", "Haustür", "Einfahrt"} {
		if strings.Contains(text, secret) {
			t.Errorf("the log names %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "nvr_event_face") || !strings.Contains(text, "haustuer") {
		t.Errorf("kind and camera are not in the log: %s", text)
	}
	if got := m.Messages(); len(got) == 0 || got[len(got)-1].Subject != "Anna Beispiel · Haustür" {
		t.Errorf("the bell lost the subject: %+v", got)
	}
}
