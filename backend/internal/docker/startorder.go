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

import (
	"context"
	"errors"
	"net/url"
)

// HeldPolicy is the restart policy a container had before HoldApps.
type HeldPolicy struct {
	Name       string `json:"name"`
	MaxRetries int    `json:"maxRetries,omitempty"`
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
// returns what each had (by container ID). Containers whose policy is "no"
// already are left out: there is nothing to give back.
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
			held[c.ID] = policy
		}
	}
	return held, errors.Join(errs...)
}

// StartApp starts the containers of one app that do not run.
func (m *Manager) StartApp(ctx context.Context, app string) error {
	containers, err := m.appContainers(ctx, app)
	if err != nil {
		return err
	}
	// The engine lists them in no useful order: a container that shares
	// the network of another (network_mode: service:vpn) cannot start
	// before that one runs. What fails in the first round is tried once
	// more after the others are up.
	start := func(id string) error {
		return m.api.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil)
	}
	var again []string
	for _, c := range containers {
		if c.State == "running" {
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
