package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// frigatePort is Frigate's port without a login. It is never published:
// titand reaches it at the container's address on the NVR's own network.
const frigatePort = 5000

// ErrNotRunning: Frigate does not run (or has no address yet).
var ErrNotRunning = errors.New("nvr: the NVR does not run")

// frigate is the client for Frigate's API.
type frigate struct {
	http *http.Client
}

func newFrigate() *frigate {
	return &frigate{http: &http.Client{
		Timeout: 15 * time.Second,
		// Frigate never sends titand elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
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

// safeMode tells whether Frigate refused config.yml: it then starts with
// an empty configuration ("safe mode") and answers as if all were well.
func (f *frigate) safeMode(ctx context.Context, base string) (bool, error) {
	data, _, err := f.get(ctx, base, "/api/config", nil, 8<<20)
	if err != nil {
		return false, err
	}
	var config struct {
		SafeMode bool `json:"safe_mode"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return false, err
	}
	return config.SafeMode, nil
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
