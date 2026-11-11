package volumedata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
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

func adopt(t *testing.T, base, from, to, feature string, mounted []string) Adopted {
	t.Helper()
	result, err := Adopt(context.Background(), base, from, to, feature, mounted)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(result.Left)
	slices.Sort(result.Aside)
	return result
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
	if result := adopt(t, base, "Backup", "fast", "logs", mounted); result.Moved != 3 || len(result.Left)+len(result.Aside) != 0 {
		t.Fatalf("%+v", result)
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
	if entries, _ := os.ReadDir(now); len(entries) != 3 {
		t.Fatalf("temporary entries left: %v", entries)
	}
	// Nothing left to do: no error, nothing created on the old volume.
	if result := adopt(t, base, "Backup", "fast", "logs", mounted); result.Moved != 0 {
		t.Fatalf("second run: %+v", result)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("source folder created again")
	}
}

// Nothing is overwritten and nothing stays split: a name that exists at
// both places with other content is put next to the current one.
func TestAdoptPutsConflictsAsideAndLeavesSpecialFiles(t *testing.T) {
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
	result := adopt(t, base, "Backup", "fast", "activity", mounted)
	if result.Moved != 1 || !slices.Equal(result.Aside, []string{"2026-11-11.jsonl"}) || !slices.Equal(result.Left, []string{"folder", "link"}) {
		t.Fatalf("%+v", result)
	}
	if data, _ := os.ReadFile(filepath.Join(now, "2026-11-11.jsonl")); string(data) != "new today\n" {
		t.Fatalf("destination overwritten: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(now, "2026-11-11.jsonl.from-Backup")); string(data) != "old today\n" {
		t.Fatalf("conflicting entry not kept next to the current one: %q", data)
	}
	if _, err := os.Stat(filepath.Join(old, "2026-11-11.jsonl")); !os.IsNotExist(err) {
		t.Fatal("conflicting entry stayed on the old volume: the data is split")
	}
	if data, _ := os.ReadFile(filepath.Join(old, "folder", "file")); string(data) != "x" {
		t.Fatal("folder with a link inside was removed at the source")
	}
	for _, name := range []string{"link", "folder"} {
		if _, err := os.Lstat(filepath.Join(now, name)); !os.IsNotExist(err) {
			t.Fatalf("%s copied", name)
		}
	}
	// The same conflict again (the choice went back and forth): the entry
	// put aside before is not overwritten, the new one stays and is named.
	put(t, filepath.Join(old, "2026-11-11.jsonl"), "third version\n")
	result = adopt(t, base, "Backup", "fast", "activity", mounted)
	if !slices.Contains(result.Left, "2026-11-11.jsonl") {
		t.Fatalf("%+v", result)
	}
	if data, _ := os.ReadFile(filepath.Join(now, "2026-11-11.jsonl.from-Backup")); string(data) != "old today\n" {
		t.Fatalf("entry put aside was overwritten: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(old, "2026-11-11.jsonl")); string(data) != "third version\n" {
		t.Fatalf("entry that could not be put aside was lost: %q", data)
	}
}

// A power cut during a move: whatever state it left, the next move
// finishes it, for files and folders, and removes what was half copied.
func TestAdoptFinishesAnInterruptedMove(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"Backup", "fast"}
	old := filepath.Join(base, "Backup", ".titanos", "logs")
	now := filepath.Join(base, "fast", ".titanos", "logs")
	// Copied and named, the source not yet removed: a file and a folder.
	for _, dir := range []string{old, now} {
		put(t, filepath.Join(dir, "boot.log"), "same\n")
		put(t, filepath.Join(dir, "boot-crash", "dmesg", "part1"), "oops")
		put(t, filepath.Join(dir, "boot-crash", "part2"), "more")
	}
	// Half copied under the temporary name: a file and a folder.
	put(t, filepath.Join(old, "other.log"), "complete\n")
	put(t, filepath.Join(now, adoptTemp("other.log")), "comp")
	put(t, filepath.Join(old, "other-crash", "part1"), "whole")
	if err := os.MkdirAll(filepath.Join(now, adoptTemp("other-crash")), 0700); err != nil {
		t.Fatal(err)
	}
	// Left by a move of something that is gone at the source meanwhile.
	put(t, filepath.Join(now, adoptTemp("gone.log")), "half")
	if result := adopt(t, base, "Backup", "fast", "logs", mounted); result.Moved != 4 || len(result.Left)+len(result.Aside) != 0 {
		t.Fatalf("%+v", result)
	}
	for name, want := range map[string]string{"boot.log": "same\n", "boot-crash/dmesg/part1": "oops", "boot-crash/part2": "more", "other.log": "complete\n", "other-crash/part1": "whole"} {
		if data, err := os.ReadFile(filepath.Join(now, name)); err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("source of a finished move kept")
	}
	entries, _ := os.ReadDir(now)
	for _, entry := range entries {
		if isAdoptTemp(entry.Name()) {
			t.Fatalf("half copied entry left: %s", entry.Name())
		}
	}
	// Without a move: the leftovers go the first time the folder is used.
	put(t, filepath.Join(now, adoptTemp("x.log")), "half")
	if err := CleanLeftovers(base, "fast", "logs", mounted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(now, adoptTemp("x.log"))); !os.IsNotExist(err) {
		t.Fatal("leftover not cleaned")
	}
	if err := CleanLeftovers(base, "Backup", "logs", mounted); err != nil {
		t.Fatalf("a folder that is not there: %v", err)
	}
}

// A move that is stopped (titand stops, the time limit) keeps every entry
// at one of the two places and can be taken up again.
func TestAdoptStopsWithItsContext(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"Backup", "fast"}
	old := filepath.Join(base, "Backup", ".titanos", "smart")
	put(t, filepath.Join(old, "a.json"), "{}")
	put(t, filepath.Join(old, "b.json"), "{}")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := Adopt(ctx, base, "Backup", "fast", "smart", mounted); !errors.Is(err, context.Canceled) || result.Moved != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	for _, name := range []string{"a.json", "b.json"} {
		if _, err := os.Stat(filepath.Join(old, name)); err != nil {
			t.Fatalf("%s lost by a stopped move", name)
		}
	}
	if result := adopt(t, base, "Backup", "fast", "smart", mounted); result.Moved != 2 {
		t.Fatalf("not taken up again: %+v", result)
	}
}

func TestAdoptRefusesUnsafeSourceAndUnmountedVolumes(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	ctx := context.Background()
	old := filepath.Join(base, "Backup", ".titanos", "smart")
	put(t, filepath.Join(old, "disk.json"), "{}")
	if _, err := Adopt(ctx, base, "Backup", "fast", "smart", []string{"fast"}); err == nil {
		t.Fatal("unmounted source accepted")
	}
	if _, err := Adopt(ctx, base, "Backup", "fast", "smart", []string{"Backup"}); err == nil {
		t.Fatal("unmounted destination accepted")
	}
	if err := os.Chmod(old, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(ctx, base, "Backup", "fast", "smart", []string{"Backup", "fast"}); err == nil {
		t.Fatal("source writable by others accepted")
	}
	if _, err := os.Stat(filepath.Join(old, "disk.json")); err != nil {
		t.Fatal("refused source was changed")
	}
}

// settled asks until the move in the background is over.
func settled(t *testing.T, home *Home, feature string) string {
	t.Helper()
	volume := home.Volume(feature)
	home.Wait()
	if again := home.Volume(feature); again != "" {
		volume = again
	}
	home.Wait()
	return volume
}

func TestHomeMovesInTheBackgroundAndFollowsTheChoice(t *testing.T) {
	base := homeFixture(t, "Backup", "fast", "other")
	mounted := []string{"fast", "Backup"}
	choice := "fast"
	var moved []string
	home := &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { return choice },
		OnMoved: func(feature, from, to string) { moved = append(moved, feature+":"+from+">"+to) }}
	old := filepath.Join(base, "Backup", ".titanos", "notify")
	put(t, filepath.Join(old, "bell.json"), "[]")
	if got := settled(t, home, "notify"); got != "fast" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos", "notify", "bell.json")); err != nil {
		t.Fatalf("not moved from the old default volume: %v", err)
	}
	if !slices.Equal(moved, []string{"notify:Backup>fast"}) {
		t.Fatalf("writer not told: %v", moved)
	}
	// What appears on the old volume later is not looked at again: the
	// sleeping volume is touched once per start.
	put(t, filepath.Join(old, "later.json"), "{}")
	settled(t, home, "notify")
	if _, err := os.Stat(filepath.Join(old, "later.json")); err != nil {
		t.Fatal("old volume touched a second time")
	}
	os.RemoveAll(old)
	// Another feature has its own turn.
	put(t, filepath.Join(base, "Backup", ".titanos", "smart", "disk.json"), "{}")
	settled(t, home, "smart")
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos", "smart", "disk.json")); err != nil {
		t.Fatalf("second feature not moved: %v", err)
	}
	// The choice changes while the NAS runs: the data follows.
	mounted = []string{"fast", "Backup", "other"}
	choice = "other"
	if got := settled(t, home, "notify"); got != "other" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, "other", ".titanos", "notify", "bell.json")); err != nil {
		t.Fatalf("data did not follow the changed choice: %v", err)
	}
	// Back and forth again: what was written meanwhile follows every time,
	// not only the first (nothing stays split over two volumes).
	for round, to := range []string{"fast", "other", "fast", "other"} {
		choice = to
		if got := settled(t, home, "notify"); got != to {
			t.Fatalf("round %d: got %q", round, got)
		}
		dir := filepath.Join(base, to, ".titanos", "notify")
		if _, err := os.Stat(filepath.Join(dir, "bell.json")); err != nil {
			t.Fatalf("round %d: bell not on %s: %v", round, to, err)
		}
		put(t, filepath.Join(dir, "written-on-"+to+".json"), "{}")
	}
	for _, name := range []string{"bell.json", "written-on-fast.json", "written-on-other.json"} {
		if _, err := os.Stat(filepath.Join(base, "other", ".titanos", "notify", name)); err != nil {
			t.Fatalf("%s not with the current choice: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos", "notify")); !os.IsNotExist(err) {
		t.Fatal("something stayed on the volume left behind")
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
	same.Wait()
	if entries, _ := os.ReadDir(filepath.Join(base2, "Backup")); len(entries) != 0 {
		t.Fatalf("something was created: %v", entries)
	}
}

// A move waits for a writer that holds the folder, and while it waits or
// runs writers get no volume: nothing is appended to a file being moved.
func TestHomeMoveAndWritersExcludeEachOther(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"fast", "Backup"}
	var mu sync.Mutex
	choice := "Backup"
	home := &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { mu.Lock(); defer mu.Unlock(); return choice }}
	volume, release := home.Use("activity")
	if volume != "Backup" {
		t.Fatalf("got %q", volume)
	}
	file := filepath.Join(base, "Backup", ".titanos", "activity", "2026-11-11.jsonl")
	put(t, file, "one\n")
	// The choice changes while the writer still holds the folder.
	mu.Lock()
	choice = "fast"
	mu.Unlock()
	if got := home.Volume("activity"); got != "" {
		t.Fatalf("got %q while the move waits for a writer", got)
	}
	if other, releaseOther := home.Use("activity"); other != "" {
		releaseOther()
		t.Fatalf("a second writer got %q while a move waits", other)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("the folder was moved under a writer")
	}
	// The writer finishes its append, then lets go.
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("two\n")
	f.Close()
	release()
	release() // letting go twice is harmless
	home.Wait()
	if got := home.Volume("activity"); got != "fast" {
		t.Fatalf("got %q after the move", got)
	}
	if data, _ := os.ReadFile(filepath.Join(base, "fast", ".titanos", "activity", "2026-11-11.jsonl")); string(data) != "one\ntwo\n" {
		t.Fatalf("the writer's lines did not arrive whole: %q", data)
	}
}

// Volume never runs the move itself: a caller in a request gets its answer
// at once, and after titand began to stop no move is started.
func TestHomeNeverMovesInTheCallerOrWhileStopping(t *testing.T) {
	base := homeFixture(t, "Backup", "fast")
	mounted := []string{"fast", "Backup"}
	put(t, filepath.Join(base, "Backup", ".titanos", "logs", "boot.log"), "lines\n")
	ctx, cancel := context.WithCancel(context.Background())
	home := &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { return "fast" }, Context: ctx}
	cancel()
	if got := home.Volume("logs"); got != "" {
		t.Fatalf("got %q while stopping with a move due", got)
	}
	home.Wait()
	if _, err := os.Stat(filepath.Join(base, "Backup", ".titanos", "logs", "boot.log")); err != nil {
		t.Fatal("a move was started while stopping")
	}
	if _, err := os.Stat(filepath.Join(base, "fast", ".titanos")); !os.IsNotExist(err) {
		t.Fatal("the new place was used before the old data were there")
	}
	// A move blocked by a writer does not hold the caller.
	running := &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { return "fast" }}
	state := running.feature("logs")
	state.io.RLock()
	begin := time.Now()
	if got := running.Volume("logs"); got != "" || time.Since(begin) > 5*time.Second {
		t.Fatalf("got %q after %v", got, time.Since(begin))
	}
	state.io.RUnlock()
	running.Wait()
	if got := running.Volume("logs"); got != "fast" {
		t.Fatalf("got %q after the move", got)
	}
}
