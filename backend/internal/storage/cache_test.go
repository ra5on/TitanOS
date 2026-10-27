package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cacheRunner answers dmsetup status for set-up caches and blkid for PARTUUIDs.
type cacheRunner struct {
	*treeRunner
	active map[string]string // dm name -> status
	dm     []string
}

func (r *cacheRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	switch {
	case name == "dmsetup":
		r.dm = append(r.dm, command)
		if args[0] == "status" {
			if out, ok := r.active[args[1]]; ok {
				return out, nil
			}
			return "", errors.New("No such device")
		}
		if args[0] == "create" {
			r.active[args[1]] = "0 1000 cache 8 10/100 512 0/1000 0 0 0 0 0 0 0 1 writethrough 2 migration_threshold 2048 smq 0 rw -"
		}
		return "", nil
	case name == "blkid" && slices.Contains(args, "PART_ENTRY_UUID"):
		if strings.HasSuffix(args[len(args)-1], "1") {
			return "aaaa-meta\n", nil
		}
		return "bbbb-data\n", nil
	}
	return r.treeRunner.Run(ctx, stdin, name, args...)
}

func cacheManager(t *testing.T) (*Manager, *cacheRunner) {
	t.Helper()
	m, tree := treeManager(t, []blockDevice{tdisk("nvme0n1", "SSD1")})
	runner := &cacheRunner{treeRunner: tree, active: map[string]string{}}
	m.runner = runner
	m.partUUIDDir = t.TempDir()
	m.zero = func(string) error { return nil }
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: runningArrayUUID, FilesystemUUID: "fs"}}
	return m, runner
}

func TestEnableCacheTakesEffectOnMount(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writeback", "", false); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal("not confirmed:", err)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "fast", "", true); !errors.Is(err, ErrInvalidLevel) {
		t.Fatal("unknown mode:", err)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writethrough", "", true); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(runner.commands, "\n")
	if !strings.Contains(commands, "sfdisk --quiet --wipe always /dev/nvme0n1") {
		t.Fatal(commands)
	}
	cfg, _ := m.find("tank")
	if cfg.Cache == nil || cfg.Cache.Mode != "writethrough" || cfg.Cache.MetaUUID != "aaaa-meta" || cfg.Cache.DataUUID != "bbbb-data" {
		t.Fatalf("cache not saved: %+v", cfg.Cache)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writethrough", "", true); !errors.Is(err, ErrExists) {
		t.Fatal("second cache:", err)
	}
	// Without the SSD a read cache mounts the array directly.
	source, err := m.mountSource(ctx, cfg)
	if err != nil || source != "UUID=fs" {
		t.Fatal(source, err)
	}
	// With the SSD: the cache device, made from the partitions and the array.
	for _, uuid := range []string{"aaaa-meta", "bbbb-data"} {
		os.WriteFile(filepath.Join(m.partUUIDDir, uuid), nil, 0o600)
	}
	cachedArray(t, m)
	source, err = m.mountSource(ctx, cfg)
	if err != nil || source != "/dev/mapper/titanos-cache-tank" {
		t.Fatal(source, err)
	}
	if got := strings.Join(runner.dm, "\n"); !strings.Contains(got, "create titanos-cache-tank --table 0 2000 cache "+m.partUUIDPath("aaaa-meta")+" "+m.partUUIDPath("bbbb-data")+" /dev/md127 512 1 writethrough smq 0") {
		t.Fatal(got)
	}
	// The status shows it, and grow refuses the volume.
	view := m.cacheView(cfg)
	if !view.Active || view.TotalBlocks != 1000 || view.Missing {
		t.Fatalf("view: %+v", view)
	}
}

func TestWritebackCacheWithoutSSDKeepsVolumeUnmounted(t *testing.T) {
	m, _ := cacheManager(t)
	missing := ""
	m.OnCacheMissing = func(v string) { missing = v }
	cfg := m.volumes[0]
	cfg.Cache = &CacheConfig{Mode: "writeback", MetaUUID: "aaaa-meta", DataUUID: "bbbb-data"}
	if _, err := m.mountSource(context.Background(), cfg); err == nil || missing != "tank" {
		t.Fatalf("writeback without SSD mounted: %v %q", err, missing)
	}
}

func TestDisableWritebackCacheWritesBackFirst(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	m.volumes[0].Cache = &CacheConfig{Mode: "writeback", MetaUUID: "aaaa-meta", DataUUID: "bbbb-data"}
	cachedArray(t, m)
	runner.active["titanos-cache-tank"] = "0 2000 cache 8 10/100 512 50/1000 10 5 3 2 0 0 7 1 writeback 2 migration_threshold 2048 smq 0 rw -"
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.dm, "\n"); !strings.Contains(got, "reload titanos-cache-tank --table 0 2000 cache") || !strings.Contains(got, "writeback cleaner 0") || !strings.Contains(got, "resume titanos-cache-tank") {
		t.Fatal(got)
	}
	if cfg, _ := m.find("tank"); cfg.Cache == nil || !cfg.Cache.Removing {
		t.Fatal("forgotten before the dirty blocks were written back")
	}
	if view := parseCacheStatus(runner.active["titanos-cache-tank"]); view.DirtyBlocks != 7 || view.ReadHitRate < 0.66 || view.ReadHitRate > 0.67 {
		t.Fatalf("status: %+v", view)
	}
	// Once clean the cleaner forgets the cache.
	runner.active["titanos-cache-tank"] = strings.Replace(runner.active["titanos-cache-tank"], " 0 0 7 1 ", " 0 0 0 1 ", 1)
	cctx, cancel := context.WithCancel(ctx)
	removed := make(chan string, 1)
	m.OnCacheRemoved = func(v string) { removed <- v; cancel() }
	m.RunCacheCleaner(cctx)
	if cfg, _ := m.find("tank"); cfg.Cache != nil || <-removed != "tank" {
		t.Fatal("clean cache not removed")
	}
}

// cachedArray makes md127 (2000 sectors) appear in the fake sysfs.
func cachedArray(t *testing.T, m *Manager) {
	t.Helper()
	for path, content := range map[string]string{"md127/size": "2000", "md127/md/level": "raid1", "md127/md/array_state": "clean", "md127/md/degraded": "0", "md127/md/raid_disks": "2", "md127/md/sync_action": "idle"} {
		full := filepath.Join(m.sysBlock, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content+"\n"), 0o644)
	}
}
