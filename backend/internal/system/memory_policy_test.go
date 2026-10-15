package system

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testMemoryPolicy(t *testing.T) (*MemoryPolicy, *Memory) {
	t.Helper()
	mem := &Memory{TotalBytes: 32 << 30, AvailableBytes: 12 << 30}
	m := NewMemoryPolicy(config.Open(t.TempDir()), func() Memory { return *mem })
	m.sysctl = t.TempDir()
	for key, value := range map[string]string{"dirty_bytes": "0", "dirty_background_bytes": "0", "dirty_ratio": "20", "dirty_background_ratio": "10"} {
		os.WriteFile(filepath.Join(m.sysctl, key), []byte(value), 0644)
	}
	// Emulate the kernel's mutually exclusive bytes/ratio controls on fake files.
	m.write = func(p string, b []byte) error {
		if err := os.WriteFile(p, b, 0644); err != nil {
			return err
		}
		paired := strings.Replace(filepath.Base(p), "bytes", "ratio", 1)
		if paired == filepath.Base(p) {
			paired = strings.Replace(paired, "ratio", "bytes", 1)
		}
		return os.WriteFile(filepath.Join(m.sysctl, paired), []byte("0"), 0644)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, mem
}
func TestMemoryBudgetAndCacheAccounting(t *testing.T) {
	m, mem := testMemoryPolicy(t)
	if s := m.Status(); s.Settings.CachePercent != 20 || s.BudgetBytes != 2<<30 || s.ReserveBytes != 2<<30 {
		t.Fatal(s)
	}
	for percent := 10; percent <= 90; percent += 10 {
		if err := m.Set(MemorySettings{percent}); err != nil {
			t.Fatal(err)
		}
	}
	for _, percent := range []int{-10, 0, 15, 100} {
		if err := m.Set(MemorySettings{percent}); !errors.Is(err, ErrMemoryPercent) {
			t.Fatal(percent, err)
		}
	}
	mem.AvailableBytes = 1 << 30
	if err := m.refreshLocked(false); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Paused {
		t.Fatal("transfers not paused")
	}
	value, _ := os.ReadFile(filepath.Join(m.sysctl, "dirty_bytes"))
	if string(value) != "1048576\n" {
		t.Fatal(string(value))
	}
	meminfo := parseMeminfo("MemTotal: 10000 kB\nMemFree: 1000 kB\nMemAvailable: 5000 kB\nCached: 4000 kB\nShmem: 1000 kB\nBuffers: 500 kB\nSReclaimable: 250 kB\nDirty: 100 kB\nWriteback: 50 kB")
	if meminfo.CacheBytes != 3750*1024 || meminfo.UsedBytes != 9000*1024 || meminfo.DirtyBytes != 100*1024 || meminfo.WritebackBytes != 50*1024 {
		t.Fatal(meminfo)
	}
}
func TestMemoryPolicyRollbackAndDev(t *testing.T) {
	m, _ := testMemoryPolicy(t)
	original, _ := os.ReadFile(filepath.Join(m.sysctl, "dirty_bytes"))
	realWrite := m.write
	failOnce := true
	m.write = func(p string, b []byte) error {
		if failOnce && strings.HasSuffix(p, "dirty_background_bytes") {
			failOnce = false
			return errors.New("write denied")
		}
		return realWrite(p, b)
	}
	if err := m.Set(MemorySettings{40}); err == nil {
		t.Fatal("accepted failed write")
	}
	after, _ := os.ReadFile(filepath.Join(m.sysctl, "dirty_bytes"))
	if string(after) != string(original) || m.Status().Settings.CachePercent != 20 {
		t.Fatal("no rollback")
	}
	bad := filepath.Join(t.TempDir(), "file")
	os.WriteFile(bad, nil, 0600)
	m.store = config.Open(bad)
	if err := m.Set(MemorySettings{40}); err == nil {
		t.Fatal("accepted failed config save")
	}
	after, _ = os.ReadFile(filepath.Join(m.sysctl, "dirty_bytes"))
	if string(after) != string(original) {
		t.Fatal("save rollback failed")
	}
	m.Disabled = true
	m.write = func(string, []byte) error { t.Fatal("dev wrote host settings"); return nil }
	if err := m.refreshLocked(true); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(MemorySettings{20}); err == nil {
		t.Fatal("dev accepted settings")
	}
}
func TestMemoryLeasesBoundedAndCancelled(t *testing.T) {
	m, mem := testMemoryPolicy(t)
	mem.TotalBytes = 1 << 30
	mem.AvailableBytes = (256 << 20) + (16 << 20)
	var releases []func()
	var total int64
	for i := 0; i < 4; i++ {
		n, release, err := m.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		total += n
	}
	if uint64(total) > m.Status().BudgetBytes {
		t.Fatal("exceeded budget", total)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := m.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	for _, release := range releases {
		release()
		release()
	}
	if m.Status().InFlightBytes != 0 {
		t.Fatal("leaked reservations")
	}
	// The reserve follows actual memory, not MemFree and not stale config reads.
	mem.AvailableBytes = 0
	if _, _, err := m.Acquire(ctx); err == nil {
		t.Fatal("ignored pressure")
	}
	b, _ := os.ReadFile(filepath.Join(m.sysctl, "dirty_background_bytes"))
	if _, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err != nil {
		t.Fatal(err)
	}
}
