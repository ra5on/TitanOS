package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The NVR (PLAN 79-7, package nvr, docs/NVR.md) runs Frigate in one
// container that TitanOS manages and never shows as an app. These calls
// are the only ones the NVR makes on the engine. Every call that changes a
// container first checks the label NVRLabel and the fixed name, so the NVR
// never stops, removes or starts anything it did not create. The container
// is no app: client.containers leaves it out.
//
// Like the calls of the remote view they work without the manager's
// settings; they fail like any engine call while Docker does not answer.

// NVRLabel marks the container and the network of the NVR.
const NVRLabel = "titanos.nvr"

// NVRName is the name of the NVR's container and of its network.
const NVRName = "titanos-nvr"

// NVRSpecLabel holds the hash of what the container was created from; a
// container with another hash is replaced.
const NVRSpecLabel = NVRLabel + ".spec"

// ErrNotNVR refuses a container that is not the NVR's own.
var ErrNotNVR = errors.New("docker: not the container of the NVR")

// NVRStopSeconds is how long Frigate gets to end (its documentation asks
// for 30 s: it closes the database and the recordings being written).
const NVRStopSeconds = 30

// NVRPort is a published port of the NVR.
type NVRPort struct {
	Port  int
	Proto string // "tcp" or "udp"
}

// NVRSpec is the container of the NVR.
type NVRSpec struct {
	// Image is the image ID (sha256:…), never a tag that could change.
	Image string
	// Config and Media are host folders mounted as /config and
	// /media/frigate.
	Config, Media string
	// ShmBytes is the size of /dev/shm, CacheBytes of the tmpfs /tmp/cache.
	ShmBytes, CacheBytes int64
	// Devices are host device nodes passed in unchanged.
	Devices []string
	// USBBus passes the whole USB bus (a Coral changes its device number
	// when its firmware is loaded, so one node is not enough).
	USBBus bool
	Ports  []NVRPort
	// Hash goes into NVRSpecLabel.
	Hash string
}

func ownNVR(labels map[string]string, name string) bool {
	return labels[NVRLabel] == "1" && strings.TrimPrefix(name, "/") == NVRName
}

// nvrCreateBody is the request of POST /containers/create for a spec.
func nvrCreateBody(spec NVRSpec) map[string]any {
	devices := []map[string]string{}
	for _, d := range spec.Devices {
		devices = append(devices, map[string]string{"PathOnHost": d, "PathInContainer": d, "CgroupPermissions": "rw"})
	}
	binds := []string{spec.Config + ":/config:rw", spec.Media + ":/media/frigate:rw", "/etc/localtime:/etc/localtime:ro"}
	host := map[string]any{
		"NetworkMode": NVRName,
		"Binds":       binds,
		"Tmpfs":       map[string]string{"/tmp/cache": "rw,nosuid,nodev,size=" + strconv.FormatInt(spec.CacheBytes, 10)},
		"ShmSize":     spec.ShmBytes,
		"Privileged":  false,
		"SecurityOpt": []string{"no-new-privileges"},
		"IpcMode":     "private",
		"Devices":     devices,
		// titand restarts Frigate itself (nvr/watch.go): it counts the
		// restarts and moves the detection to another device when Frigate
		// keeps ending. Docker's own policy would hide that loop.
		"RestartPolicy": map[string]any{"Name": "no"},
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "5m", "max-file": "2"}},
	}
	if spec.USBBus {
		host["Binds"] = append(binds, "/dev/bus/usb:/dev/bus/usb")
		host["DeviceCgroupRules"] = []string{"c 189:* rmw"}
	}
	exposed := map[string]any{}
	bindings := map[string]any{}
	for _, p := range spec.Ports {
		key := fmt.Sprintf("%d/%s", p.Port, p.Proto)
		exposed[key] = map[string]any{}
		bindings[key] = []map[string]string{{"HostPort": strconv.Itoa(p.Port)}}
	}
	host["PortBindings"] = bindings
	return map[string]any{
		"Image":            spec.Image,
		"Labels":           map[string]string{NVRLabel: "1", NVRSpecLabel: spec.Hash},
		"ExposedPorts":     exposed,
		"StopTimeout":      NVRStopSeconds,
		"NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{NVRName: map[string]any{}}},
		"HostConfig":       host,
	}
}

// NVRState is the container of the NVR as the engine sees it.
type NVRState struct {
	// Exists is false while there is no container.
	Exists  bool
	ID      string
	Running bool
	// ExitCode and FinishedAt are of the last end, StartedAt of the last start.
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	// Address is the IPv4 address on the NVR's network ("" while stopped).
	Address string
	// Hash is NVRSpecLabel, Image the image ID it was created from.
	Hash, Image string
}

type nvrInspect struct {
	ID    string `json:"Id"`
	Name  string
	Image string
	State struct {
		Running    bool
		ExitCode   int
		StartedAt  time.Time
		FinishedAt time.Time
	}
	Config struct {
		Labels map[string]string
	}
	NetworkSettings struct {
		Networks map[string]struct{ IPAddress string }
	}
}

// NVRInspect reports the NVR's container. A container of that name that is
// not the NVR's own is an error, never touched.
func (m *Manager) NVRInspect(ctx context.Context) (NVRState, error) {
	var info nvrInspect
	err := m.api.getJSON(ctx, "/containers/"+NVRName+"/json", nil, &info)
	if notFound(err) {
		return NVRState{}, nil
	}
	if err != nil {
		return NVRState{}, err
	}
	if !ownNVR(info.Config.Labels, info.Name) {
		return NVRState{}, ErrNotNVR
	}
	return NVRState{Exists: true, ID: info.ID, Running: info.State.Running, ExitCode: info.State.ExitCode,
		StartedAt: info.State.StartedAt, FinishedAt: info.State.FinishedAt,
		Address: info.NetworkSettings.Networks[NVRName].IPAddress, Hash: info.Config.Labels[NVRSpecLabel], Image: info.Image}, nil
}

// NVRCreate creates (not starts) the container.
func (m *Manager) NVRCreate(ctx context.Context, spec NVRSpec) error {
	if !strings.HasPrefix(spec.Image, "sha256:") || spec.Config == "" || spec.Media == "" {
		return errors.New("docker: incomplete NVR container")
	}
	m.noteStart(NVRName)
	return m.api.sendJSON(ctx, http.MethodPost, "/containers/create?name="+NVRName, nvrCreateBody(spec), nil)
}

// NVRStart starts the container.
func (m *Manager) NVRStart(ctx context.Context) error {
	if _, err := m.nvrOwn(ctx); err != nil {
		return err
	}
	return m.api.post(ctx, "/containers/"+NVRName+"/start", nil)
}

// NVRRestart stops the container (NVRStopSeconds) and starts it again.
// ctx must allow for the stop time.
func (m *Manager) NVRRestart(ctx context.Context) error {
	if _, err := m.nvrOwn(ctx); err != nil {
		return err
	}
	return m.api.post(ctx, "/containers/"+NVRName+"/restart", url.Values{"t": {strconv.Itoa(NVRStopSeconds)}})
}

func (m *Manager) nvrOwn(ctx context.Context) (NVRState, error) {
	st, err := m.NVRInspect(ctx)
	if err != nil {
		return st, err
	}
	if !st.Exists {
		return st, ErrNotFound
	}
	return st, nil
}

// NVRRemove stops the container (NVRStopSeconds) and removes it; one that
// is gone counts as removed. The data folders stay. ctx must allow for the
// stop time.
func (m *Manager) NVRRemove(ctx context.Context) error {
	st, err := m.NVRInspect(ctx)
	if err != nil || !st.Exists {
		return err
	}
	// A container that is not running answers 304, which is no error.
	if err := m.api.post(ctx, "/containers/"+st.ID+"/stop", url.Values{"t": {strconv.Itoa(NVRStopSeconds)}}); err != nil && !notFound(err) {
		return err
	}
	res, err := m.api.do(ctx, http.MethodDelete, "/containers/"+st.ID, url.Values{"force": {"true"}})
	if res != nil {
		res.Body.Close()
	}
	if notFound(err) {
		return nil
	}
	return err
}

// NVRLogs are the last lines the container wrote (Frigate's services),
// only those after since if it is set.
func (m *Manager) NVRLogs(ctx context.Context, tail int, since time.Time) (string, error) {
	st, err := m.nvrOwn(ctx)
	if err != nil {
		return "", err
	}
	query := url.Values{"stdout": {"true"}, "stderr": {"true"}, "tail": {strconv.Itoa(tail)}}
	if !since.IsZero() {
		query.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	res, err := m.api.do(ctx, http.MethodGet, "/containers/"+st.ID+"/logs", query)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return demux(bufio.NewReader(io.LimitReader(res.Body, 2<<20)))
}

// NVRNetwork makes sure the NVR's bridge network exists. It is not
// internal: Frigate reaches the cameras in the home network through it. A
// network of that name that is not the NVR's own is refused, never changed.
func (m *Manager) NVRNetwork(ctx context.Context) error {
	var net struct {
		Name   string
		Labels map[string]string
	}
	err := m.api.getJSON(ctx, "/networks/"+NVRName, nil, &net)
	if err == nil {
		if !ownNVR(net.Labels, net.Name) {
			return fmt.Errorf("docker: network %s exists and is not the network of the NVR", NVRName)
		}
		return nil
	}
	if !notFound(err) {
		return err
	}
	body := map[string]any{"Name": NVRName, "Driver": "bridge", "CheckDuplicate": true, "Labels": map[string]string{NVRLabel: "1"}}
	return m.api.sendJSON(ctx, http.MethodPost, "/networks/create", body, nil)
}

// NVRImageID returns the ID of an image, or "" if there is none.
func (m *Manager) NVRImageID(ctx context.Context, ref string) (string, error) {
	return m.ViewerImageID(ctx, ref)
}

// NVRPull pulls repository@digest and reports the bytes downloaded so far
// and their total (the sum over the layers the registry has named yet; it
// grows while the pull starts). It ends when the engine has the image.
func (m *Manager) NVRPull(ctx context.Context, repository, digest string, progress func(done, total int64)) error {
	// The tag field takes a digest too (see ensureImage).
	res, err := m.api.do(ctx, http.MethodPost, "/images/create", url.Values{"fromImage": {repository}, "tag": {digest}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return readPull(res.Body, progress)
}

// readPull follows the JSON lines of a pull: one per layer and step, with
// the bytes of that layer; an error comes as a line with "error".
func readPull(body io.Reader, progress func(done, total int64)) error {
	type layer struct{ current, total int64 }
	layers := map[string]layer{}
	decoder := json.NewDecoder(body)
	for {
		var line struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := decoder.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if line.Error != "" {
			return errors.New("docker: " + clipText(line.Error, 300))
		}
		if line.ID == "" || progress == nil {
			continue
		}
		l := layers[line.ID]
		switch {
		case line.Status == "Downloading" && line.ProgressDetail.Total > 0:
			l = layer{line.ProgressDetail.Current, line.ProgressDetail.Total}
		case line.Status == "Download complete" || line.Status == "Pull complete" || line.Status == "Already exists":
			// Extracting counts its own bytes; the download of the layer is done.
			l.current = l.total
		default:
			continue
		}
		layers[line.ID] = l
		var done, total int64
		for _, each := range layers {
			done += each.current
			total += each.total
		}
		progress(done, total)
	}
}

// EngineReady tells whether Docker is switched on and its engine was
// started by this titand (or found running).
func (m *Manager) EngineReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.Disabled && m.settings.Enabled && m.applied != ""
}

// NVRPruneImages removes older images of the NVR: images of repository
// that were pulled by digest only (no tag: what a person pulled or an app
// uses keeps its tag and stays) and are not keep. Never by force: Docker
// refuses an image a container still uses. It returns what was removed.
func (m *Manager) NVRPruneImages(ctx context.Context, repository, keep string) ([]string, error) {
	var list []struct {
		ID          string `json:"Id"`
		RepoTags    []string
		RepoDigests []string
	}
	filters, err := json.Marshal(map[string][]string{"reference": {repository}})
	if err != nil {
		return nil, err
	}
	if err := m.api.getJSON(ctx, "/images/json", url.Values{"filters": {string(filters)}}, &list); err != nil {
		return nil, err
	}
	var removed []string
	for _, image := range list {
		if image.ID == keep || !prunable(repository, image.RepoTags, image.RepoDigests) {
			continue
		}
		res, err := m.api.do(ctx, http.MethodDelete, "/images/"+url.PathEscape(image.ID), nil)
		if res != nil {
			res.Body.Close() // the answer lists what was removed; left open, it keeps a connection to the engine
		}
		if err == nil {
			removed = append(removed, image.ID)
		}
	}
	return removed, nil
}

// prunable tells whether an image is one the NVR pulled: known only by
// digests of repository, under no tag.
func prunable(repository string, tags, digests []string) bool {
	for _, tag := range tags {
		if tag != "" && tag != "<none>:<none>" {
			return false
		}
	}
	if len(digests) == 0 {
		return false
	}
	for _, digest := range digests {
		if !strings.HasPrefix(digest, repository+"@sha256:") {
			return false
		}
	}
	return true
}
