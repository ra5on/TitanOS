package storage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestShareQuotasSwitchAndMountOptions(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{
		{Name: "xfsvol", Filesystem: "xfs", FilesystemUUID: "u1"},
		{Name: "extvol", Filesystem: "ext4", FilesystemUUID: "u2"},
		{Name: "mirror", Filesystem: "btrfs", FilesystemUUID: "u3"},
	}
	if err := m.SetShareQuotas("mirror", true); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("btrfs: %v", err)
	}
	if err := m.SetShareQuotas("missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, name := range []string{"xfsvol", "extvol"} {
		if err := m.SetShareQuotas(name, true); err != nil {
			t.Fatal(err)
		}
	}
	reloaded := NewManager(m.store)
	reloaded.Load()
	if v, _ := reloaded.find("extvol"); !v.ShareQuotas {
		t.Fatal("not saved")
	}

	ctx := context.Background()
	xfs, _ := m.find("xfsvol")
	if err := m.mountWith(ctx, xfs, false); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(runner.commands, func(c string) bool { return strings.HasPrefix(c, "mount -t xfs -o noatime,prjquota ") }) {
		t.Fatalf("%q", runner.commands)
	}
	ext, _ := m.find("extvol")
	if err := m.mountWith(ctx, ext, false); err != nil {
		t.Fatal(err)
	}
	tune := slices.Index(runner.commands, "tune2fs -O quota,project -Q prjquota UUID=u2")
	mount := slices.IndexFunc(runner.commands, func(c string) bool { return strings.HasPrefix(c, "mount -t ext4 -o noatime,prjquota ") })
	if tune < 0 || mount < tune {
		t.Fatalf("tune2fs must run before the mount: %q", runner.commands)
	}

	// A refusing tune2fs leaves the volume mountable and says why.
	runner.fail = "tune2fs -O"
	runner.mounted = map[string]bool{}
	if err := m.mountWith(ctx, ext, false); err != nil {
		t.Fatalf("mount blocked by quotas: %v", err)
	}
	if last := runner.commands[len(runner.commands)-1]; !strings.HasPrefix(last, "mount -t ext4 -o noatime UUID=u2 ") {
		t.Fatalf("prjquota without the feature: %q", last)
	}
	m.quotaProbe = func(string) (bool, error) { return false, nil }
	var v Volume
	v.Mounted = true
	m.quotaState(ext, &v)
	if v.ShareQuotas != QuotasPending || !strings.Contains(v.ShareQuotasError, "tune2fs") {
		t.Fatalf("%+v", v)
	}
	m.quotaProbe = func(string) (bool, error) { return true, nil }
	m.quotaState(xfs, &v)
	if v.ShareQuotas != QuotasActive {
		t.Fatalf("%+v", v)
	}
	mirror, _ := m.find("mirror")
	m.quotaState(mirror, &v)
	if v.ShareQuotas != QuotasUnsupported {
		t.Fatalf("%+v", v)
	}
}

func TestExt4ProjectQuotaFeatureDetection(t *testing.T) {
	if !ext4HasProjectQuota("Filesystem volume name:   data\nFilesystem features:      has_journal ext_attr quota project extent\n") {
		t.Fatal("features present")
	}
	if ext4HasProjectQuota("Filesystem features:      has_journal quota extent\n") || ext4HasProjectQuota("") {
		t.Fatal("missing project accepted")
	}
}

func TestQuotaMountOptionNeverBlocksTheMount(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "xfsvol", Filesystem: "xfs", FilesystemUUID: "u1", ShareQuotas: true}}
	runner.fail = "mount -t xfs -o noatime,prjquota "
	xfs, _ := m.find("xfsvol")
	if err := m.mountWith(context.Background(), xfs, false); err != nil {
		t.Fatalf("mount blocked by quotas: %v", err)
	}
	if last := runner.commands[len(runner.commands)-1]; !strings.HasPrefix(last, "mount -t xfs -o noatime UUID=u1 ") {
		t.Fatalf("%q", runner.commands)
	}
	if m.quotaProblems["xfsvol"] == "" {
		t.Fatal("problem not recorded")
	}
}

func TestShareQuotasAreSwitchedOnWhereTheVolumeIsMounted(t *testing.T) {
	ctx := context.Background()
	// Below a cache: tune2fs must go through it, not past it to the array.
	m, runner := cacheManager(t)
	activeCache(t, m, runner, "writeback", 0)
	m.volumes[0].ShareQuotas = true
	if err := m.mountWith(ctx, m.volumes[0], false); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(runner.commands, "\n")
	if !strings.Contains(all, "tune2fs -O quota,project -Q prjquota /dev/mapper/titanos-cache-tank") || strings.Contains(all, "UUID=fs") {
		t.Fatalf("tune2fs not on the mounted device:\n%s", all)
	}

	// Encrypted: the file system is inside the opened LUKS device.
	e, tree, _ := encryptedArray(t)
	if err := e.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	tree.mounted = map[string]bool{}
	tree.commands = nil
	cfg, _ := e.find("tank")
	cfg.ShareQuotas = true
	if err := e.mountWith(ctx, cfg, false); err != nil {
		t.Fatal(err)
	}
	all = strings.Join(tree.commands, "\n")
	if !strings.Contains(all, "tune2fs -O quota,project -Q prjquota "+e.mapperPath("titanos-crypt-tank")) {
		t.Fatalf("tune2fs not inside LUKS:\n%s", all)
	}
}
