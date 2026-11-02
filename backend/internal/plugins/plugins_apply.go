package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Run lays the enabled plugins over the system once their volume is
// mounted and takes them away before it goes, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	go m.runDriverUpdates(ctx, mounted)
	m.DetectHardware()
	go m.watchDevices(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			_ = m.Load() // Load keeps its own state; a failure shows up as not loaded
		} else if !m.Disabled {
			volumes := mounted()
			if err := m.Apply(ctx, volumes); err != nil {
				slog.Error("plugins", "err", err)
			}
			m.notifyRecommendations(volumes)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Apply makes the system carry exactly the plugins that are switched on,
// installed and fit the kernel.
func (m *Manager) Apply(ctx context.Context, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	return m.apply(ctx, mounted)
}

func (m *Manager) apply(ctx context.Context, mounted []string) (err error) {
	if m.Disabled {
		return nil
	}
	settings := m.current()
	wanted := map[string]Manifest{}
	if slices.Contains(mounted, settings.Volume) {
		for name, manifest := range m.installed(settings.Volume) {
			if slices.Contains(settings.Enabled, name) && m.compatible(manifest) {
				wanted[name] = manifest
			}
		}
	}
	m.withoutConflicts(settings.Enabled, wanted)
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)
	key := ":"
	if len(names) > 0 {
		key = settings.Volume + ":"
	}
	for _, name := range names {
		key += name + "=" + wanted[name].SHA256 + ";"
	}
	if key == m.applied && (m.retryAt.IsZero() || time.Now().Before(m.retryAt)) {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		for name := range wanted {
			if _, active := m.active[name]; !active && m.problems[name] == "" {
				m.problems[name] = "activation"
			}
		}
		m.applied = key
	}()
	tr := trackerOf(ctx)
	target, tracked := wanted[tr.plugin()]
	tr.begin(StepSysext)
	if err := os.MkdirAll(m.extDir, 0o755); err != nil {
		return err
	}
	links, err := os.ReadDir(m.extDir)
	if err != nil {
		return err
	}
	for _, link := range links {
		if !strings.HasSuffix(link.Name(), ".raw") {
			continue
		}
		name := strings.TrimSuffix(link.Name(), ".raw")
		if wanted[name].Name != "" {
			continue
		}
		if err := m.deactivate(ctx, name); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(m.extDir, link.Name())); err != nil {
			return err
		}
		delete(m.resident, name)
		delete(m.problems, name)
	}
	for _, name := range names {
		manifest := wanted[name]
		if _, exists := m.resident[name]; !exists {
			if manifest.LocalBuild {
				if _, err := m.localDriver(m.dir(settings.Volume)); err != nil {
					m.problems[name] = "integrity"
					return fmt.Errorf("%w: local driver receipt", ErrInvalid)
				}
			}
			image := filepath.Join(m.dir(settings.Volume), name+".raw")
			if sum, size, err := hashFile(image, maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) || size != manifest.Size {
				m.problems[name] = "integrity"
				return fmt.Errorf("%w: installed image %s changed", ErrInvalid, name)
			}
		}
		link := filepath.Join(m.extDir, name+".raw")
		if _, err := os.Lstat(link); errors.Is(err, fs.ErrNotExist) {
			if err := os.Symlink(filepath.Join(m.dir(settings.Volume), name+".raw"), link); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if _, err := m.runner.Run(ctx, "", "systemd-sysext", "refresh"); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "ldconfig", "-X"); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "systemctl", "daemon-reload"); err != nil {
		return err
	}
	needsModules := false
	for _, manifest := range wanted {
		needsModules = needsModules || len(manifest.Modules) > 0
	}
	if needsModules {
		if tracked && len(target.Modules) > 0 {
			tr.begin(StepModules)
		}
		if err := m.prepareModules(ctx); err != nil {
			return err
		}
	}
	var activationErrors []error
	for _, name := range names {
		manifest := wanted[name]
		m.resident[name] = manifest
		if _, active := m.active[name]; active {
			continue
		}
		var failure error
		mine := tracked && name == target.Name
		if mine && len(manifest.Modules) > 0 {
			tr.begin(StepModules)
		} else if mine && len(manifest.Services) > 0 {
			tr.begin(StepServices)
		}
		if m.PrepareRuntime != nil {
			failure = m.PrepareRuntime(name, settings.Volume)
		}
		for _, module := range manifest.Modules {
			if failure != nil {
				break
			}
			call, cancel := context.WithTimeout(ctx, activationLimit)
			_, failure = m.runner.Run(call, "", "modprobe", "-d", m.moduleRoot, module)
			cancel()
			if failure != nil {
				break
			}
		}
		if failure == nil {
			if mine && len(manifest.Services) > 0 {
				tr.begin(StepServices)
			}
			for _, service := range manifest.Services {
				call, cancel := context.WithTimeout(ctx, activationLimit)
				_, failure = m.runner.Run(call, "", "systemctl", "start", service)
				cancel()
				if failure != nil {
					break
				}
			}
		}
		if failure == nil && m.ActivateRuntime != nil {
			failure = m.ActivateRuntime(name)
		}
		if failure != nil {
			m.problems[name] = "activation"
			activationErrors = append(activationErrors, fmt.Errorf("%s: %w", name, failure))
		} else {
			m.active[name] = manifest
			delete(m.problems, name)
		}
	}
	// Failed activation remains visible; an explicit Enable retries it. A
	// device may just not be ready yet after the start, so it is tried
	// again three times (after 30, 60 and 90 s), then left alone: do not
	// keep probing absent GPUs or restarting failed services for good.
	m.applied = key
	switch {
	case len(activationErrors) == 0:
		m.retryAt, m.retries = time.Time{}, 0
	case m.retries < 3:
		m.retries++
		m.retryAt = time.Now().Add(time.Duration(m.retries) * 30 * time.Second)
	default:
		m.retryAt = time.Time{}
	}
	return errors.Join(activationErrors...)
}

func (m *Manager) deactivate(ctx context.Context, name string) (err error) {
	manifest, exists := m.resident[name]
	if !exists {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		m.problems[name] = "busy"
		// Keep a running consumer usable if a later module refuses unloading.
		for _, module := range manifest.Modules {
			if _, restoreErr := m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, module); restoreErr != nil {
				delete(m.active, name)
				err = errors.Join(err, restoreErr)
			}
		}
		for _, service := range manifest.Services {
			if _, restoreErr := m.runner.Run(ctx, "", "systemctl", "start", service); restoreErr != nil {
				delete(m.active, name)
				err = errors.Join(err, restoreErr)
			}
		}
	}()
	for i := len(manifest.Services) - 1; i >= 0; i-- {
		if _, failure := m.runner.Run(ctx, "", "systemctl", "stop", manifest.Services[i]); failure != nil {
			return fmt.Errorf("%w: %v", ErrBusy, failure)
		}
	}
	for i := len(manifest.Modules) - 1; i >= 0; i-- {
		if _, failure := m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, "-r", manifest.Modules[i]); failure != nil {
			return fmt.Errorf("%w: %v", ErrBusy, failure)
		}
	}
	delete(m.active, name)
	delete(m.resident, name)
	return nil
}

// sysext makes /usr read-only. Build the module index in RAM with links to
// the merged base/plugin modules, rather than running depmod against /usr.
func (m *Manager) prepareModules(ctx context.Context) error {
	source := filepath.Join(m.moduleSource, m.kernel)
	target := filepath.Join(m.moduleRoot, "lib/modules", m.kernel)
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if rel == "build" || rel == "source" || (strings.HasPrefix(rel, "modules.") && !strings.HasPrefix(rel, "modules.builtin")) {
			return nil
		}
		return os.Symlink(path, destination)
	}); err != nil {
		return err
	}
	_, err := m.runner.Run(ctx, "", "depmod", "-b", m.moduleRoot, m.kernel)
	return err
}

// activationLimit bounds loading one module or starting one service of a
// plugin: a hanging modprobe must not block the plugins for good.
const activationLimit = 2 * time.Minute
