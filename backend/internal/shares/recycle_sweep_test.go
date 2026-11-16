package shares

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The hourly sweep runs as root in folders accounts write to. A folder of
// the bin swapped for a link to somewhere else while it runs must not lead
// it there: old files outside the share would be deleted.
func TestSweepStaysInsideTheBin(t *testing.T) {
	m, _ := newTestManager(t)
	m.Create(Share{Name: "Fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}, Recycle: Recycle{Enabled: true, Days: 30}}, volumes, users)
	bin := filepath.Join(m.Path(m.List()[0]), ".recycle", "alex")
	outside := filepath.Join(t.TempDir(), "outside")
	os.MkdirAll(outside, 0o755)
	victim := filepath.Join(outside, "victim")
	os.WriteFile(victim, []byte("keep me"), 0o644)
	past := time.Now().AddDate(0, 0, -400)
	os.Chtimes(victim, past, past)
	os.MkdirAll(filepath.Join(bin, "d"), 0o770)
	for i := range 1000 {
		os.WriteFile(filepath.Join(bin, fmt.Sprintf("a%05d", i)), nil, 0o660) // fresh: kept
	}
	d, real := filepath.Join(bin, "d"), filepath.Join(bin, "d.real")
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			os.Rename(d, real)
			os.Symlink(outside, d)
			time.Sleep(200 * time.Microsecond)
			os.Remove(d)
			os.Rename(real, d)
			time.Sleep(200 * time.Microsecond)
		}
	}()
	defer func() { stop.Store(true); <-done }()
	// The bin itself swapped for a link is not followed either.
	other := t.TempDir()
	os.MkdirAll(filepath.Join(other, "alex"), 0o755)
	os.WriteFile(filepath.Join(other, "alex", "victim"), nil, 0o644)
	os.Chtimes(filepath.Join(other, "alex", "victim"), past, past)
	share2 := filepath.Join(t.TempDir(), "share2")
	os.MkdirAll(share2, 0o755)
	os.Symlink(other, filepath.Join(share2, recycleDir))
	if n := m.sweepBin(share2, recycleDir, time.Now()); n != 0 {
		t.Fatalf("a bin that is a link was swept: %d", n)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.sweepRecycle([]string{"data"}, time.Now())
		if _, err := os.Stat(victim); err != nil {
			t.Fatalf("the hourly sweep deleted a file outside the share: %v", err)
		}
	}
}

// In a share with a folder per account (KI-Agent), Samba keeps the bin in
// the folder of the account; the sweep empties it there too.
func TestSweepEmptiesTheBinsOfAShareWithFoldersPerAccount(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Create(Share{Name: "KI-Agent", Volume: "data", SMB: true, PerUser: true, Access: map[string]string{"alex": ReadWrite}, Recycle: Recycle{Enabled: true, Days: 30}}, volumes, users); err != nil {
		t.Fatal(err)
	}
	if conf := renderSMBConf(m.List(), m.Path, nil); !strings.Contains(conf, "path = "+m.Path(m.List()[0])+"/%u") {
		t.Fatalf("path:\n%s", conf)
	}
	old := filepath.Join(m.Path(m.List()[0]), "alex", ".recycle", "alex", "a.txt")
	fresh := filepath.Join(filepath.Dir(old), "b.txt")
	os.MkdirAll(filepath.Dir(old), 0o770)
	os.WriteFile(old, []byte("x"), 0o660)
	os.WriteFile(fresh, []byte("y"), 0o660)
	past := time.Now().AddDate(0, 0, -60)
	os.Chtimes(old, past, past)
	m.sweepRecycle([]string{"data"}, time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Error("a file 60 days in the bin of the account is still there")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a fresh file went: %v", err)
	}
}
