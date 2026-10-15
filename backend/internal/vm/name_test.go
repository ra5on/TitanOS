package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayNameValidation(t *testing.T) {
	for _, name := range []string{"Home Assistant", "Büro – Test", "日本語", `VM <test> & "NAS"`} {
		if !validDisplayName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"", "a\nb", "a\x00b", "a\u202eb", strings.Repeat("a", 81), string([]byte{0xff})} {
		if validDisplayName(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

func TestRenamePreservesRunningDomainDisksAndIdentity(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	spec := VM{Name: "home-assistant", DisplayName: "Home Assistant", Template: "linux", MemoryMiB: 512, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: gib, Volume: "fast"}}}
	if err := m.Create(ctx, spec, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	before, _ := m.get(spec.Name)
	if err := m.Start(ctx, spec.Name, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	label := `Haus & Büro <Test>`
	change := Change{DisplayName: &label, MemoryMiB: before.MemoryMiB, VCPUs: before.VCPUs, Network: before.Network}
	if err := m.Update(ctx, spec.Name, change, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	after, _ := m.get(spec.Name)
	if after.Label() != label || after.UUID != before.UUID || after.MAC != before.MAC || after.Name != before.Name || m.diskPath(after, after.Disks[0]) != m.diskPath(before, before.Disks[0]) {
		t.Fatalf("rename changed identity: %+v", after)
	}
	xml, err := m.XML(spec.Name)
	if err != nil || !strings.Contains(xml, "<name>home-assistant</name>") || !strings.Contains(xml, "<title>Haus &amp; Büro &lt;Test&gt;</title>") {
		t.Fatalf("domain XML: %s / %v", xml, err)
	}
	// Older clients omit displayName; updating other metadata retains it.
	change.DisplayName = nil
	if err := m.Update(ctx, spec.Name, change, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.get(spec.Name); got.Label() != label {
		t.Fatal("older client cleared display name")
	}
	blank := "  "
	change.DisplayName = &blank
	if err := m.Update(ctx, spec.Name, change, []string{"fast"}); !errors.Is(err, ErrInvalidDisplayName) {
		t.Fatal("accepted blank label", err)
	}
	os.Remove(filepath.Join(m.pidDir, spec.Name+".pid"))
	if err := m.Delete(spec.Name, true, label); err != nil {
		t.Fatal("display name confirmation rejected", err)
	}
}
