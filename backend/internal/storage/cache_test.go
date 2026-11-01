package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testMetaUUID = "aaaaaaaa-0000-4000-8000-000000000001"
	testDataUUID = "bbbbbbbb-0000-4000-8000-000000000002"
)

// fakeDM plays device-mapper for dmsetup: tables become live on resume,
// and a cache target under the cleaner policy writes its dirty blocks back
// after a number of status reads.
type fakeDM struct {
	live, pending map[string]string
	dirty         map[string]uint64
	// cleanAfter is how many status reads under "cleaner" it takes until
	// the dirty blocks are written back; negative: never.
	cleanAfter int
	// onStatus may change the state before each status read.
	onStatus func(name, mode, policy string)
	log      []string
}

func newFakeDM() *fakeDM {
	return &fakeDM{live: map[string]string{}, pending: map[string]string{}, dirty: map[string]uint64{}}
}

func (d *fakeDM) run(args []string) (string, error) {
	d.log = append(d.log, strings.Join(args, " "))
	name := args[1]
	switch args[0] {
	case "create", "reload":
		table := args[len(args)-1]
		if args[0] == "create" {
			d.live[name] = table
		} else if _, ok := d.live[name]; ok {
			d.pending[name] = table
		} else {
			return "", errors.New("No such device")
		}
	case "suspend":
		if _, ok := d.live[name]; !ok {
			return "", errors.New("No such device")
		}
	case "resume":
		if table, ok := d.pending[name]; ok {
			d.live[name] = table
			delete(d.pending, name)
		}
	case "clear":
		delete(d.pending, name)
	case "remove":
		delete(d.live, name)
		delete(d.dirty, name)
	case "table":
		table, ok := d.live[name]
		if !ok {
			return "", errors.New("No such device")
		}
		return table + "\n", nil
	case "status":
		table, ok := d.live[name]
		if !ok {
			return "", errors.New("No such device")
		}
		fields := strings.Fields(table)
		if fields[2] != "cache" {
			return "0 " + fields[1] + " " + fields[2] + " \n", nil
		}
		mode, policy := fields[8], fields[9]
		if d.onStatus != nil {
			d.onStatus(name, mode, policy)
		}
		if policy == "cleaner" && d.dirty[name] > 0 && d.cleanAfter >= 0 {
			if d.cleanAfter == 0 {
				d.dirty[name] = 0
			} else {
				d.cleanAfter--
			}
		}
		return fmt.Sprintf("0 %s cache 8 10/100 512 50/1000 10 5 3 2 0 0 %d 1 %s 2 migration_threshold 2048 %s 0 rw -\n", fields[1], d.dirty[name], mode, policy), nil
	}
	return "", nil
}

// target of a live table, "" when there is none.
func (d *fakeDM) target(name string) string {
	return target(d.live[name])
}

// cacheRunner answers dmsetup with fakeDM and blkid with PARTUUIDs.
type cacheRunner struct {
	*treeRunner
	*fakeDM
}

func (r *cacheRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	switch {
	case name == "dmsetup":
		return r.fakeDM.run(args)
	case name == "blkid" && slices.Contains(args, "PART_ENTRY_UUID"):
		if strings.HasSuffix(args[len(args)-1], "1") {
			return testMetaUUID + "\n", nil
		}
		return testDataUUID + "\n", nil
	}
	return r.treeRunner.Run(ctx, stdin, name, args...)
}

func (r *cacheRunner) dm() string { return strings.Join(r.fakeDM.log, "\n") }

func cacheManager(t *testing.T) (*Manager, *cacheRunner) {
	t.Helper()
	m, tree := treeManager(t, []blockDevice{tdisk("nvme0n1", "SSD1")})
	runner := &cacheRunner{treeRunner: tree, fakeDM: newFakeDM()}
	m.runner = runner
	m.partUUIDDir = t.TempDir()
	m.zero = func(string) error { return nil }
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: runningArrayUUID, FilesystemUUID: "fs"}}
	m.saveVolumes(m.volumes)
	return m, runner
}

func ssdPresent(t *testing.T, m *Manager) {
	t.Helper()
	for _, uuid := range []string{testMetaUUID, testDataUUID} {
		os.WriteFile(filepath.Join(m.partUUIDDir, uuid), nil, 0o600)
	}
}

// activeCache sets up the cache of tank in the given mode with dirty blocks.
func activeCache(t *testing.T, m *Manager, runner *cacheRunner, mode string, dirty uint64) {
	t.Helper()
	m.volumes[0].Cache = &CacheConfig{Mode: mode, MetaUUID: testMetaUUID, DataUUID: testDataUUID}
	m.saveVolumes(m.volumes)
	ssdPresent(t, m)
	cachedArray(t, m)
	if _, err := m.mountSource(context.Background(), m.volumes[0]); err != nil {
		t.Fatal(err)
	}
	runner.fakeDM.dirty["titanos-cache-tank"] = dirty
	runner.fakeDM.log = nil
}

func TestEnableCacheTakesEffectOnMount(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writeback", idOf(m, "nvme0n1"), false); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal("not confirmed:", err)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "fast", idOf(m, "nvme0n1"), true); !errors.Is(err, ErrInvalidLevel) {
		t.Fatal("unknown mode:", err)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writethrough", idOf(m, "nvme0n1"), true); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(runner.commands, "\n")
	if !strings.Contains(commands, "sfdisk --quiet --wipe always /dev/nvme0n1") {
		t.Fatal(commands)
	}
	cfg, _ := m.find("tank")
	if cfg.Cache == nil || cfg.Cache.Mode != "writethrough" || cfg.Cache.MetaUUID != testMetaUUID || cfg.Cache.DataUUID != testDataUUID {
		t.Fatalf("cache not saved: %+v", cfg.Cache)
	}
	if err := m.EnableCache(ctx, "tank", "nvme0n1", "writethrough", idOf(m, "nvme0n1"), true); !errors.Is(err, ErrExists) {
		t.Fatal("second cache:", err)
	}
	// With the SSD: the cache device, made from the partitions and the array.
	ssdPresent(t, m)
	cachedArray(t, m)
	source, err := m.mountSource(ctx, cfg)
	if err != nil || source != "/dev/mapper/titanos-cache-tank" {
		t.Fatal(source, err)
	}
	if got := runner.dm(); !strings.Contains(got, "create titanos-cache-tank --table 0 2000 cache "+m.partUUIDPath(testMetaUUID)+" "+m.partUUIDPath(testDataUUID)+" /dev/md127 512 1 writethrough smq 0") {
		t.Fatal(got)
	}
	// The status shows it, and grow refuses the volume.
	view := m.cacheView(context.Background(), cfg)
	if !view.Active || view.TotalBlocks != 1000 || view.Missing {
		t.Fatalf("view: %+v", view)
	}
}

func TestEnableCacheRefusesGarbageAsPartUUID(t *testing.T) {
	m, tree := treeManager(t, []blockDevice{tdisk("nvme0n1", "SSD1")})
	m.runner = &warningBlkid{tree}
	m.partUUIDDir = t.TempDir()
	m.zero = func(string) error { return nil }
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: runningArrayUUID, FilesystemUUID: "fs"}}
	if err := m.EnableCache(context.Background(), "tank", "nvme0n1", "writethrough", idOf(m, "nvme0n1"), true); !errors.Is(err, ErrNotPossible) {
		t.Fatal(err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache != nil {
		t.Fatal("stored a warning as PARTUUID")
	}
}

// warningBlkid prints something that is no UUID.
type warningBlkid struct{ *treeRunner }

func (r *warningBlkid) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "blkid" {
		return "blkid: warning: something odd\n", nil
	}
	return r.treeRunner.Run(ctx, stdin, name, args...)
}

func TestReadCacheWithoutSSDIsMarkedStaleAndStartsEmpty(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	var zeroed []string
	m.zero = func(part string) error { zeroed = append(zeroed, part); return nil }
	missing := ""
	m.OnCacheMissing = func(v string) { missing = v }
	m.volumes[0].Cache = &CacheConfig{Mode: "writethrough", MetaUUID: testMetaUUID, DataUUID: testDataUUID}
	m.saveVolumes(m.volumes)
	cachedArray(t, m)

	// The SSD may come later: first the volume waits for it.
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	m.clock = func() time.Time { return now }
	if _, err := m.mountSource(ctx, m.volumes[0]); !errors.Is(err, ErrNotPossible) || missing != "" {
		t.Fatal("did not wait for the SSD:", err, missing)
	}
	if !m.btrfsWaiting() {
		t.Fatal("the volumes count as settled while a cache SSD is awaited")
	}
	now = start.Add(cacheGrace)

	// Then the array is mounted directly, and that is recorded.
	source, err := m.mountSource(ctx, m.volumes[0])
	if err != nil || source != "UUID=fs" || missing != "tank" {
		t.Fatal(source, err, missing)
	}
	cfg, _ := m.find("tank")
	if !cfg.Cache.Stale {
		t.Fatal("not marked stale")
	}
	var saved []VolumeConfig
	if err := m.store.ReadJSON(configFile, &saved); err != nil || saved[0].Cache == nil || !saved[0].Cache.Stale {
		t.Fatalf("stale not on the stick: %+v %v", saved, err)
	}
	if len(zeroed) != 0 || runner.fakeDM.live["titanos-cache-tank"] != "" {
		t.Fatal("cache touched without its SSD")
	}

	// With the SSD back the cache starts empty, then is trusted again.
	ssdPresent(t, m)
	source, err = m.mountSource(ctx, cfg)
	if err != nil || source != "/dev/mapper/titanos-cache-tank" {
		t.Fatal(source, err)
	}
	if !slices.Equal(zeroed, []string{m.partUUIDPath(testMetaUUID)}) {
		t.Fatalf("metadata not cleared: %v", zeroed)
	}
	if cfg, _ := m.find("tank"); cfg.Cache.Stale {
		t.Fatal("still stale after a fresh start")
	}
}

func TestStaleCacheStaysOffWhenMetadataCannotBeCleared(t *testing.T) {
	m, runner := cacheManager(t)
	m.zero = func(string) error { return errors.New("busy") }
	m.volumes[0].Cache = &CacheConfig{Mode: "writethrough", MetaUUID: testMetaUUID, DataUUID: testDataUUID, Stale: true}
	ssdPresent(t, m)
	cachedArray(t, m)
	source, err := m.mountSource(context.Background(), m.volumes[0])
	if err != nil || source != "UUID=fs" || runner.fakeDM.live["titanos-cache-tank"] != "" {
		t.Fatal("stale cache set up", source, err)
	}
}

func TestWritebackCacheWithoutSSDKeepsVolumeUnmounted(t *testing.T) {
	for _, c := range []CacheConfig{
		{Mode: "writeback"},
		// Being written back: dirty blocks may still be on the SSD.
		{Mode: "writethrough", Removing: true},
		{Mode: "writeback", Removing: true},
	} {
		m, _ := cacheManager(t)
		missing := ""
		m.OnCacheMissing = func(v string) { missing = v }
		cfg := m.volumes[0]
		c.MetaUUID, c.DataUUID = testMetaUUID, testDataUUID
		cfg.Cache = &c
		if _, err := m.mountSource(context.Background(), cfg); err == nil || missing != "tank" {
			t.Fatalf("%+v without SSD mounted: %v %q", c, err, missing)
		}
	}
}

func TestDisableWritebackCacheWritesBackFirst(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writeback", 7)
	runner.fakeDM.cleanAfter = -1 // keeps its dirty blocks for now
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if got := runner.dm(); !strings.Contains(got, "reload titanos-cache-tank --table 0 2000 cache") || !strings.Contains(got, "writeback cleaner 0") || !strings.Contains(got, "resume titanos-cache-tank") {
		t.Fatal(got)
	}
	if cfg, _ := m.find("tank"); cfg.Cache == nil || !cfg.Cache.Removing {
		t.Fatal("forgotten before the dirty blocks were written back")
	}
	out, _ := runner.fakeDM.run([]string{"status", "titanos-cache-tank"})
	if view := parseCacheStatus(out); view.DirtyBlocks != 7 || view.ReadHitRate < 0.66 || view.ReadHitRate > 0.67 {
		t.Fatalf("status: %+v", view)
	}
	// A round with dirty blocks keeps the cache.
	if m.finishCacheRemoval(ctx, "tank") {
		t.Fatal("removed with dirty blocks")
	}
	// Once clean the cleaner detaches the cache and only then forgets it.
	runner.fakeDM.dirty["titanos-cache-tank"] = 0
	runner.fakeDM.log = nil
	cctx, cancel := context.WithCancel(ctx)
	removed := make(chan string, 1)
	m.OnCacheRemoved = func(v string) { removed <- v; cancel() }
	m.RunCacheCleaner(cctx)
	if cfg, _ := m.find("tank"); cfg.Cache != nil || <-removed != "tank" {
		t.Fatal("clean cache not removed")
	}
	if got := runner.fakeDM.target("titanos-cache-tank"); got != "linear" {
		t.Fatalf("cache device left as %q: new writes could be dirty again", got)
	}
	got := runner.dm()
	writethrough := strings.Index(got, "writethrough cleaner 0")
	linear := strings.Index(got, "linear /dev/md127 0")
	if writethrough < 0 || linear < writethrough {
		t.Fatalf("must go writethrough before the linear table:\n%s", got)
	}
	// A later mount of the volume goes through the leftover mapping.
	cfg, _ := m.find("tank")
	if source, err := m.mountSource(ctx, cfg); err != nil || source != "/dev/mapper/titanos-cache-tank" {
		t.Fatal(source, err)
	}
}

func TestCacheIsNotForgottenWhenBlocksTurnDirtyAgain(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writeback", 0)
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	// A write lands between the cleaner's look and the switch.
	runner.fakeDM.cleanAfter = -1
	runner.fakeDM.dirty["titanos-cache-tank"] = 3
	if m.finishCacheRemoval(ctx, "tank") {
		t.Fatal("forgotten with dirty blocks")
	}
	if cfg, _ := m.find("tank"); cfg.Cache == nil || !cfg.Cache.Removing {
		t.Fatal("forgotten")
	}
	if runner.fakeDM.target("titanos-cache-tank") != "cache" {
		t.Fatal("detached with dirty blocks")
	}
}

func TestCacheCleanerDoesNotTrustAnUnreadableStatus(t *testing.T) {
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writeback", 0)
	m.volumes[0].Cache.Removing = true
	m.saveVolumes(m.volumes)
	m.runner = &garbledStatus{runner}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m.RunCacheCleaner(ctx)
	if cfg, _ := m.find("tank"); cfg.Cache == nil {
		t.Fatal("forgotten on a status that said nothing")
	}
}

type garbledStatus struct{ *cacheRunner }

func (r *garbledStatus) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "dmsetup" && args[0] == "status" {
		return "device-mapper: warning\n", nil
	}
	return r.cacheRunner.Run(ctx, stdin, name, args...)
}

func TestDisableReadCacheDetachesAtOnce(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writethrough", 0)
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache != nil {
		t.Fatal("read cache kept")
	}
	if runner.fakeDM.target("titanos-cache-tank") != "linear" {
		t.Fatal("not detached")
	}
	if strings.Contains(runner.dm(), "writeback") {
		t.Fatalf("a read cache must never switch to writeback:\n%s", runner.dm())
	}
}

func TestDisableReadCacheWithDirtyBlocksWritesBack(t *testing.T) {
	// Switched from writeback to writethrough with blocks still dirty.
	ctx := context.Background()
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writethrough", 4)
	runner.fakeDM.cleanAfter = -1
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache == nil || !cfg.Cache.Removing {
		t.Fatal("read cache with dirty blocks forgotten")
	}
}

func TestDisableWriteCacheThatIsNotRunning(t *testing.T) {
	ctx := context.Background()
	m, _ := cacheManager(t)
	m.volumes[0].Cache = &CacheConfig{Mode: "writeback", MetaUUID: testMetaUUID, DataUUID: testDataUUID}
	m.saveVolumes(m.volumes)
	// The SSD is there: its blocks can still be written back later.
	ssdPresent(t, m)
	if err := m.DisableCache(ctx, "tank"); !errors.Is(err, ErrNotPossible) {
		t.Fatal(err)
	}
	// The SSD is gone: forgetting it is the only way back to the array.
	os.RemoveAll(m.partUUIDDir)
	if err := m.DisableCache(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache != nil {
		t.Fatal("kept")
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

func TestOwnsTheCachePartitions(t *testing.T) {
	m, _ := cacheManager(t)
	if m.Owns(testMetaUUID) {
		t.Fatal("owned before the cache exists")
	}
	m.volumes[0].Cache = &CacheConfig{Mode: "writethrough", MetaUUID: testMetaUUID, DataUUID: testDataUUID}
	for _, uuid := range []string{testMetaUUID, strings.ToUpper(testDataUUID)} {
		if !m.Owns(uuid) {
			t.Fatalf("cache partition %s not owned", uuid)
		}
	}
}

func TestWriteCacheBecomesReadCacheOnlyWhenClean(t *testing.T) {
	ctx := context.Background()
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writeback", 5)
	runner.fakeDM.cleanAfter = -1
	cacheFlushWait, cacheFlushPoll = 0, 0
	t.Cleanup(func() { cacheFlushWait, cacheFlushPoll = 5*time.Minute, 2*time.Second })
	if err := m.SetCacheMode(ctx, "tank", "writethrough"); !errors.Is(err, ErrBusy) {
		t.Fatalf("switched with dirty blocks: %v", err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache.Mode != "writeback" {
		t.Fatalf("saved %q with dirty blocks", cfg.Cache.Mode)
	}
	if table := runner.fakeDM.live["titanos-cache-tank"]; !strings.Contains(table, "writeback smq") {
		t.Fatalf("left as %q", table)
	}
	runner.fakeDM.dirty["titanos-cache-tank"] = 0
	if err := m.SetCacheMode(ctx, "tank", "writethrough"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := m.find("tank"); cfg.Cache.Mode != "writethrough" {
		t.Fatal("clean cache not switched")
	}
	if table := runner.fakeDM.live["titanos-cache-tank"]; !strings.Contains(table, "writethrough smq") {
		t.Fatalf("live %q", table)
	}
}

// zeroWatch records which cache partitions get zeroed and what the cache
// device was at that moment: an SSD may only be cleared once no table
// uses it.
func zeroWatch(m *Manager, runner *cacheRunner) *[]string {
	var zeroed []string
	m.zero = func(part string) error {
		zeroed = append(zeroed, filepath.Base(part)+"@"+runner.fakeDM.target("titanos-cache-tank"))
		return nil
	}
	return &zeroed
}

func TestForgottenCacheClearsTheSSD(t *testing.T) {
	want := []string{testMetaUUID + "@linear", testDataUUID + "@linear"}
	t.Run("read cache", func(t *testing.T) {
		m, runner := cacheManager(t)
		activeCache(t, m, runner, "writethrough", 0)
		zeroed := zeroWatch(m, runner)
		if err := m.DisableCache(context.Background(), "tank"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(*zeroed, want) {
			t.Fatalf("zeroed %v, want %v", *zeroed, want)
		}
	})
	t.Run("write cache", func(t *testing.T) {
		m, runner := cacheManager(t)
		activeCache(t, m, runner, "writeback", 0)
		zeroed := zeroWatch(m, runner)
		if err := m.DisableCache(context.Background(), "tank"); err != nil {
			t.Fatal(err)
		}
		if len(*zeroed) != 0 {
			t.Fatalf("cleared while still in use: %v", *zeroed)
		}
		if !m.finishCacheRemoval(context.Background(), "tank") {
			t.Fatal("not removed")
		}
		if !slices.Equal(*zeroed, want) {
			t.Fatalf("zeroed %v, want %v", *zeroed, want)
		}
	})
	t.Run("dirty write cache", func(t *testing.T) {
		m, runner := cacheManager(t)
		activeCache(t, m, runner, "writeback", 5)
		runner.fakeDM.cleanAfter = -1
		zeroed := zeroWatch(m, runner)
		m.DisableCache(context.Background(), "tank")
		if m.finishCacheRemoval(context.Background(), "tank") || len(*zeroed) != 0 {
			t.Fatalf("SSD with dirty blocks cleared: %v", *zeroed)
		}
	})
}
