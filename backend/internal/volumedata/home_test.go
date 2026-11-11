package volumedata

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func homeFixture(t *testing.T, volumes ...string) string {
	t.Helper()
	base := t.TempDir()
	for _, volume := range volumes {
		if err := os.Mkdir(filepath.Join(base, volume), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptMovesFilesAndFolders(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"Backup", "fast"}
	old := filepath.Join(base, "Backup", ".titanos", "logs")
	put(t, filepath.Join(old, "boot1.log"), "lines\n")
	put(t, filepath.Join(old, "boot1-crash", "dmesg", "part1"), "oops")
	put(t, filepath.Join(old, "running"), "boot1 2026-11-11T10:00:00Z\n")
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(old, "boot1.log"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	moved, left, err := Adopt(base, "Backup", "fast", "logs", mounted)
	if err != nil || moved != 3 || len(left) != 0 {
		t.Fatalf("moved=%d left=%v err=%v", moved, left, err)
	}
	now := filepath.Join(base, "fast", ".titanos", "logs")
	for name, want := range map[string]string{"boot1.log": "lines\n", "boot1-crash/dmesg/part1": "oops", "running": "boot1 2026-11-11T10:00:00Z\n"} {
		if data, err := os.ReadFile(filepath.Join(now, name)); err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	if info, err := os.Stat(filepath.Join(now, "boot1.log")); err != nil || !info.ModTime().Equal(stamp) || info.Mode().Perm() != 0600 {
		t.Fatalf("time or mode not kept: %v %v", info, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("emptied source folder still there: %v", err)
	}
	// Nothing left to do: no error, nothing created on the old volume.
	if moved, left, err := Adopt(base, "Backup", "fast", "logs", mounted); err != nil || moved != 0 || left != nil {
		t.Fatalf("second run: moved=%d left=%v err=%v", moved, left, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("source folder created again")
	}
}

func TestAdoptNeverOverwritesAndLeavesSpecialFiles(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"Backup", "fast"}
	old := filepath.Join(base, "Backup", ".titanos", "activity")
	now := filepath.Join(base, "fast", ".titanos", "activity")
	put(t, filepath.Join(old, "2026-11-10.jsonl"), "old day\n")
	put(t, filepath.Join(old, "2026-11-11.jsonl"), "old today\n")
	put(t, filepath.Join(now, "2026-11-11.jsonl"), "new today\n")
	if err := os.Symlink("/etc/passwd", filepath.Join(old, "link")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(old, "folder", "file"), "x")
	if err := os.Symlink("/etc/passwd", filepath.Join(old, "folder", "inner")); err != nil {
		t.Fatal(err)
	}
	moved, left, err := Adopt(base, "Backup", "fast", "activity", mounted)
	slices.Sort(left)
	if err != nil || moved != 1 || !slices.Equal(left, []string{"2026-11-11.jsonl", "folder", "link"}) {
		t.Fatalf("moved=%d left=%v err=%v", moved, left, err)
	}
	if data, _ := os.ReadFile(filepath.Join(now, "2026-11-11.jsonl")); string(data) != "new today\n" {
		t.Fatalf("destination overwritten: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(old, "2026-11-11.jsonl")); string(data) != "old today\n" {
		t.Fatalf("source of a conflict lost: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(old, "folder", "file")); string(data) != "x" {
		t.Fatal("folder with a link inside was removed at the source")
	}
	if _, err := os.Lstat(filepath.Join(now, "link")); !os.IsNotExist(err) {
		t.Fatal("link copied")
	}
}

func TestAdoptFinishesAnInterruptedMove(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	old := filepath.Join(base, "Backup", ".titanos", "logs", "boot.log")
	now := filepath.Join(base, "fast", ".titanos", "logs", "boot.log")
	put(t, old, "same\n")
	put(t, now, "same\n")
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	os.Chtimes(old, stamp, stamp)
	os.Chtimes(now, stamp, stamp)
	moved, left, err := Adopt(base, "Backup", "fast", "logs", []string{"Backup", "fast"})
	if err != nil || moved != 1 || len(left) != 0 {
		t.Fatalf("moved=%d left=%v err=%v", moved, left, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("source of a finished copy kept")
	}
	if data, _ := os.ReadFile(now); string(data) != "same\n" {
		t.Fatalf("copy changed: %q", data)
	}
}

func TestAdoptRefusesUnsafeSourceAndUnmountedVolumes(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	old := filepath.Join(base, "Backup", ".titanos", "smart")
	put(t, filepath.Join(old, "disk.json"), "{}")
	if _, _, err := Adopt(base, "Backup", "fast", "smart", []string{"fast"}); err == nil {
		t.Fatal("unmounted source accepted")
	}
	if _, _, err := Adopt(base, "Backup", "fast", "smart", []string{"Backup"}); err == nil {
		t.Fatal("unmounted destination accepted")
	}
	if err := os.Chmod(old, 0777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Adopt(base, "Backup", "fast", "smart", []string{"Backup", "fast"}); err == nil {
		t.Fatal("source writable by others accepted")
	}
	if _, err := os.Stat(filepath.Join(old, "disk.json")); err != nil {
		t.Fatal("refused source was changed")
	}
}

func TestHomeMovesOncePerStartAndFollowsTheChoice(t *testing.T) {
	base := homeFixture(t, "Backup", "fast", "other")
	mounted := []string{"fast", "Backup"}
	choice := "fast"
	home := &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { return choice }}
	old := filepath.Join(base, "Backup", ".titanos", "notify")
	put(t, filepath.Join(old, "bell.json"), "[]")
	if got := home.Volume("notify"); got != "fast" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos", "notify", "bell.json")); err != nil {
		t.Fatalf("not moved from the old default volume: %v", err)
	}
	// What appears on the old volume later is not looked at again: the
	// sleeping volume is touched once per start.
	put(t, filepath.Join(old, "later.json"), "{}")
	home.Volume("notify")
	if _, err := os.Stat(filepath.Join(old, "later.json")); err != nil {
		t.Fatal("old volume touched a second time")
	}
	// Another feature has its own turn.
	put(t, filepath.Join(base, "Backup", ".titanos", "smart", "disk.json"), "{}")
	home.Volume("smart")
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos", "smart", "disk.json")); err != nil {
		t.Fatalf("second feature not moved: %v", err)
	}
	// The choice changes while the NAS runs: the data follows.
	mounted = []string{"fast", "Backup", "other"}
	choice = "other"
	if got := home.Volume("notify"); got != "other" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, "other", ".titanos", "notify", "bell.json")); err != nil {
		t.Fatalf("data did not follow the changed choice: %v", err)
	}
	// No default yet, or the chosen volume just went away: no volume.
	choice = ""
	if got := home.Volume("notify"); got != "" {
		t.Fatalf("got %q without a choice", got)
	}
	choice, mounted = "other", []string{"fast"}
	if got := home.Volume("notify"); got != "" {
		t.Fatalf("got %q for a volume that is not mounted", got)
	}
	// The first volume by name is the choice: nothing to move, nothing created.
	base2 := homeFixture(t, "Backup")
	same := &Home{MountRoot: base2, Mounted: func() []string { return []string{"Backup"} }, Choose: func() string { return "Backup" }}
	if got := same.Volume("logs"); got != "Backup" {
		t.Fatalf("got %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(base2, "Backup")); len(entries) != 0 {
		t.Fatalf("something was created: %v", entries)
	}
}
