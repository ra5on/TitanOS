// Package app runs a newer titand on the system that is on the stick,
// without a new system image and without a restart of the NAS.
//
// An app update is titand alone, with the web UI built into it. It lies on
// the stick next to the system it was made for, in titanos/<slot>/app/,
// together with the signed manifest of its release. The titand inside the
// system image (the base) starts first, checks the app against the
// manifest and its own keys and hands over to it. A new app is on trial:
// should it not start three times, or not become ready, the previous app
// runs again, or the base itself.
//
// See docs/APP-UPDATES.md.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

const (
	// BaseVersionFile names the version of the system image.
	BaseVersionFile = "/usr/share/titanos/base-version"
	// Env is set for the app the base hands over to, so that it does not
	// look for an app itself.
	Env = "TITANOS_APP"
	// MaxTries is how often a new app is started before it is given up on.
	MaxTries = 3

	stateFile = "state.json"
	maxBinary = 256 << 20
)

// Results of a change, kept until titand has told about it.
const (
	ResultInstalled  = "installed"
	ResultFailed     = "failed"
	ResultRolledBack = "rolled_back"
)

var (
	// ErrBusy means an app is still on trial.
	ErrBusy = errors.New("app: an app update is still on trial")
	// ErrNothing means there is no app to go back to.
	ErrNothing = errors.New("app: nothing to go back to")
	// ErrUnusable means an app on the stick does not pass the checks.
	ErrUnusable = errors.New("app: the app is not usable on this system")

	versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?$`)
)

// State is titanos/<slot>/app/state.json.
type State struct {
	// Current is the app that starts; empty for the base.
	Current string `json:"current"`
	// Previous is where a failed Current goes back to; empty for the base.
	Previous string `json:"previous"`
	// Trial is set until Current proved itself; Tries counts its starts.
	Trial bool `json:"trial"`
	Tries int  `json:"tries"`
	// Change is update or rollback while on trial.
	Change string `json:"change,omitempty"`
	// Result and ResultVersion tell titand what came of the last change.
	Result        string `json:"result,omitempty"`
	ResultVersion string `json:"resultVersion,omitempty"`
}

// Info is what the manifest of a release says about its app.
type Info struct {
	// Base is the version of the system image the app runs on.
	Base   string `json:"base"`
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// manifest is the part of a release manifest an app needs.
type manifest struct {
	Version string `json:"version"`
	App     *Info  `json:"app"`
}

// Stick is the app folder of one slot.
type Stick struct {
	// Dir is titanos/<slot>/app on the stick.
	Dir string
	// KeyDir holds the keys a manifest must be signed with.
	KeyDir string
	// Base is the version of the running system image.
	Base string
	// RunDir is where the app is copied to before it starts: in RAM, on
	// the root file system, because /run does not allow running programs.
	RunDir string
}

// New returns the app folder of a slot on the stick mounted at bootDir.
func New(bootDir, slot string) *Stick {
	return &Stick{
		Dir:    filepath.Join(bootDir, "titanos", slot, "app"),
		KeyDir: "/usr/share/titanos/update-keys",
		Base:   ReadBase(BaseVersionFile),
		RunDir: "/var/lib/titanos/app",
	}
}

// ReadBase returns the version of the system image, or "" for an image
// from before app updates.
func ReadBase(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	base := strings.TrimSpace(string(data))
	if !versionRE.MatchString(base) {
		return ""
	}
	return base
}

func (s *Stick) binary(version string) string {
	return filepath.Join(s.Dir, "titand-"+version)
}

func (s *Stick) manifest(version string) string {
	return filepath.Join(s.Dir, "manifest-"+version+".json")
}

// ReadState returns the state of the slot; no file is the base alone.
func (s *Stick) ReadState() (State, error) {
	var state State
	data, err := os.ReadFile(filepath.Join(s.Dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	for _, v := range []string{state.Current, state.Previous} {
		if v != "" && !versionRE.MatchString(v) {
			return State{}, fmt.Errorf("app: bad version %q in %s", v, stateFile)
		}
	}
	return state, nil
}

// WriteState replaces the state on the stick: written next to it, flushed,
// renamed over it, and the stick synced.
func (s *Stick) WriteState(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(s.Dir, stateFile), append(data, '\n'), 0o644); err != nil {
		return err
	}
	unix.Sync()
	return nil
}

// Verify checks an app on the stick: its manifest carries a valid
// signature, it was made for this system image, and the file is the one
// the manifest names.
func (s *Stick) Verify(version string) error {
	if !versionRE.MatchString(version) {
		return ErrUnusable
	}
	data, err := os.ReadFile(s.manifest(version))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	signature, err := os.ReadFile(s.manifest(version) + ".minisig")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	return s.check(version, data, signature, s.binary(version))
}

// check verifies a manifest and the file of its app.
func (s *Stick) check(version string, data, signature []byte, file string) error {
	keys := minisign.LoadKeys(s.KeyDir)
	if len(keys) == 0 || minisign.Verify(keys, data, string(signature)) != nil {
		return fmt.Errorf("%w: the manifest is not signed by a trusted key", ErrUnusable)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version != version || m.App == nil {
		return fmt.Errorf("%w: the manifest is not for app %s", ErrUnusable, version)
	}
	if s.Base == "" || m.App.Base != s.Base {
		return fmt.Errorf("%w: made for system %s, this is %q", ErrUnusable, m.App.Base, s.Base)
	}
	sum, size, err := hashFile(file)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	if size != m.App.Size || sum != m.App.SHA256 {
		return fmt.Errorf("%w: the file does not match the manifest", ErrUnusable)
	}
	return nil
}

// Launch decides which titand runs: the app of the slot, if there is one
// that passes the checks, or else "" for the base itself. A new app on
// trial counts one more start; after MaxTries the one before it runs
// again. The chosen app is copied into RAM and that copy is returned.
func (s *Stick) Launch() (string, string, error) {
	state, err := s.ReadState()
	if err != nil {
		return "", "", err
	}
	if state.Current == "" && state.Previous == "" && !state.Trial {
		s.noteRunning("")
		return "", "", nil
	}
	changed := false
	if state.Trial {
		state.Tries++
		changed = true
		if state.Tries > MaxTries {
			slog.Error("app does not start, going back", "app", state.Current, "to", orBase(state.Previous))
			state = fallBack(state)
		}
	}
	version := ""
	for _, candidate := range []string{state.Current, state.Previous} {
		if candidate == "" {
			break
		}
		err := s.Verify(candidate)
		if err == nil {
			version = candidate
			break
		}
		slog.Error("app on the stick not usable", "app", candidate, "err", err)
		if candidate == state.Current {
			state = fallBack(state)
			changed = true
		}
	}
	if changed {
		if err := s.WriteState(state); err != nil {
			// Without the count a broken app could start for ever.
			slog.Error("state of the app not written, starting the base", "err", err)
			s.noteRunning("")
			return "", "", err
		}
	}
	if version == "" {
		s.noteRunning("")
		return "", "", nil
	}
	path, err := s.copyToRun(version)
	if err != nil {
		s.noteRunning("")
		return "", "", err
	}
	s.noteRunning(version)
	return path, version, nil
}

// fallBack gives up on the current app: the previous one runs, or the base.
func fallBack(state State) State {
	return State{Current: state.Previous, Result: ResultFailed, ResultVersion: state.Current}
}

func orBase(version string) string {
	if version == "" {
		return "base"
	}
	return version
}

// copyToRun copies an app into RAM and checks the copy once more: the
// stick may be unplugged while titand runs.
func (s *Stick) copyToRun(version string) (string, error) {
	if err := os.MkdirAll(s.RunDir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(s.RunDir, "titand-"+version)
	// Copies of other apps would only take RAM.
	if old, _ := filepath.Glob(filepath.Join(s.RunDir, "titand-*")); len(old) > 0 {
		for _, path := range old {
			if path != target {
				os.Remove(path)
			}
		}
	}
	source, err := os.Open(s.binary(version))
	if err != nil {
		return "", err
	}
	defer source.Close()
	tmp := target + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, io.LimitReader(source, maxBinary)); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(s.manifest(version))
	if err != nil {
		return "", err
	}
	signature, err := os.ReadFile(s.manifest(version) + ".minisig")
	if err != nil {
		return "", err
	}
	if err := s.check(version, data, signature, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return target, os.Rename(tmp, target)
}

// runningFile notes which app the base handed over to, for the check.
func (s *Stick) runningFile() string { return filepath.Join(s.RunDir, "running") }

func (s *Stick) noteRunning(version string) {
	if version == "" {
		os.Remove(s.runningFile())
		return
	}
	if err := os.MkdirAll(s.RunDir, 0o700); err == nil {
		_ = os.WriteFile(s.runningFile(), []byte(version+"\n"), 0o600) // a runtime or state file only; without it the default applies
	}
}

// Running returns the app the base handed over to at the last start, or
// "" for the base.
func (s *Stick) Running() string {
	data, err := os.ReadFile(s.runningFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Install puts an app onto the stick and makes it the one that starts,
// on trial. The file at binary was downloaded and checked against the
// manifest already; it is moved into place.
func (s *Stick) Install(version string, manifestData, signature []byte, binary string) error {
	if !versionRE.MatchString(version) {
		return ErrUnusable
	}
	state, err := s.ReadState()
	if err != nil {
		return err
	}
	if state.Trial {
		return ErrBusy
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if err := s.check(version, manifestData, signature, binary); err != nil {
		return err
	}
	if err := writeFile(s.manifest(version), manifestData, 0o644); err != nil {
		return err
	}
	if err := writeFile(s.manifest(version)+".minisig", signature, 0o644); err != nil {
		return err
	}
	if err := os.Rename(binary, s.binary(version)); err != nil {
		return err
	}
	if err := s.Verify(version); err != nil {
		return err
	}
	previous := state.Current
	if previous == version {
		previous = state.Previous
	}
	next := State{Current: version, Previous: previous, Trial: true, Change: "update"}
	if err := s.WriteState(next); err != nil {
		return err
	}
	s.prune(next)
	return nil
}

// PartPath is where an app is downloaded to before Install.
func (s *Stick) PartPath(version string) string {
	return s.binary(version) + ".part"
}

// Rollback makes the previous app, or the base, the one that starts. A
// way back to an app is on trial like an update.
func (s *Stick) Rollback() (string, error) {
	state, err := s.ReadState()
	if err != nil {
		return "", err
	}
	if state.Trial {
		return "", ErrBusy
	}
	if state.Current == "" && state.Previous == "" {
		return "", ErrNothing
	}
	if state.Previous != "" {
		if err := s.Verify(state.Previous); err != nil {
			return "", err
		}
	}
	next := State{Current: state.Previous, Previous: state.Current, Trial: state.Previous != "", Change: "rollback"}
	if next.Current == "" {
		next.Result, next.ResultVersion = ResultRolledBack, ""
	}
	if err := s.WriteState(next); err != nil {
		return "", err
	}
	return state.Previous, nil
}

// Confirm settles a trial: the app proved itself, or it is given up on.
func (s *Stick) Confirm(good bool) (State, error) {
	state, err := s.ReadState()
	if err != nil || !state.Trial {
		return state, err
	}
	if good {
		result := ResultInstalled
		if state.Change == "rollback" {
			result = ResultRolledBack
		}
		state = State{Current: state.Current, Previous: state.Previous, Result: result, ResultVersion: state.Current}
	} else {
		state = fallBack(state)
	}
	return state, s.WriteState(state)
}

// TakeResult returns what came of the last change once and forgets it.
func (s *Stick) TakeResult() (string, string, error) {
	state, err := s.ReadState()
	if err != nil || state.Result == "" || state.Trial {
		return "", "", err
	}
	result, version := state.Result, state.ResultVersion
	state.Result, state.ResultVersion = "", ""
	return result, version, s.WriteState(state)
}

// Clear removes every app of the slot: the base runs again. A system
// update does that for the slot it writes, whose image brings its own
// titand.
func (s *Stick) Clear() error {
	return os.RemoveAll(s.Dir)
}

// Size is what the apps of the slot take on the stick.
func (s *Stick) Size() int64 {
	var total int64
	entries, _ := os.ReadDir(s.Dir)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

// prune removes the files of apps that are neither current nor previous.
func (s *Stick) prune(state State) {
	keep := map[string]bool{stateFile: true}
	for _, v := range []string{state.Current, state.Previous} {
		if v != "" {
			keep["titand-"+v] = true
			keep["manifest-"+v+".json"] = true
			keep["manifest-"+v+".json.minisig"] = true
		}
	}
	entries, _ := os.ReadDir(s.Dir)
	for _, entry := range entries {
		if !keep[entry.Name()] {
			os.Remove(filepath.Join(s.Dir, entry.Name()))
		}
	}
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(f, maxBinary+1))
	if err != nil {
		return "", 0, err
	}
	if size > maxBinary {
		return "", 0, errors.New("larger than an app can be")
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// writeFile replaces a file: written next to it, flushed, then renamed.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
