package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/minisign/minisigntest"
)

type fixture struct {
	t      *testing.T
	stick  *Stick
	signer minisigntest.Signer
	files  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, signer: minisigntest.New(), files: filepath.Join(root, "downloads")}
	keys := filepath.Join(root, "keys")
	os.MkdirAll(keys, 0o755)
	os.MkdirAll(f.files, 0o755)
	os.WriteFile(filepath.Join(keys, "test.pub"), []byte(f.signer.PublicFile()), 0o644)
	f.stick = &Stick{Dir: filepath.Join(root, "efi/titanos/a/app"), KeyDir: keys, Base: "0.8.0", RunDir: filepath.Join(root, "run")}
	return f
}

// release makes a downloaded app and its signed manifest.
func (f *fixture) release(version, base string, content []byte) (manifest, signature []byte, binary string) {
	f.t.Helper()
	binary = filepath.Join(f.files, "titand-"+version)
	os.WriteFile(binary, content, 0o755)
	sum := sha256.Sum256(content)
	manifest, _ = json.Marshal(map[string]any{"version": version, "app": Info{Base: base, Asset: "titanos-" + version + "-titand", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}})
	return manifest, []byte(f.signer.Sign(manifest)), binary
}

func (f *fixture) install(version string) {
	f.t.Helper()
	manifest, signature, binary := f.release(version, "0.8.0", []byte("titand "+version))
	if err := f.stick.Install(version, manifest, signature, binary); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) state() State {
	f.t.Helper()
	state, err := f.stick.ReadState()
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func TestBaseRunsWithoutApps(t *testing.T) {
	f := newFixture(t)
	path, version, err := f.stick.Launch()
	if err != nil || path != "" || version != "" || f.stick.Running() != "" {
		t.Errorf("launch without apps: %q %q %v", path, version, err)
	}
	if _, err := os.Stat(f.stick.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("launching the base wrote to the stick")
	}
}

func TestInstallTrialAndConfirm(t *testing.T) {
	f := newFixture(t)
	f.install("0.8.1")
	if s := f.state(); s.Current != "0.8.1" || s.Previous != "" || !s.Trial || s.Tries != 0 {
		t.Fatalf("after install: %+v", s)
	}
	path, version, err := f.stick.Launch()
	if err != nil || version != "0.8.1" || f.stick.Running() != "0.8.1" {
		t.Fatalf("launch: %q %q %v", path, version, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "titand 0.8.1" {
		t.Error("the copy in RAM is not the app")
	}
	if info, _ := os.Stat(path); info.Mode().Perm()&0o100 == 0 {
		t.Error("the copy in RAM cannot be run")
	}
	if s := f.state(); s.Tries != 1 || !s.Trial {
		t.Errorf("a start on trial is not counted: %+v", s)
	}
	if _, err := f.stick.Rollback(); !errors.Is(err, ErrBusy) {
		t.Errorf("rollback during the trial: %v", err)
	}
	if _, err := f.stick.Confirm(true); err != nil {
		t.Fatal(err)
	}
	if s := f.state(); s.Trial || s.Result != ResultInstalled || s.ResultVersion != "0.8.1" {
		t.Errorf("after the check: %+v", s)
	}
	// Told once.
	if result, version, _ := f.stick.TakeResult(); result != ResultInstalled || version != "0.8.1" {
		t.Errorf("result: %q %q", result, version)
	}
	if result, _, _ := f.stick.TakeResult(); result != "" {
		t.Error("a result was told twice")
	}
	// A started app that proved itself is not counted again.
	f.stick.Launch()
	if s := f.state(); s.Tries != 0 {
		t.Errorf("a good app is counted: %+v", s)
	}

	// The next one keeps 0.8.1 as the way back, and older files go.
	f.install("0.8.2")
	f.stick.Confirm(true)
	f.install("0.8.3")
	if s := f.state(); s.Current != "0.8.3" || s.Previous != "0.8.2" {
		t.Errorf("after two more updates: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(f.stick.Dir, "titand-0.8.1")); !errors.Is(err, os.ErrNotExist) {
		t.Error("an app that is neither current nor previous stayed on the stick")
	}
}

func TestAppThatDoesNotStartGoesBack(t *testing.T) {
	f := newFixture(t)
	f.install("0.8.1")
	f.stick.Confirm(true)
	f.install("0.8.2")
	for try := 1; try <= MaxTries; try++ {
		if _, version, _ := f.stick.Launch(); version != "0.8.2" {
			t.Fatalf("start %d ran %q", try, version)
		}
	}
	// The fourth start gives up on it.
	_, version, err := f.stick.Launch()
	if err != nil || version != "0.8.1" {
		t.Fatalf("after %d failed starts: %q %v", MaxTries, version, err)
	}
	if s := f.state(); s.Trial || s.Current != "0.8.1" || s.Result != ResultFailed || s.ResultVersion != "0.8.2" {
		t.Errorf("state after giving up: %+v", s)
	}
}

func TestAppThatIsNotReadyGoesBackToTheBase(t *testing.T) {
	f := newFixture(t)
	f.install("0.8.1")
	if _, err := f.stick.Confirm(false); err != nil {
		t.Fatal(err)
	}
	if s := f.state(); s.Current != "" || s.Result != ResultFailed {
		t.Errorf("after a failed check: %+v", s)
	}
	if _, version, _ := f.stick.Launch(); version != "" {
		t.Errorf("ran %q instead of the base", version)
	}
}

func TestDamagedOrForeignAppsAreNotStarted(t *testing.T) {
	for name, damage := range map[string]func(f *fixture){
		"changed file": func(f *fixture) {
			os.WriteFile(filepath.Join(f.stick.Dir, "titand-0.8.2"), []byte("something else"), 0o755)
		},
		"foreign signature": func(f *fixture) {
			other := minisigntest.New()
			data, _ := os.ReadFile(filepath.Join(f.stick.Dir, "manifest-0.8.2.json"))
			os.WriteFile(filepath.Join(f.stick.Dir, "manifest-0.8.2.json.minisig"), []byte(other.Sign(data)), 0o644)
		},
		"other system image": func(f *fixture) { f.stick.Base = "0.9.0" },
		"missing manifest":   func(f *fixture) { os.Remove(filepath.Join(f.stick.Dir, "manifest-0.8.2.json")) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.install("0.8.1")
			f.stick.Confirm(true)
			f.install("0.8.2")
			f.stick.Confirm(true)
			damage(f)
			_, version, _ := f.stick.Launch()
			want := "0.8.1"
			if name == "other system image" {
				want = ""
			}
			if version != want {
				t.Errorf("ran %q, want %q", version, want)
			}
			if s := f.state(); s.Current != want || s.Result != ResultFailed {
				t.Errorf("state: %+v", s)
			}
		})
	}
}

func TestInstallRefusesWhatDoesNotFit(t *testing.T) {
	f := newFixture(t)
	manifest, signature, binary := f.release("0.8.1", "0.7.9", []byte("titand"))
	if err := f.stick.Install("0.8.1", manifest, signature, binary); !errors.Is(err, ErrUnusable) {
		t.Errorf("app for another system: %v", err)
	}
	manifest, signature, binary = f.release("0.8.1", "0.8.0", []byte("titand"))
	if err := f.stick.Install("0.8.1", manifest, []byte(minisigntest.New().Sign(manifest)), binary); !errors.Is(err, ErrUnusable) {
		t.Errorf("foreign signature: %v", err)
	}
	if err := f.stick.Install("0.8.2", manifest, signature, binary); !errors.Is(err, ErrUnusable) {
		t.Errorf("manifest of another version: %v", err)
	}
	if err := f.stick.Install("../x", manifest, signature, binary); !errors.Is(err, ErrUnusable) {
		t.Errorf("odd version: %v", err)
	}
	if s := f.state(); s.Current != "" || s.Trial {
		t.Errorf("a refused app changed the state: %+v", s)
	}
}

func TestRollbackToPreviousAndBase(t *testing.T) {
	f := newFixture(t)
	if _, err := f.stick.Rollback(); !errors.Is(err, ErrNothing) {
		t.Errorf("rollback without apps: %v", err)
	}
	f.install("0.8.1")
	f.stick.Confirm(true)
	f.install("0.8.2")
	f.stick.Confirm(true)
	f.stick.TakeResult()

	target, err := f.stick.Rollback()
	if err != nil || target != "0.8.1" {
		t.Fatalf("rollback: %q %v", target, err)
	}
	if s := f.state(); s.Current != "0.8.1" || s.Previous != "0.8.2" || !s.Trial || s.Change != "rollback" {
		t.Errorf("after rollback: %+v", s)
	}
	f.stick.Confirm(true)
	if result, version, _ := f.stick.TakeResult(); result != ResultRolledBack || version != "0.8.1" {
		t.Errorf("result: %q %q", result, version)
	}

	// From an app without a previous one, back to the base.
	g := newFixture(t)
	g.install("0.8.1")
	g.stick.Confirm(true)
	if target, err := g.stick.Rollback(); err != nil || target != "" {
		t.Fatalf("rollback to the base: %q %v", target, err)
	}
	if s := g.state(); s.Current != "" || s.Trial || s.Result != ResultRolledBack {
		t.Errorf("after rollback to the base: %+v", s)
	}
	if _, version, _ := g.stick.Launch(); version != "" {
		t.Errorf("ran %q instead of the base", version)
	}
}

func TestReadBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base-version")
	if ReadBase(path) != "" {
		t.Error("an image without the file has a base version")
	}
	os.WriteFile(path, []byte("0.8.0\n"), 0o644)
	if ReadBase(path) != "0.8.0" {
		t.Error("base version not read")
	}
	os.WriteFile(path, []byte("dev\n"), 0o644)
	if ReadBase(path) != "" {
		t.Error("a development image has a base version")
	}
}
