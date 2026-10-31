package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// Update replaces an installed plugin with the other version of it that lies
// in a share. The running plugin is stopped first; if the new version cannot
// be put in place or activated, the previous files come back and are switched
// on again. An unsigned version is confirmed by name, like a new community
// plugin. Official plugins come with their system release; NVIDIA has its own
// driver preparation and is never swapped here.
//
// Without manifestPath the newer version comes from the official catalog
// (UpdateOfficial).
func (m *Manager) Update(ctx context.Context, name, manifestPath, confirm string, mounted []string) error {
	if manifestPath == "" {
		return m.UpdateDownload(ctx, name, confirm, mounted)
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	tr := m.track(name, "update", StepSignature, StepChecksum, StepWrite, StepStop)
	err := m.update(withTracker(ctx, tr), name, manifestPath, confirm, mounted)
	tr.finish(err)
	return err
}

func (m *Manager) update(ctx context.Context, name, manifestPath, confirm string, mounted []string) error {
	tr := trackerOf(ctx)
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	old, exists := m.installed(settings.Volume)[name]
	if !exists {
		return ErrNotFound
	}
	real, err := filepath.EvalSymlinks(manifestPath)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") || strings.HasPrefix(real, m.baseDir(settings.Volume)+"/") {
		return ErrNotFound
	}
	manifest, err := readManifest(real)
	if err != nil {
		return err
	}
	if manifest.Name != name || !updatable(old, manifest) {
		return fmt.Errorf("%w: not another version of %s", ErrInvalid, name)
	}
	if !m.compatible(manifest) {
		return fmt.Errorf("%w: the new version targets another kernel", ErrInvalid)
	}
	signed := m.official(real)
	if !signed && confirm != name {
		return ErrNotConfirmed
	}
	tr.name(name, manifest.Title)
	enabled := slices.Contains(settings.Enabled, name)
	if enabled {
		tr.expect(activationSteps(manifest)...)
	}
	if signed {
		tr.begin(StepSignature)
	} else {
		tr.skip(StepSignature)
	}
	return m.replace(ctx, name, real, manifest, signed, enabled, settings.Volume, mounted)
}

// replace swaps the installed plugin name for the verified manifest at
// real and the image next to it, and brings the previous files back if the
// new version cannot be put in place or started. Caller holds actions.
func (m *Manager) replace(ctx context.Context, name, real string, manifest Manifest, signed, enabled bool, volume string, mounted []string) error {
	tr := trackerOf(ctx)
	tr.begin(StepChecksum)
	image := filepath.Join(filepath.Dir(real), name+".raw")
	if sum, size, err := hashFile(image, maxImageBytes); err != nil || size != manifest.Size || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image is not the one the manifest describes", ErrInvalid)
	}
	tr.begin(StepWrite)
	if err := m.ensureReleaseSet(volume); err != nil {
		return err
	}
	dir := m.dir(volume)

	// The new files are copied and checked next to the installed ones
	// before anything running is touched.
	stage, err := os.MkdirTemp(dir, ".update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, name+manifestSuffix)
	if err := copyFile(image, filepath.Join(stage, name+".raw"), 0o600); err != nil {
		return err
	}
	if sum, _, err := hashFile(filepath.Join(stage, name+".raw"), maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image changed while it was copied", ErrInvalid)
	}
	if signed {
		if err := copyFile(real+".minisig", staged+".minisig", 0o600); err != nil {
			return err
		}
	}
	if err := copyFile(real, staged, 0o600); err != nil {
		return err
	}
	if copied, err := readManifest(staged); err != nil || !reflect.DeepEqual(copied, manifest) || (signed && !m.official(staged)) {
		return fmt.Errorf("%w: plugin metadata changed while being copied", ErrInvalid)
	}
	previous, err := os.MkdirTemp(dir, ".previous-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(previous)

	tr.begin(StepStop)
	if err := m.deactivate(ctx, name); err != nil {
		return err // still running as before, nothing was replaced
	}
	files := []string{name + ".raw", name + manifestSuffix + ".minisig", name + manifestSuffix}
	move := func(from, to string) error {
		for _, file := range files {
			if err := os.Rename(filepath.Join(from, file), filepath.Join(to, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return syncDirectory(dir)
	}
	putBack := func(cause error) error {
		// The failing step keeps its mark; bringing the old files back is a
		// step of its own, not followed by the steps of the new version.
		if trackerOf(ctx) != nil {
			tr.fail(cause)
			tr.begin(StepRollback)
			ctx = withTracker(ctx, nil)
		}
		// Stop what of the new version may have started, without the
		// restart-on-failure of deactivate: it is not going to stay.
		if manifest, resident := m.resident[name]; resident {
			for i := len(manifest.Services) - 1; i >= 0; i-- {
				_, _ = m.runner.Run(ctx, "", "systemctl", "stop", manifest.Services[i]) // cleanup, best effort: nothing more can be done if it fails
			}
			for i := len(manifest.Modules) - 1; i >= 0; i-- {
				_, _ = m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, "-r", manifest.Modules[i]) // cleanup, best effort: nothing more can be done if it fails
			}
		}
		delete(m.active, name)
		delete(m.resident, name)
		delete(m.problems, name)
		for _, file := range files {
			os.Remove(filepath.Join(dir, file))
		}
		err := move(previous, dir)
		m.applied = "-"
		err = errors.Join(err, m.apply(ctx, mounted))
		if tr != nil {
			if err != nil {
				tr.fail(err)
			} else {
				tr.complete()
			}
		}
		return errors.Join(fmt.Errorf("the new version was not installed, the previous one is back: %w", cause), err)
	}
	// Swapping the files is part of laying the new version over the system.
	if enabled {
		tr.begin(StepSysext)
	} else {
		tr.complete()
	}
	if err := move(dir, previous); err != nil {
		return putBack(err)
	}
	if err := move(stage, dir); err != nil {
		return putBack(err)
	}
	m.applied = "-"
	err = m.apply(ctx, mounted)
	// An error of another plugin is reported, but does not undo this one.
	if _, active := m.active[name]; enabled && !active {
		if err == nil {
			tr.begin(StepHealth)
		}
		return putBack(errors.Join(errors.New("activation failed"), err))
	}
	if enabled {
		tr.begin(StepHealth)
		tr.complete()
	}
	return err
}
