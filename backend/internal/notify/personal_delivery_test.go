package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeferredPersistenceNeverStoresApprovalCredentials(t *testing.T) {
	m, _ := newManager(t)
	base := t.TempDir()
	available := true
	m.SetPersonalDataDir(func() (string, bool) { return base, available })
	m.now = func() time.Time { return time.Date(2026, 10, 23, 23, 0, 0, 0, time.Local) }
	personalSink(t, m, "alex", Quiet{Enabled: true, From: "22:00", To: "07:00"})
	p, _, _ := m.PersonalSettings("alex")
	p.PersistQuiet = true
	if err := m.SetPersonal("alex", p); err != nil {
		t.Fatal(err)
	}
	msg := PersonalMessage{Event: EventDone, Severity: Info, Title: "Result", Text: "private", Actions: []PushAction{{URL: "https://nas/secret-approval", HAAction: "secret-action"}}}
	if err := m.SendPersonal("alex", "first", msg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, "alex", "notifications", "pending.json"))
	if err != nil || !strings.Contains(string(data), "private") || strings.Contains(string(data), "secret-") {
		t.Fatal(string(data), err)
	}
	available = false
	if err = m.SendPersonal("alex", "second", msg); err == nil {
		t.Fatal("lost volume accepted")
	}
	m.people.mu.Lock()
	if m.people.users["alex"].sent["second"] || len(m.people.users["alex"].held) != 1 {
		t.Error("failed persistence accepted")
	}
	m.people.users["alex"].flush.Stop()
	m.people.mu.Unlock()
	available = true
	if err = m.SendPersonal("alex", "second", msg); err != nil {
		t.Fatal(err)
	}
	m.people.mu.Lock()
	m.people.users["alex"].flush.Stop()
	m.people.mu.Unlock()
}
func TestDeliveryReportsRetriesWithoutSecrets(t *testing.T) {
	m, _ := newManager(t)
	m.PersonalSettings("alex")
	m.retryPause = 0
	n := 0
	if !m.deliverPersonal("alex", "ntfy", func() error {
		n++
		if n < 3 {
			return errors.New("secret error")
		}
		return nil
	}) {
		t.Fatal("retry failed")
	}
	d := m.Deliveries("alex")["ntfy"]
	if !d.OK || d.Attempts != 3 {
		t.Fatal(d)
	}
	if len(m.Deliveries("sam")) != 0 {
		t.Fatal("shared delivery status")
	}
}
