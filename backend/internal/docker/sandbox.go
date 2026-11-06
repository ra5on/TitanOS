package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The sandbox of the KI-Agent (PLAN 55 point 8, decision of the owner of
// 5.11.2026: the middle way between no code and a console): a container
// that lives for one call, without network, with a read-only system, as
// the account's uid, with only the account's own folder of the share
// KI-Agent as /work and limits for time, memory, CPU and processes. It is
// labelled and never shown or touched as an app (client.containers leaves
// it out).

// SandboxImage is pinned by digest, so a changed tag never runs unseen.
const SandboxImage = "python:3.13-alpine@sha256:1e1f6334529d1fb26a2ef619d7cffb30a7c7d7cc1cc7338ebee283a58d1aeaca"

// sandboxLabel marks the containers of the sandbox.
const sandboxLabel = "titanos.sandbox"

// SandboxRequest is one run.
type SandboxRequest struct {
	// Language is "python" or "sh".
	Language string
	Code     string
	// Work is the host folder mounted as /work (read and write).
	Work     string
	UID, GID int
	Timeout  time.Duration
	MemoryMB int
	// User is the account, for the label.
	User string
}

// SandboxResult is what the run printed.
type SandboxResult struct {
	Output   string
	ExitCode int
	TimedOut bool
	Took     time.Duration
}

// sandboxOutput is how much of the output is kept.
const sandboxOutput = 64 << 10

// Sandbox runs code in a throw-away container.
func (m *Manager) Sandbox(ctx context.Context, req SandboxRequest) (SandboxResult, error) {
	if !m.current().Enabled {
		return SandboxResult{}, errors.New("Docker is switched off")
	}
	var cmd []string
	switch req.Language {
	case "python", "":
		cmd = []string{"python3", "-I", "-c", req.Code}
	case "sh":
		cmd = []string{"sh", "-c", req.Code}
	default:
		return SandboxResult{}, errors.New("language: python or sh")
	}
	if req.Work == "" || req.UID <= 0 {
		return SandboxResult{}, errors.New("sandbox without folder or account")
	}
	if req.Timeout <= 0 {
		req.Timeout = time.Minute
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 512
	}
	m.cleanSandboxes(ctx)
	if err := m.api.ensureImage(ctx, SandboxImage); err != nil {
		return SandboxResult{}, fmt.Errorf("sandbox image: %w", err)
	}
	mem := int64(req.MemoryMB) << 20
	body := map[string]any{
		"Image":           SandboxImage,
		"Cmd":             cmd,
		"User":            fmt.Sprintf("%d:%d", req.UID, req.GID),
		"WorkingDir":      "/work",
		"Env":             []string{"HOME=/tmp", "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"},
		"Labels":          map[string]string{sandboxLabel: "1", "titanos.user": req.User},
		"NetworkDisabled": true,
		"HostConfig": map[string]any{
			"NetworkMode":    "none",
			"ReadonlyRootfs": true,
			"Tmpfs":          map[string]string{"/tmp": "rw,nosuid,nodev,size=64m,mode=1777"},
			"Binds":          []string{req.Work + ":/work:rw"},
			"Memory":         mem,
			"MemorySwap":     mem,
			"NanoCpus":       int64(1e9),
			"PidsLimit":      128,
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges"},
			"Privileged":     false,
			"IpcMode":        "private",
			"LogConfig":      map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "1m"}},
		},
	}
	var created struct{ ID string `json:"Id"` }
	if err := m.api.sendJSON(ctx, http.MethodPost, "/containers/create", body, &created); err != nil {
		return SandboxResult{}, err
	}
	// Whatever happens, the container goes; the run's context may be gone.
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = m.api.do(dctx, http.MethodDelete, "/containers/"+created.ID, url.Values{"force": {"true"}})
	}()
	start := time.Now()
	if err := m.api.post(ctx, "/containers/"+created.ID+"/start", nil); err != nil {
		return SandboxResult{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	res := SandboxResult{ExitCode: -1}
	var wait struct{ StatusCode int }
	err := m.api.sendJSON(wctx, http.MethodPost, "/containers/"+created.ID+"/wait", map[string]any{}, &wait)
	res.Took = time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if wctx.Err() == nil {
			return res, err
		}
		res.TimedOut = true
		kctx, kcancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = m.api.post(kctx, "/containers/"+created.ID+"/kill", nil)
		kcancel()
	} else {
		res.ExitCode = wait.StatusCode
	}
	lctx, lcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer lcancel()
	out, _ := m.api.output(lctx, created.ID)
	if len(out) > sandboxOutput {
		out = out[:sandboxOutput] + "\n[… cut]"
	}
	res.Output = out
	return res, nil
}

// cleanSandboxes removes sandboxes a crash of titand left behind.
func (m *Manager) cleanSandboxes(ctx context.Context) {
	var list []apiContainer
	filters := `{"label":["` + sandboxLabel + `"],"status":["exited","created","dead"]}`
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}, "filters": {filters}}, &list); err != nil {
		return
	}
	for _, c := range list {
		_, _ = m.api.do(ctx, http.MethodDelete, "/containers/"+c.ID, url.Values{"force": {"true"}})
	}
}

// ensureImage pulls an image that is not there yet.
func (c *client) ensureImage(ctx context.Context, ref string) error {
	var img struct{ ID string `json:"Id"` }
	if err := c.getJSON(ctx, "/images/"+url.PathEscape(ref)+"/json", nil, &img); err == nil {
		return nil
	}
	name, digest, _ := strings.Cut(ref, "@")
	// The tag field takes a digest too: python + sha256:….
	q := url.Values{"fromImage": {strings.Split(name, ":")[0]}, "tag": {digest}}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	res, err := c.do(pctx, http.MethodPost, "/images/create", q)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// The pull streams its progress; it is done when the stream ends. An
	// error comes as a line with "error".
	data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if i := strings.LastIndex(string(data), `"error"`); i >= 0 {
		return errors.New(strings.TrimSpace(clipText(string(data[i:]), 300)))
	}
	return nil
}

// output is all a finished container printed, stdout and stderr.
func (c *client) output(ctx context.Context, id string) (string, error) {
	res, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/logs", url.Values{"stdout": {"true"}, "stderr": {"true"}, "tail": {"all"}})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return demux(bufio.NewReader(io.LimitReader(res.Body, sandboxOutput+4096)))
}

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
