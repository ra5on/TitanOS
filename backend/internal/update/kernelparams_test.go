package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditOptionsKeepsOtherParameters(t *testing.T) {
	base := "boot=live live-media=/dev/disk/by-label/TITANOS live-media-path=/titanos/a toram=filesystem.squashfs overlay-size=512M"
	cases := []struct{ in, name, value, want string }{
		{base, "i915.enable_guc", "2", base + " i915.enable_guc=2"},
		{base + " i915.enable_guc=3 quiet", "i915.enable_guc", "2", base + " quiet i915.enable_guc=2"},
		{base + " i915.enable_guc=3 quiet i915.enable_guc=1", "i915.enable_guc", "", base + " quiet"},
		{base + " i915.enable_guc", "i915.enable_guc", "", base},
		// Similar names and other i915 parameters stay.
		{base + " i915.enable_gucx=1 i915.force_probe=* xe.enable_guc=1", "i915.enable_guc", "2", base + " i915.enable_gucx=1 i915.force_probe=* xe.enable_guc=1 i915.enable_guc=2"},
		{"  a  b ", "x.y", "", "a b"},
	}
	for _, c := range cases {
		if got := editOptions(c.in, c.name, c.value); got != c.want {
			t.Errorf("editOptions(%q, %q, %q)\n got %q\nwant %q", c.in, c.name, c.value, got, c.want)
		}
	}
	if v, ok := optionValue("a i915.enable_guc=3 b i915.enable_guc=2", "i915.enable_guc"); !ok || v != "2" {
		t.Errorf("the last value counts: %q %v", v, ok)
	}
	if _, ok := optionValue("a b", "i915.enable_guc"); ok {
		t.Error("absent parameter found")
	}
}

func TestWithParamEditsOnlyTheOptions(t *testing.T) {
	entry := "title    TitanOS\nversion  1.0.0\nsort-key titanos-1\nlinux    /titanos/a/vmlinuz\ninitrd   /titanos/a/initrd.img\noptions  boot=live live-media-path=/titanos/a\n"
	got := withParam(entry, "i915.enable_guc", "2")
	want := strings.Replace(entry, "live-media-path=/titanos/a\n", "live-media-path=/titanos/a i915.enable_guc=2\n", 1)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if again := withParam(got, "i915.enable_guc", "2"); again != got {
		t.Errorf("setting the same value changed the entry:\n%s", again)
	}
	if back := withParam(got, "i915.enable_guc", ""); back != entry {
		t.Errorf("taking it out:\n%q\nwant\n%q", back, entry)
	}
	// Several options lines: systemd-boot joins them; the parameter ends up
	// once, on the last line.
	two := "linux /x\noptions a i915.enable_guc=3\noptions b\n"
	if got := withParam(two, "i915.enable_guc", "2"); got != "linux /x\noptions a\noptions b i915.enable_guc=2\n" {
		t.Errorf("two lines: %q", got)
	}
	if got := entryOptions(two); got != "a i915.enable_guc=3 b" {
		t.Errorf("entryOptions: %q", got)
	}
	if got := withParam("linux /x\n", "i915.enable_guc", "2"); got != "linux /x\noptions  i915.enable_guc=2\n" {
		t.Errorf("no options line: %q", got)
	}
	if got := withParam("linux /x\n", "i915.enable_guc", ""); got != "linux /x\n" {
		t.Errorf("nothing to take out: %q", got)
	}
}

func readEntry(t *testing.T, e *env, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.boot, "loader/entries", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestKernelParamGoesIntoEveryEntryAndSurvivesUpdates(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.boot, "loader/entries/titanos-a.conf")
	os.WriteFile(path, []byte("title    TitanOS\nversion  1.0.0\nsort-key titanos-1\nlinux    /titanos/a/vmlinuz\noptions  boot=live live-media-path=/titanos/a quiet\n"), 0o644)
	if _, err := e.m.SetKernelParam("i915.enable_guc", "2;rm"); !errors.Is(err, ErrParam) {
		t.Errorf("invalid value: %v", err)
	}
	if _, err := e.m.SetKernelParam("enable_guc", "2"); !errors.Is(err, ErrParam) {
		t.Errorf("name without module: %v", err)
	}
	state, err := e.m.SetKernelParam("i915.enable_guc", "2")
	if err != nil || state.Deferred || state.Entries["a"] != "2" {
		t.Fatalf("set: %+v %v", state, err)
	}
	if got := readEntry(t, e, "titanos-a.conf"); !strings.Contains(got, "options  boot=live live-media-path=/titanos/a quiet i915.enable_guc=2\n") {
		t.Errorf("entry a:\n%s", got)
	}
	// Unchanged: the stick is not written again.
	info, _ := os.Stat(path)
	os.Chtimes(path, info.ModTime().Add(-3600e9), info.ModTime().Add(-3600e9))
	before, _ := os.Stat(path)
	if _, err := e.m.SetKernelParam("i915.enable_guc", "2"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged entry was written again")
	}

	// A full update writes the new entry from the manifest, with the parameter.
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.install(t)
	if got := readEntry(t, e, "titanos-b+3.conf"); !strings.Contains(got, "live-media-path=/titanos/b toram=filesystem.squashfs i915.enable_guc=2\n") {
		t.Errorf("updated entry:\n%s", got)
	}
	if got := e.m.KernelParam("i915.enable_guc"); got.Entries["a"] != "2" || got.Entries["b"] != "2" {
		t.Errorf("state: %+v", got)
	}

	// Changing it again reaches the pending entry too and keeps its counter.
	if _, err := e.m.SetKernelParam("i915.enable_guc", "3"); err != nil {
		t.Fatal(err)
	}
	if got := readEntry(t, e, "titanos-b+3.conf"); !strings.Contains(got, "i915.enable_guc=3\n") || strings.Contains(got, "enable_guc=2") {
		t.Errorf("pending entry:\n%s", got)
	}
	// Automatic again: taken out everywhere, the rest stays.
	if _, err := e.m.SetKernelParam("i915.enable_guc", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"titanos-a.conf", "titanos-b+3.conf"} {
		if got := readEntry(t, e, name); strings.Contains(got, "enable_guc") || !strings.Contains(got, "live-media-path=") {
			t.Errorf("%s:\n%s", name, got)
		}
	}
}

func TestKernelParamWaitsForTheRunningTrialEntry(t *testing.T) {
	e := newEnv(t)
	os.Remove(filepath.Join(e.boot, "loader/entries/titanos-a.conf"))
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-a+2-1.conf"), []byte("sort-key titanos-1\noptions  boot=live\n"), 0o644)
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-b.conf"), []byte("sort-key titanos-2\noptions  boot=live\n"), 0o644)
	m := e.manager(t, "1.0.0", "a", "boot-1")
	state, err := m.SetKernelParam("i915.enable_guc", "2")
	if err != nil || !state.Deferred || state.Entries["a"] != "" || state.Entries["b"] != "2" {
		t.Fatalf("set: %+v %v", state, err)
	}
	if got := readEntry(t, e, "titanos-a+2-1.conf"); strings.Contains(got, "enable_guc") {
		t.Errorf("the trial entry was written:\n%s", got)
	}
	if got := m.KernelParam("i915.enable_guc"); !got.Deferred {
		t.Errorf("state: %+v", got)
	}
	// Once the boot check has confirmed it, the entry follows.
	m.Bless()
	if state, err := m.SetKernelParam("i915.enable_guc", "2"); err != nil || state.Deferred || state.Entries["a"] != "2" {
		t.Fatalf("after bless: %+v %v", state, err)
	}
	if got := readEntry(t, e, "titanos-a.conf"); !strings.Contains(got, "options  boot=live i915.enable_guc=2\n") {
		t.Errorf("confirmed entry:\n%s", got)
	}
}

func TestKernelParamInDevelopmentModeTouchesNothing(t *testing.T) {
	e := newEnv(t)
	e.m.Disabled = true
	if _, err := e.m.SetKernelParam("i915.enable_guc", "2"); !errors.Is(err, ErrDisabled) {
		t.Errorf("err: %v", err)
	}
	if got := readEntry(t, e, "titanos-a.conf"); strings.Contains(got, "enable_guc") {
		t.Errorf("entry written:\n%s", got)
	}
}
