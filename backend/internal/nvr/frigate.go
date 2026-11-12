package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// frigatePort is Frigate's port behind its web server's check of the
// proxy secret (proxy.auth_secret): titand reaches it at the container's
// address on the NVR's own network and shows the secret on every
// request. Port 5000, which never asks, listens on the container's
// loopback only (render.go). Neither is published.
const frigatePort = 8971

// proxySecretHeader carries the secret; Frigate's nginx hands it to the
// check of every request (proxy_trusted_headers.conf).
const proxySecretHeader = "X-Proxy-Secret"

// secretTransport puts the secret on every request to Frigate.
type secretTransport struct {
	base   http.RoundTripper
	secret func() string
}

func (t secretTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(proxySecretHeader, t.secret())
	return t.base.RoundTrip(req)
}

// ErrNotRunning: Frigate does not run (or has no address yet).
var ErrNotRunning = errors.New("nvr: the NVR does not run")

// frigate is the client for Frigate's API.
type frigate struct {
	http *http.Client
	// stream is for answers the caller passes on: no overall timeout.
	stream *http.Client
}

func newFrigate(secret func() string) *frigate {
	// Frigate never sends titand elsewhere.
	stay := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := secretTransport{base: http.DefaultTransport.(*http.Transport).Clone(), secret: secret}
	return &frigate{
		http:   &http.Client{Timeout: 15 * time.Second, CheckRedirect: stay, Transport: transport},
		stream: &http.Client{CheckRedirect: stay, Transport: transport},
	}
}

func (f *frigate) get(ctx context.Context, base, path string, query url.Values, limit int64) ([]byte, http.Header, error) {
	if base == "" {
		return nil, nil, ErrNotRunning
	}
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := f.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("nvr: Frigate answered %d for %s", res.StatusCode, path)
	}
	return data, res.Header, nil
}

// version is GET /api/version: the sign that Frigate's API is up.
func (f *frigate) version(ctx context.Context, base string) (string, error) {
	data, _, err := f.get(ctx, base, "/api/version", nil, 256)
	return strings.TrimSpace(string(data)), err
}

// runningConfig is the part of GET /api/config that is used: whether
// Frigate refused config.yml (it then starts with an empty configuration,
// "safe mode", and answers as if all were well) and the cameras it runs.
type runningConfig struct {
	SafeMode bool                       `json:"safe_mode"`
	Cameras  map[string]json.RawMessage `json:"cameras"`
}

// cameraIDs are the cameras of the configuration Frigate runs, sorted.
func (c runningConfig) cameraIDs() []string {
	ids := []string{}
	for id := range c.Cameras {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (f *frigate) runningConfig(ctx context.Context, base string) (runningConfig, error) {
	var config runningConfig
	data, _, err := f.get(ctx, base, "/api/config", nil, 8<<20)
	if err != nil {
		return config, err
	}
	return config, json.Unmarshal(data, &config)
}

// frigateStats is the part of GET /api/stats that is used.
type frigateStats struct {
	Detectors map[string]struct {
		InferenceSpeed float64 `json:"inference_speed"`
		PID            *int    `json:"pid"`
	} `json:"detectors"`
	Cameras map[string]struct {
		CameraFPS    float64 `json:"camera_fps"`
		ProcessFPS   float64 `json:"process_fps"`
		SkippedFPS   float64 `json:"skipped_fps"`
		DetectionFPS float64 `json:"detection_fps"`
	} `json:"cameras"`
	GPUUsages map[string]json.RawMessage `json:"gpu_usages"`
	NPUUsages map[string]json.RawMessage `json:"npu_usages"`
	Service   struct {
		Uptime  int64 `json:"uptime"`
		Storage map[string]struct {
			Total float64 `json:"total"`
			Used  float64 `json:"used"`
			Free  float64 `json:"free"`
		} `json:"storage"`
	} `json:"service"`
}

func (f *frigate) stats(ctx context.Context, base string) (frigateStats, error) {
	var stats frigateStats
	data, _, err := f.get(ctx, base, "/api/stats", nil, 4<<20)
	if err != nil {
		return stats, err
	}
	return stats, json.Unmarshal(data, &stats)
}

// logs are the last lines of Frigate's own log (GET /api/logs/frigate),
// passwords taken out.
func (f *frigate) logs(ctx context.Context, base string, last int) ([]string, error) {
	data, _, err := f.get(ctx, base, "/api/logs/frigate", nil, 8<<20)
	if err != nil {
		return nil, err
	}
	var body struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	lines := body.Lines
	if len(lines) > last {
		lines = lines[len(lines)-last:]
	}
	for i := range lines {
		lines[i] = Redact(lines[i])
	}
	return lines, nil
}

// recordingsUsage is GET /api/recordings/storage: bytes per camera.
func (f *frigate) recordingsUsage(ctx context.Context, base string) (uint64, error) {
	data, _, err := f.get(ctx, base, "/api/recordings/storage", nil, 1<<20)
	if err != nil {
		return 0, err
	}
	// {camera: {bandwidth, usage (MB), usage_percent}}
	var usage map[string]struct {
		Usage float64 `json:"usage"`
	}
	if err := json.Unmarshal(data, &usage); err != nil {
		return 0, err
	}
	var total float64
	for _, camera := range usage {
		total += camera.Usage
	}
	return uint64(total * (1 << 20)), nil
}

// snapshot is GET /api/<camera>/latest.jpg.
func (f *frigate) snapshot(ctx context.Context, base, camera string, height int) ([]byte, error) {
	query := url.Values{}
	if height > 0 {
		query.Set("height", fmt.Sprint(height))
	}
	data, header, err := f.get(ctx, base, "/api/"+url.PathEscape(camera)+"/latest.jpg", query, 16<<20)
	if err != nil {
		return nil, err
	}
	if kind := header.Get("Content-Type"); !strings.HasPrefix(kind, "image/jpeg") {
		return nil, fmt.Errorf("nvr: Frigate sent %q instead of a picture", kind)
	}
	return data, nil
}

// frigateError is an answer of Frigate that is no success.
type frigateError struct {
	Status  int
	Message string
}

func (e *frigateError) Error() string {
	return fmt.Sprintf("nvr: Frigate answered %d: %s", e.Status, e.Message)
}

// frigateStatus is the status of a failed call, 0 if err is something else.
func frigateStatus(err error) int {
	var fe *frigateError
	if errors.As(err, &fe) {
		return fe.Status
	}
	return 0
}

// call sends a request with an optional JSON body and decodes a JSON
// answer into out (nil: the answer is dropped). An answer of 400 and more
// is a *frigateError with Frigate's message.
func (f *frigate) call(ctx context.Context, method, base, path string, query url.Values, body, out any) error {
	if base == "" {
		return ErrNotRunning
	}
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		var problem struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &problem) // an answer that is no JSON leaves the message empty
		return &frigateError{Status: res.StatusCode, Message: clip(Redact(problem.Message), 300)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// open starts a request whose answer the caller streams on (pictures,
// video, playlists). header carries Range and the conditional headers of
// the browser. The caller closes the body.
func (f *frigate) open(ctx context.Context, base, path, rawQuery string, header http.Header) (*http.Response, error) {
	if base == "" {
		return nil, ErrNotRunning
	}
	target := base + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	// No overall timeout: a video is as long as it is. The request ends
	// with ctx (the browser's request).
	return f.stream.Do(req)
}
