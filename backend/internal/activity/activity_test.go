package activity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func testManager(t *testing.T) *Manager {
	m := New(config.Open(t.TempDir()))
	m.MountRoot = t.TempDir()
	os.Mkdir(filepath.Join(m.MountRoot, "data"), 0700)
	m.Mounted = func() []string { return []string{"data"} }
	return m
}

func TestRecordFlushQueryAndTornLine(t *testing.T) {
	m := testManager(t)
	now := time.Now().UTC()
	m.Record(Entry{At: now.Add(-48 * time.Hour), User: "anna", Via: ViaWeb, Action: "DELETE /api/v1/shares/{name}", Target: "fotos", Status: 204, OK: true})
	m.Record(Entry{At: now.Add(-time.Minute), User: "ben", Via: ViaWeb, Action: "login_failed", OK: false})
	m.Flush()
	if st := m.Status(); st.Pending != 0 || st.Volume != "data" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
	file := filepath.Join(m.MountRoot, "data", ".titanos", "activity", now.Format("2006-01-02")+".jsonl")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"at":"broken`)
	f.Close()
	m.Record(Entry{At: now, User: "anna", Via: ViaToken, Action: "PUT /api/v1/users/{name}", Target: "ben\nfake line", OK: true})
	m.Flush()
	rows, more, stored, err := m.Query(context.Background(), Filter{Days: 7, Limit: 10})
	if err != nil || more || !stored || len(rows) != 3 {
		t.Fatalf("%v %v %v %+v", err, more, stored, rows)
	}
	if rows[0].Via != ViaToken || strings.Contains(rows[0].Target, "\n") || rows[2].Target != "fotos" {
		t.Fatalf("%+v", rows)
	}
	rows, _, _, _ = m.Query(context.Background(), Filter{Days: 7, Limit: 10, User: "anna", Text: "SHARES"})
	if len(rows) != 1 {
		t.Fatalf("%+v", rows)
	}
	rows, _, _, _ = m.Query(context.Background(), Filter{Days: 7, Limit: 10, Failed: true})
	if len(rows) != 1 || rows[0].Action != "login_failed" {
		t.Fatalf("%+v", rows)
	}
	rows, more, _, _ = m.Query(context.Background(), Filter{Days: 7, Limit: 1})
	if len(rows) != 1 || !more {
		t.Fatal("paging")
	}
	rows, _, _, _ = m.Query(context.Background(), Filter{Days: 7, Limit: 10, Before: rows[0].At})
	if len(rows) != 2 {
		t.Fatalf("before: %+v", rows)
	}
	rows, _, _, _ = m.Query(context.Background(), Filter{Days: 1, Limit: 10})
	if len(rows) != 2 {
		t.Fatalf("days: %+v", rows)
	}
}

func TestMemoryOnlyWithoutVolumeAndLaterFlush(t *testing.T) {
	m := testManager(t)
	m.Mounted = func() []string { return nil }
	m.Record(Entry{User: "anna", Action: "login", OK: true})
	m.Flush()
	rows, _, stored, err := m.Query(context.Background(), Filter{Days: 1, Limit: 10})
	if err != nil || stored || len(rows) != 1 || m.Status().Pending != 1 {
		t.Fatalf("%v %v %+v", err, stored, rows)
	}
	m.Mounted = func() []string { return []string{"data"} }
	m.Flush()
	rows, _, stored, _ = m.Query(context.Background(), Filter{Days: 1, Limit: 10})
	if !stored || len(rows) != 1 || m.Status().Pending != 0 {
		t.Fatalf("%v %+v", stored, rows)
	}
}

func TestDisabledRecordsNothingAndSettingsAreChecked(t *testing.T) {
	m := testManager(t)
	for _, s := range []Settings{{true, "../x", 90, 64}, {true, "", 7, 64}, {true, "", 90, 4}} {
		if m.Set(s) == nil {
			t.Fatal(s)
		}
	}
	if err := m.Set(Settings{false, "", 30, 8}); err != nil {
		t.Fatal(err)
	}
	m.Record(Entry{User: "anna", Action: "login"})
	if m.Status().Pending != 0 {
		t.Fatal("recorded while off")
	}
	reloaded := New(m.store)
	if err := reloaded.Load(); err != nil || reloaded.Status().Settings.Enabled {
		t.Fatal("settings not kept")
	}
}

func TestPruneKeepsRetentionAndBudget(t *testing.T) {
	m := testManager(t)
	m.Set(Settings{true, "data", 30, 8})
	dir := filepath.Join(m.MountRoot, "data", ".titanos", "activity")
	m.Record(Entry{User: "anna", Action: "login"})
	m.Flush()
	old := filepath.Join(dir, time.Now().UTC().AddDate(0, 0, -40).Format("2006-01-02")+".jsonl")
	big := filepath.Join(dir, time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")+".jsonl")
	os.WriteFile(old, []byte("{}\n"), 0600)
	os.WriteFile(big, []byte(strings.Repeat("x", 8<<20)+"\n"), 0600)
	m.Record(Entry{User: "anna", Action: "logout"})
	m.Flush()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old day kept")
	}
	if _, err := os.Stat(big); !os.IsNotExist(err) {
		t.Fatal("budget ignored")
	}
	if m.Status().Error != "" {
		t.Fatal(m.Status().Error)
	}
}

func TestCleanCutsLongTextOnRuneBoundary(t *testing.T) {
	s := clean(strings.Repeat("ä", 400), 501)
	if !strings.HasSuffix(s, "…") || len(s) > 504 || strings.ContainsRune(s, '�') {
		t.Fatal(s)
	}
}
