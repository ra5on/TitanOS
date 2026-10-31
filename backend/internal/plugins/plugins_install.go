package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

func hashFile(path string, limit int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, limit+1))
	return hex.EncodeToString(hash.Sum(nil)), n, err
}

// Install copies a plugin found on a volume into the plugin folder and
// switches it on. confirm has to be the name of the plugin: nothing has
// checked what is in it.
func (m *Manager) Install(ctx context.Context, manifestPath, volume, confirm string, mounted []string) error {
	return m.InstallWithActivation(ctx, manifestPath, volume, confirm, mounted, true)
}

// InstallWithActivation can store a verified plugin without touching live
// drivers, services or sysext. Devices already in use remain with their owners.
func (m *Manager) InstallWithActivation(ctx context.Context, manifestPath, volume, confirm string, mounted []string, activate bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	tr := m.track("", "install", StepSignature, StepChecksum, StepWrite)
	err := m.install(withTracker(ctx, tr), manifestPath, volume, confirm, mounted, activate)
	tr.finish(err)
	return err
}

// install is shared by local files and verified downloads. Caller holds actions.
func (m *Manager) install(ctx context.Context, manifestPath, volume, confirm string, mounted []string, activate bool) error {
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	if m.Disabled {
		return ErrDisabled
	}
	real, err := filepath.EvalSymlinks(manifestPath)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") {
		return ErrNotFound
	}
	manifest, err := readManifest(real)
	if err != nil {
		return err
	}
	tr := trackerOf(ctx)
	tr.name(manifest.Name, manifest.Title)
	// Nothing vouches for an unsigned plugin; whoever installs it says so by name.
	signed := m.official(real)
	if !signed && confirm != manifest.Name {
		return ErrNotConfirmed
	}
	settings := m.current()
	if settings.Volume == "" {
		settings.Volume = volume
	}
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if _, exists := m.installed(settings.Volume)[manifest.Name]; exists {
		return ErrExists
	}
	if activate && m.compatible(manifest) {
		tr.expect(activationSteps(manifest)...)
	}
	if signed {
		tr.begin(StepSignature)
	} else {
		tr.skip(StepSignature)
	}
	tr.begin(StepChecksum)
	image := filepath.Join(filepath.Dir(real), manifest.Name+".raw")
	if sum, size, err := hashFile(image, maxImageBytes); err != nil || size != manifest.Size || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image is not the one the manifest describes", ErrInvalid)
	}

	tr.begin(StepWrite)
	if err := m.ensureReleaseSet(settings.Volume); err != nil {
		return err
	}
	dir := m.dir(settings.Volume)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// The image first, under another name until it is complete; a plugin
	// counts as installed once its manifest is there.
	target := filepath.Join(dir, manifest.Name+".raw")
	if err := copyFile(image, target+".part", 0o600); err != nil {
		os.Remove(target + ".part")
		return err
	}
	// What was copied is what was checked: the source may have changed in between.
	if sum, _, err := hashFile(target+".part", maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) {
		os.Remove(target + ".part")
		return fmt.Errorf("%w: the image changed while it was copied", ErrInvalid)
	}
	if err := os.Rename(target+".part", target); err != nil {
		return err
	}
	copiedPath := filepath.Join(dir, manifest.Name+manifestSuffix)
	// Nothing half-copied stays behind: without its manifest an image is
	// not installed, but it would block the next attempt's disk space.
	discard := func() {
		os.Remove(copiedPath)
		os.Remove(copiedPath + ".minisig")
		os.Remove(target)
	}
	if signed {
		if err := copyFile(real+".minisig", copiedPath+".minisig", 0o600); err != nil {
			discard()
			return err
		}
	}
	if err := copyFile(real, copiedPath, 0o600); err != nil {
		discard()
		return err
	}
	// Recheck the copied metadata too: a writable share may have changed
	// after the original signature/image checks.
	copied, err := readManifest(copiedPath)
	if err != nil || !reflect.DeepEqual(copied, manifest) || (signed && !m.official(copiedPath)) {
		discard()
		return fmt.Errorf("%w: plugin metadata changed while being copied", ErrInvalid)
	}
	// The renames above survive a power cut only once the folder is synced.
	if err := syncDirectory(dir); err != nil {
		discard()
		return err
	}
	// A previous interrupted installation may have left an enabled name.
	settings.Enabled = slices.DeleteFunc(settings.Enabled, func(name string) bool { return name == manifest.Name })
	if activate {
		settings.Enabled = append(settings.Enabled, manifest.Name)
	}
	if err := m.save(settings); err != nil {
		return err
	}
	if !activate {
		return nil
	}
	return m.checkActive(ctx, manifest, m.apply(ctx, mounted))
}

// activationSteps are the steps laying a plugin over the running system.
func activationSteps(manifest Manifest) []string {
	steps := []string{StepSysext}
	if len(manifest.Modules) > 0 {
		steps = append(steps, StepModules)
	}
	if len(manifest.Services) > 0 {
		steps = append(steps, StepServices)
	}
	return append(steps, StepHealth)
}

// checkActive is the health step after an activation: the plugin must be part
// of the running system, as the boot healthcheck expects of enabled plugins.
// err is what apply returned; a failure of another plugin is passed on
// without marking a step of this one.
func (m *Manager) checkActive(ctx context.Context, manifest Manifest, err error) error {
	if !m.compatible(manifest) {
		return err // installed for another kernel: nothing to activate
	}
	tr := trackerOf(ctx)
	if _, active := m.active[manifest.Name]; !active {
		if err == nil {
			tr.begin(StepHealth)
			return fmt.Errorf("%s is not active after activation", manifest.Name)
		}
		return err
	}
	tr.begin(StepHealth)
	tr.complete()
	return err
}

func copyFile(from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	if err := target.Sync(); err != nil {
		target.Close()
		return err
	}
	return target.Close()
}

// SetEnabled switches an installed plugin on or off.
func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	return m.setEnabled(ctx, name, enabled, mounted)
}

func (m *Manager) setEnabled(ctx context.Context, name string, enabled bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if _, exists := m.installed(settings.Volume)[name]; !exists {
		return ErrNotFound
	}
	if err := m.ensureReleaseSet(settings.Volume); err != nil {
		return err
	}
	if !enabled {
		if err := m.deactivate(ctx, name); err != nil {
			return err
		}
	}
	settings.Enabled = slices.DeleteFunc(settings.Enabled, func(other string) bool { return other == name })
	if enabled {
		settings.Enabled = append(settings.Enabled, name)
	}
	if err := m.save(settings); err != nil {
		m.applied = "-"
		// Disabling stopped the driver before persisting. Restore the previous
		// enabled state if the stick could not store the change.
		return errors.Join(err, m.apply(ctx, mounted))
	}
	m.applied = "-" // an explicit action also retries a failed activation
	return m.apply(ctx, mounted)
}

// Remove switches a plugin off and deletes it from the volume.
func (m *Manager) Remove(ctx context.Context, name string, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	tr := m.track(name, "remove", StepStop, StepSysext, StepRemove)
	err := m.remove(withTracker(ctx, tr), name, mounted)
	tr.finish(err)
	return err
}

func (m *Manager) remove(ctx context.Context, name string, mounted []string) error {
	tr := trackerOf(ctx)
	if m.Disabled {
		return ErrDisabled
	}
	if !slices.Contains(mounted, m.current().Volume) {
		return ErrUnknownVolume
	}
	manifest, exists := m.installed(m.current().Volume)[name]
	if !exists {
		return ErrNotFound
	}
	tr.name(name, manifest.Title)
	tr.begin(StepStop)
	if err := m.setEnabled(ctx, name, false, mounted); err != nil {
		return err
	}
	tr.begin(StepRemove)
	dir := m.dir(m.current().Volume)
	// The manifest goes first: without it the plugin no longer counts as
	// installed, even if removing the rest fails.
	files := []string{name + manifestSuffix, name + manifestSuffix + ".minisig", name + ".raw"}
	if name == "nvidia" {
		files = append(files, localReceipt)
	}
	var errs []error
	for _, file := range files {
		if err := os.Remove(filepath.Join(dir, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(append(errs, syncDirectory(dir))...)
}
