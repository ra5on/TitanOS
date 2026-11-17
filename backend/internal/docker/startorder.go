package docker

// PLAN 73: Docker apps in the owner's start order (Freigaben › Dienste ›
// Start nach dem Hochfahren). Docker itself starts every container with a
// restart policy at once when the engine starts. So that titand can start
// the listed apps one after the other, their containers get the policy "no"
// just before the NAS goes down (HoldApps), titand starts them in order at
// boot (StartApp) and then gives every container its policy back
// (RestorePolicies). While running nothing differs: a crashed container is
// restarted as before. After a power cut (no orderly shutdown) the engine
// starts them all at once, as before.
//
// N-102-22 (owner 17.11.2026): at boot only the containers start that ran
// when the NAS went down and have a restart policy. One stopped by hand and
// a one-shot init container stay stopped. Docker itself keeps a stopped
// "unless-stopped" container stopped across an engine restart, starts an
// "always" one again even after a manual stop, and restarts "on-failure"
// only after an error exit, not after an engine restart (Docker docs,
// "Start containers automatically"). The order is stricter for "always"
// and starts a running "on-failure" container again.

import (
	"context"
	"errors"
	"net/url"
)

// HeldPolicy is the restart policy a container had before HoldApps.
// Running: it ran (or was restarting) then, so the order starts it again.
// Missing in a note of an older titand: such a container is not started.
type HeldPolicy struct {
	Name       string `json:"name"`
	MaxRetries int    `json:"maxRetries,omitempty"`
	Running    bool   `json:"running,omitempty"`
}

type apiRestart struct {
	HostConfig struct {
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

func (m *Manager) appContainers(ctx context.Context, app string) ([]apiContainer, error) {
	list, err := m.api.containers(ctx)
	if err != nil {
		return nil, err
	}
	var out []apiContainer
	for _, c := range list {
		if c.Labels["com.docker.compose.project"] == app {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *Manager) setRestart(ctx context.Context, id string, p HeldPolicy) error {
	body := map[string]any{"RestartPolicy": map[string]any{"Name": p.Name, "MaximumRetryCount": p.MaxRetries}}
	return m.api.sendJSON(ctx, "POST", "/containers/"+url.PathEscape(id)+"/update", body, nil)
}

// HoldApps sets the restart policy of the apps' containers to "no" and
// returns what each had (by container ID) and whether it ran. Containers
// whose policy is "no" already are left out: there is nothing to give back.
// Stopped ones are held too, or the engine would start an "always" one by
// itself at boot.
func (m *Manager) HoldApps(ctx context.Context, apps []string) (map[string]HeldPolicy, error) {
	held := map[string]HeldPolicy{}
	if m.Disabled || len(apps) == 0 {
		return held, nil
	}
	var errs []error
	for _, app := range apps {
		containers, err := m.appContainers(ctx, app)
		if err != nil {
			return held, err
		}
		for _, c := range containers {
			var info apiRestart
			if err := m.api.getJSON(ctx, "/containers/"+url.PathEscape(c.ID)+"/json", nil, &info); err != nil {
				errs = append(errs, err)
				continue
			}
			policy := HeldPolicy{Name: info.HostConfig.RestartPolicy.Name, MaxRetries: info.HostConfig.RestartPolicy.MaximumRetryCount}
			if policy.Name == "" || policy.Name == "no" {
				continue
			}
			if err := m.setRestart(ctx, c.ID, HeldPolicy{Name: "no"}); err != nil {
				errs = append(errs, err)
				continue
			}
			policy.Running = c.State == "running" || c.State == "restarting"
			held[c.ID] = policy
		}
	}
	return held, errors.Join(errs...)
}

// StartApp starts the containers of one app that ran when the NAS went
// down (held with Running) and do not run now. Without a note nothing is
// started: after a power cut the engine started those with a policy itself.
func (m *Manager) StartApp(ctx context.Context, app string, held map[string]HeldPolicy) error {
	m.noteStart(app)
	containers, err := m.appContainers(ctx, app)
	if err != nil {
		return err
	}
	// The engine lists them in no useful order: a container that shares
	// the network of another (network_mode: service:vpn) cannot start
	// before that one runs. What fails in the first round is tried once
	// more after the others are up.
	// Like any container start (Action): not on a GPU a VM holds, not
	// while an app backup holds Docker.
	start := func(id string) error {
		m.hardwareMu.Lock()
		defer m.hardwareMu.Unlock()
		if m.backupActive() {
			return errBackupRunning
		}
		if err := m.canStartContainer(ctx, url.PathEscape(id)); err != nil {
			return err
		}
		return m.api.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil)
	}
	var again []string
	for _, c := range containers {
		if c.State == "running" || !held[c.ID].Running {
			continue
		}
		if err := start(c.ID); err != nil {
			again = append(again, c.ID)
		}
	}
	var errs []error
	for _, id := range again {
		if err := start(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RestorePolicies gives the containers their restart policies back. A
// container that is gone (the app was removed) is skipped.
func (m *Manager) RestorePolicies(ctx context.Context, held map[string]HeldPolicy) error {
	var errs []error
	for id, p := range held {
		if err := m.setRestart(ctx, id, p); err != nil {
			var api *apiError
			if errors.As(err, &api) && api.Status == 404 {
				continue
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
