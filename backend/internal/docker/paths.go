package docker

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"syscall"
)

// HostPath is a folder of the NAS a container or app uses.
type HostPath struct {
	// Name is the container, or the app for its data folder.
	Name string `json:"name"`
	Path string `json:"path"`
}

// HostPaths lists the bind and volume mounts of all containers, running or
// not, and the data folders of the apps. An engine that is configured but
// does not answer is an error: what it would mount is unknown. In
// development mode, and with the engine switched off and gone, there are no
// containers.
func (m *Manager) HostPaths(ctx context.Context) ([]HostPath, error) {
	if m.Disabled {
		return []HostPath{}, nil
	}
	settings := m.current()
	out := []HostPath{}
	if settings.Volume != "" {
		m.mu.Lock()
		apps := m.knownApps()
		m.mu.Unlock()
		for _, app := range apps {
			out = append(out, HostPath{Name: app, Path: m.appData(settings.Volume, app)})
		}
	}
	items, err := m.api.containers(ctx)
	if err != nil {
		if !settings.Enabled && (errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)) {
			return out, nil
		}
		return nil, err
	}
	for _, item := range items {
		name := item.ID
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		var inspect struct {
			Mounts []struct {
				Source string `json:"Source"`
				Type   string `json:"Type"`
			} `json:"Mounts"`
		}
		if err := m.api.getJSON(ctx, "/containers/"+url.PathEscape(item.ID)+"/json", nil, &inspect); err != nil {
			return nil, err
		}
		for _, mount := range inspect.Mounts {
			if (mount.Type == "bind" || mount.Type == "volume") && mount.Source != "" {
				out = append(out, HostPath{Name: name, Path: mount.Source})
			}
		}
	}
	return out, nil
}
