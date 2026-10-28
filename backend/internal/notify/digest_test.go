package notify

import (
	"strings"
	"testing"
	"time"
)

func TestDailySummary(t *testing.T) {
	m, _ := newManager(t)
	var pushed []Message
	m.Push = func(message Message, title string) { pushed = append(pushed, message) }
	settings, _ := m.Settings()
	settings.Language, settings.MinSeverity = "de", Info
	settings.Digest = Digest{Enabled: true, Time: "25:00"}
	if err := m.SetSettings(settings); err == nil {
		t.Fatal("invalid time accepted")
	}
	settings.Digest = Digest{Enabled: true, Time: "19:30", ErrorsNow: true}
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	m.Notify(Warning, "volume_degraded", "data")
	m.Notify(Info, "update_available", "0.35.0")
	if len(pushed) != 0 || m.Pending() != 2 {
		t.Fatalf("sent at once: %d, waiting %d", len(pushed), m.Pending())
	}
	m.Notify(Error, "volume_failed", "data")
	if len(pushed) != 1 || pushed[0].Kind != "volume_failed" {
		t.Fatal("error not sent at once")
	}
	if len(m.Messages()) != 3 {
		t.Fatal("the bell lost messages")
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	morning := time.Date(2026, 10, 29, 8, 0, 0, 0, berlin)
	if digestDue(settings.Digest, morning, morning.Add(-time.Hour)) {
		t.Fatal("due before its time")
	}
	evening := time.Date(2026, 10, 29, 19, 31, 0, 0, berlin)
	if !digestDue(settings.Digest, evening, morning) || digestDue(settings.Digest, evening, evening.Add(-30*time.Second)) {
		t.Fatal("due wrongly")
	}
	m.sendDigest(evening)
	if len(pushed) != 2 || m.Pending() != 0 {
		t.Fatalf("summary not sent: %d", len(pushed))
	}
	summary := pushed[1]
	if summary.Title != "2 Meldungen heute" || summary.Severity != Warning || !strings.Contains(summary.Text, "⚠") || strings.Count(summary.Text, "\n") != 2 {
		t.Fatalf("summary %+v", summary)
	}
	m.sendDigest(evening.Add(time.Minute))
	if len(pushed) != 2 {
		t.Fatal("empty summary sent")
	}
}
