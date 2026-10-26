package docker

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"syscall"
)

type BackupLease struct {
	manager *Manager
	closed  bool
}

// BackupLease keeps Docker from changing containers until Close: app jobs,
// container actions, engine restarts and automatic GPU switches are refused
// meanwhile. It does not hold hardwareMu, so VM starts that need no container
// change and other Docker calls keep working during a long backup.
func (m *Manager) BackupLease(ctx context.Context) (*BackupLease, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	// hardwareMu is held only briefly by lifecycle calls; waiting for it lets
	// one that is underway finish before the backup takes over.
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	if m.composing > 0 {
		return nil, errors.New("Docker is changing containers")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.State == "running" {
			return nil, errors.New("Docker app work is running")
		}
	}
	if m.backups > 0 {
		return nil, errors.New("another app backup holds Docker")
	}
	m.backups++
	return &BackupLease{manager: m}, nil
}
func (l *BackupLease) Close() {
	if !l.closed {
		l.closed = true
		l.manager.mu.Lock()
		l.manager.backups--
		l.manager.mu.Unlock()
	}
}
func (l *BackupLease) Containers(ctx context.Context) ([]Container, error) {
	items, err := l.manager.api.containers(ctx)
	if err != nil {
		if !l.manager.current().Enabled && (errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)) {
			return []Container{}, nil
		}
		return nil, err
	}
	out := []Container{}
	for _, item := range items {
		name := ""
		if len(item.Names) > 0 {
			name = item.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		out = append(out, Container{ID: item.ID, Name: name, State: item.State})
	}
	return out, nil
}
func (l *BackupLease) Stop(ctx context.Context, id string) error {
	if err := l.manager.api.post(ctx, "/containers/"+url.PathEscape(id)+"/stop", url.Values{"t": []string{"60"}}); err != nil {
		return err
	}
	var inspected struct {
		State struct {
			Running   bool `json:"Running"`
			OOMKilled bool `json:"OOMKilled"`
			ExitCode  int  `json:"ExitCode"`
		} `json:"State"`
	}
	if err := l.manager.api.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &inspected); err != nil {
		return err
	}
	if inspected.State.Running || inspected.State.OOMKilled || inspected.State.ExitCode == 137 {
		return errors.New("container did not stop cleanly; refusing a consistent backup")
	}
	return nil
}
func (l *BackupLease) Start(ctx context.Context, id string) error {
	return l.manager.api.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil)
}

type BackupMount struct {
	Source   string `json:"source"`
	Writable bool   `json:"writable"`
}

func (l *BackupLease) Mounts(ctx context.Context, id string) ([]BackupMount, error) {
	var inspect struct {
		Mounts []struct {
			Source string `json:"Source"`
			RW     bool   `json:"RW"`
			Type   string `json:"Type"`
		} `json:"Mounts"`
	}
	if err := l.manager.api.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &inspect); err != nil {
		return nil, err
	}
	out := []BackupMount{}
	for _, m := range inspect.Mounts {
		if m.Type == "bind" || m.Type == "volume" {
			out = append(out, BackupMount{m.Source, m.RW})
		}
	}
	return out, nil
}

// PostgresUser returns the superuser role the official postgres image created:
// POSTGRES_USER from the container environment, "postgres" if unset.
func (l *BackupLease) PostgresUser(ctx context.Context, id string) (string, error) {
	var inspect struct {
		Config struct {
			Env []string `json:"Env"`
		} `json:"Config"`
	}
	if err := l.manager.api.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &inspect); err != nil {
		return "", err
	}
	return postgresUser(inspect.Config.Env), nil
}

func postgresUser(env []string) string {
	user := ""
	for _, value := range env {
		if v, ok := strings.CutPrefix(value, "POSTGRES_USER="); ok {
			user = v
		}
	}
	if user == "" {
		return "postgres"
	}
	return user
}
