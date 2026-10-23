package docker

import (
	"context"
	"errors"
	"net/url"
)

type BackupLease struct {
	manager *Manager
	closed  bool
}

func (m *Manager) BackupLease(ctx context.Context) (*BackupLease, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	if !m.hardwareMu.TryLock() {
		return nil, errors.New("Docker is changing containers")
	}
	m.mu.Lock()
	busy := false
	for _, job := range m.jobs {
		if job.State == "running" {
			busy = true
		}
	}
	m.mu.Unlock()
	if busy {
		m.hardwareMu.Unlock()
		return nil, errors.New("Docker app work is running")
	}
	return &BackupLease{manager: m}, nil
}
func (l *BackupLease) Close() {
	if !l.closed {
		l.closed = true
		l.manager.hardwareMu.Unlock()
	}
}
func (l *BackupLease) Containers(ctx context.Context) ([]Container, error) {
	items, err := l.manager.api.containers(ctx)
	if err != nil {
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
