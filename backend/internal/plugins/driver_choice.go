package plugins

// The owner chooses any NVIDIA version of NVIDIA's archive (PLAN 31): the NAS
// downloads and builds it (local_build.go), selects it for the next restart
// and rebuilds it by itself after a kernel update. Until the build is done,
// and if it fails, the driver shipped with TitanOS runs.
import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var nvidiaVersionRE = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4}){1,3}$`)

// maxDrivers bounds the list of versions the build environment reports.
const maxDrivers = 500

// driverOf is the upstream NVIDIA version in a plugin version such as
// "580.105.08-titanos-0.46.2-local".
func driverOf(version string) string {
	driver, _, _ := strings.Cut(version, "-")
	return driver
}

// readDrivers takes the versions a check found; a build environment made
// before version choice writes none and the list stays empty.
func (m *Manager) readDrivers(root string) {
	data, err := readSandboxFile(root, "versions.json", 64<<10)
	if err != nil {
		return
	}
	var file struct {
		Drivers []string `json:"drivers"`
	}
	if json.Unmarshal(data, &file) != nil {
		return
	}
	drivers := []string{}
	for _, driver := range file.Drivers {
		if nvidiaVersionRE.MatchString(driver) && !slices.Contains(drivers, driver) && len(drivers) < maxDrivers {
			drivers = append(drivers, driver)
		}
	}
	m.mu.Lock()
	m.driverStatus.Drivers = drivers
	m.mu.Unlock()
}

// ChooseDriver stores the NVIDIA version to use ("" the shipped driver)
// and starts building it; the build selects it for the next restart.
func (m *Manager) ChooseDriver(version string, mounted []string) error {
	if version != "" && !nvidiaVersionRE.MatchString(version) {
		return ErrInvalid
	}
	m.actions.Lock()
	if err := m.choiceAllowed(mounted); err != nil {
		m.actions.Unlock()
		return err
	}
	settings := m.current()
	running := m.installed(settings.Volume)["nvidia"]
	if version != "" {
		select {
		case m.driverRequests <- "choose:" + version:
		default:
			m.actions.Unlock()
			return ErrBusy
		}
	}
	settings.NvidiaDriver = version
	err := m.save(settings)
	m.actions.Unlock()
	if err != nil || version != "" || !running.LocalBuild {
		return err
	}
	// Back to the shipped driver from the next restart on.
	return m.SelectDriver("bundled", mounted)
}

func (m *Manager) choiceAllowed(mounted []string) error {
	if m.Disabled || !m.LocalBuilds {
		return ErrDisabled
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	s := m.current()
	if s.PendingDriver != nil {
		return ErrDriverPending
	}
	if !slices.Contains(mounted, s.Volume) {
		return ErrUnknownVolume
	}
	if m.installed(s.Volume)["nvidia"].Name == "" {
		return ErrNotFound
	}
	return nil
}

// buildChoice builds the chosen version for the running kernel and selects
// it for the next restart. Runs in the driver worker (runDriverUpdates).
func (m *Manager) buildChoice(ctx context.Context, mounted []string, version string) {
	m.mu.Lock()
	m.driverStatus.Target = version
	m.mu.Unlock()
	err := m.checkLocalDriver(ctx, mounted, true, true, version)
	if err == nil {
		m.mu.Lock()
		hash := m.driverStatus.SHA256
		m.mu.Unlock()
		err = m.SelectDriver(hash, mounted)
	}
	if err != nil {
		m.mu.Lock()
		m.driverStatus.State, m.driverStatus.Error = "error", err.Error()
		m.mu.Unlock()
	}
	if m.DriverBuilt != nil {
		m.DriverBuilt(version, err)
	}
}

// rebuildChoice builds the chosen version again when the running driver is
// not it for this release, after a kernel or system update: once per
// release and version, so that a failed build does not repeat every minute.
func (m *Manager) rebuildChoice(ctx context.Context, mounted []string) {
	settings := m.current()
	version := settings.NvidiaDriver
	if version == "" || m.Disabled || !m.LocalBuilds || settings.PendingDriver != nil || !slices.Contains(mounted, settings.Volume) {
		return
	}
	running := m.installed(settings.Volume)["nvidia"]
	if running.Name == "" || running.LocalBuild && driverOf(running.Version) == version && running.TitanOS == m.Release && running.Kernel == m.kernel {
		return
	}
	key := m.Release + "/" + m.kernel + "/" + version
	m.mu.Lock()
	tried := m.choiceTried == key
	m.choiceTried = key
	m.mu.Unlock()
	if !tried {
		m.buildChoice(ctx, mounted, version)
	}
}

// fillDriverVersions adds the shipped and the running driver to status.
func (m *Manager) fillDriverVersions(status *DriverUpdateStatus, volume string, running Manifest) {
	if running.Name != "" {
		status.Running, status.RunningLocal = driverOf(running.Version), running.LocalBuild
	}
	if bundled, err := readManifest(filepath.Join(m.releaseDir(volume, m.key()), "nvidia"+manifestSuffix)); err == nil {
		status.Bundled = driverOf(bundled.Version)
	}
}
