package shares

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSMBConfPreviousVersionsGuardAndLock(t *testing.T) {
	path := func(s Share) string { return "/mnt/" + s.Volume + "/" + s.Name }
	shadow := func(s Share) string {
		if s.Volume == "pool" {
			return "/mnt/pool/.snapshots"
		}
		return ""
	}
	conf := renderSMBConf([]Share{
		{Name: "Fotos", Volume: "pool", SMB: true, Access: map[string]string{"alex": ReadWrite}, PreviousVersions: true, Guard: true, Recycle: Recycle{Enabled: true}},
		{Name: "Alt", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}, PreviousVersions: true},
		{Name: "Zu", Volume: "pool", SMB: true, Access: map[string]string{"alex": ReadWrite}, Guard: true, GuardLocked: &GuardLock{Reason: "known-extension"}},
	}, path, shadow)
	section := func(name string) string {
		_, after, _ := strings.Cut(conf, "["+name+"]")
		before, _, _ := strings.Cut(after, "\n[")
		return before
	}
	fotos := section("Fotos")
	for _, want := range []string{"vfs objects = shadow_copy2 full_audit recycle", "shadow:snapdir = /mnt/pool/.snapshots", "shadow:basedir = /mnt/pool", "shadow:format = @GMT-%Y.%m.%d-%H.%M.%S", "shadow:localtime = no", "full_audit:success = renameat unlinkat", "write list = alex"} {
		if !strings.Contains(fotos, want) {
			t.Errorf("Fotos lacks %q:\n%s", want, fotos)
		}
	}
	// ext4 has no snapshots: no previous versions, whatever the share says.
	if strings.Contains(section("Alt"), "shadow_copy2") {
		t.Error("previous versions on a volume without snapshots")
	}
	if zu := section("Zu"); strings.Contains(zu, "write list") || !strings.Contains(zu, "read only = yes") {
		t.Errorf("locked share still writable:\n%s", zu)
	}
	exports := renderExports([]Share{{Name: "Zu", Volume: "pool", NFS: NFS{Enabled: true, Clients: []string{"10.0.0.0/24"}}, GuardLocked: &GuardLock{}}}, "/srv/nfs")
	if !strings.Contains(exports, "10.0.0.0/24(ro,") {
		t.Errorf("locked share exported writable over NFS:\n%s", exports)
	}
}

func TestParseAudit(t *testing.T) {
	e, ok := parseAudit("alex|Fotos|renameat|ok|urlaub/a.jpg|urlaub/a.jpg.locked")
	if !ok || e.user != "alex" || e.share != "Fotos" || e.from != "urlaub/a.jpg" || e.to != "urlaub/a.jpg.locked" {
		t.Fatalf("%+v %v", e, ok)
	}
	for _, line := range []string{"alex|Fotos|renameat|fail|a|b", "alex|Fotos|openat|ok|a", "garbage"} {
		if _, ok := parseAudit(line); ok {
			t.Errorf("%q accepted", line)
		}
	}
}

func TestGuardRules(t *testing.T) {
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	rename := func(g *guardState, i int, to string, at time.Time) (string, int) {
		return g.observe(auditEvent{at: at, user: "alex", share: "Fotos", op: "renameat", from: fmt.Sprintf("d/f%d.docx", i), to: fmt.Sprintf("d/f%d.docx%s", i, to)})
	}
	var g guardState
	for i := range guardKnown - 1 {
		if reason, _ := rename(&g, i, ".locked", start); reason != "" {
			t.Fatalf("locked after %d renames", i+1)
		}
	}
	if reason, files := rename(&g, 99, ".locked", start); reason != "known-extension" || files != guardKnown {
		t.Fatalf("known extension: %q %d", reason, files)
	}

	// Many files with one new, unknown extension within a minute.
	var h guardState
	for i := range guardSameExtension - 1 {
		if reason, _ := rename(&h, i, ".x7k2", start.Add(time.Duration(i)*time.Second)); reason != "" {
			t.Fatalf("same extension: locked after %d", i+1)
		}
	}
	if reason, _ := rename(&h, 1000, ".x7k2", start.Add(50*time.Second)); reason != "same-extension" {
		t.Fatal("same extension not caught")
	}

	// Ordinary work: renames that keep the extension, or slowly over time.
	var k guardState
	for i := range 200 {
		if reason, _ := k.observe(auditEvent{at: start, user: "alex", share: "Fotos", op: "renameat", from: fmt.Sprintf("a%d.jpg", i), to: fmt.Sprintf("b%d.jpg", i)}); reason != "" {
			t.Fatal("plain renames locked the share")
		}
		if reason, _ := rename(&k, i, ".bak", start.Add(time.Duration(i)*2*time.Second)); reason != "" {
			t.Fatalf("slow renames locked the share at %d", i)
		}
	}
}

func TestGuardLocksAndUnlocks(t *testing.T) {
	m, runner := newTestManager(t)
	if err := m.Create(Share{Name: "Fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}, Guard: true}, volumes, users); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(Share{Name: "Frei", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}}, volumes, users); err != nil {
		t.Fatal(err)
	}
	var locked []string
	onLock := func(s Share) { locked = append(locked, s.Name) }
	at := time.Now()
	for i := range guardKnown + 3 {
		for _, share := range []string{"Fotos", "Frei"} {
			m.observeAudit(context.Background(), auditEvent{at: at, user: "alex", share: share, op: "renameat", from: fmt.Sprintf("f%d", i), to: fmt.Sprintf("f%d.crypt", i)}, onLock)
		}
	}
	if len(locked) != 1 || locked[0] != "Fotos" {
		t.Fatalf("locked: %v (only the guarded share, once)", locked)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "smbcontrol smbd close-share Fotos") {
		t.Fatal("open connections not closed:", runner.commands)
	}
	share := m.List()[0]
	if share.GuardLocked == nil || share.GuardLocked.User != "alex" {
		t.Fatalf("lock not stored: %+v", share)
	}
	// Editing the share keeps the lock; only unlocking lifts it.
	share.GuardLocked = nil
	if err := m.Update("Fotos", share, volumes, users); err != nil {
		t.Fatal(err)
	}
	if m.List()[0].GuardLocked == nil {
		t.Fatal("an edit lifted the lock")
	}
	if err := m.UnlockGuard("Fotos"); err != nil || m.List()[0].GuardLocked != nil {
		t.Fatal("not unlocked", err)
	}
}
