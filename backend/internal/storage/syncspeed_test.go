package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readSys(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncSpeedIsStoredAndApplied(t *testing.T) {
	ctx := context.Background()
	m, _ := replaceFixture(t)
	md := filepath.Join(m.sysBlock, "md127", "md")
	if err := m.SetSyncSpeed(ctx, "data", "turbo"); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("unknown preset: %v", err)
	}
	if err := m.SetSyncSpeed(ctx, "nope", SyncSpeedFast); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown volume: %v", err)
	}
	if err := m.SetSyncSpeed(ctx, "data", SyncSpeedGentle); err != nil {
		t.Fatal(err)
	}
	if readSys(t, filepath.Join(md, "sync_speed_min")) != "1000" || readSys(t, filepath.Join(md, "sync_speed_max")) != "30000" {
		t.Fatal("gentle limits not written")
	}
	// Stored with the volume, shown in the status.
	var saved []VolumeConfig
	if err := m.store.ReadJSON(configFile, &saved); err != nil || saved[0].SyncSpeed != SyncSpeedGentle {
		t.Fatalf("stored %+v %v", saved, err)
	}
	status, err := m.Status(ctx)
	if err != nil || status.Volumes[0].SyncSpeed != SyncSpeedGentle {
		t.Fatalf("status %+v %v", status.Volumes, err)
	}
	// Assembled again: the kernel shows its own values, titand sets them again.
	writeSys(t, filepath.Join(md, "sync_speed_max"), "200000 (system)")
	writeSys(t, filepath.Join(md, "sync_speed_min"), "1000 (system)")
	m.applySyncSpeeds(ctx)
	if readSys(t, filepath.Join(md, "sync_speed_max")) != "30000" {
		t.Fatal("limits not set again after reassembly")
	}
	// The kernel's marker: nothing written while it says local.
	writeSys(t, filepath.Join(md, "sync_speed_max"), "30000 (local)")
	m.applySyncSpeeds(ctx)
	if readSys(t, filepath.Join(md, "sync_speed_max")) != "30000 (local)" {
		t.Fatal("limits written again without need")
	}
	// Normal gives the kernel's values back and is stored as empty.
	if err := m.SetSyncSpeed(ctx, "data", SyncSpeedNormal); err != nil {
		t.Fatal(err)
	}
	if readSys(t, filepath.Join(md, "sync_speed_max")) != "system" || readSys(t, filepath.Join(md, "sync_speed_min")) != "system" {
		t.Fatal("normal does not restore the kernel's values")
	}
	var again []VolumeConfig
	if err := m.store.ReadJSON(configFile, &again); err != nil || again[0].SyncSpeed != "" {
		t.Fatalf("normal stored as %q", again[0].SyncSpeed)
	}
	// Btrfs and RAID 0 have no rebuild.
	m.volumes[0].Level = "raid0"
	if err := m.SetSyncSpeed(ctx, "data", SyncSpeedFast); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("raid0: %v", err)
	}
}

func TestSyncSpeedLeavesSysfsAloneInDevelopmentMode(t *testing.T) {
	ctx := context.Background()
	m, _ := replaceFixture(t)
	m.ReadOnly = true
	md := filepath.Join(m.sysBlock, "md127", "md")
	if err := m.SetSyncSpeed(ctx, "data", SyncSpeedFast); err != nil {
		t.Fatal(err)
	}
	m.applySyncSpeeds(ctx)
	if _, err := os.Stat(filepath.Join(md, "sync_speed_max")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("sysfs written in development mode")
	}
	if m.volumes[0].SyncSpeed != SyncSpeedFast {
		t.Fatal("choice not kept in development mode")
	}
}

func TestSyncSpeedOfUnknownValues(t *testing.T) {
	if syncSpeedOf(VolumeConfig{}) != SyncSpeedNormal || syncSpeedOf(VolumeConfig{SyncSpeed: "later"}) != SyncSpeedNormal || syncSpeedOf(VolumeConfig{SyncSpeed: "fast"}) != SyncSpeedFast {
		t.Fatal("presets")
	}
}
