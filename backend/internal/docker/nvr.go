package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	// CDIDevices are CDI names (nvidia.com/gpu=<UUID>) of an NVIDIA GPU
	// that decodes the streams. Left out of the hash while empty, so that
	// a container made before the field is not made anew for it.
	CDIDevices []string `json:",omitempty"`
	// USBBus passes the whole USB bus (a Coral changes its device number
	// when its firmware is loaded, so one node is not enough).
	USBBus bool
	// PidsLimit bounds processes and threads; 0 is NVRPidsLimit.
	PidsLimit int64
	Ports     []NVRPort
	// Hash goes into NVRSpecLabel.
	Hash string
}

func ownNVR(labels map[string]string, name string) bool {
	return labels[NVRLabel] == "1" && strings.TrimPrefix(name, "/") == NVRName
}

// NVRPidsLimit bounds the processes and threads of the container when
// the spec names no limit (a runaway ffmpeg or a fork loop). Threads
// count; the NVR's own limit grows with the cameras (nvr.pidsLimit).
const NVRPidsLimit = 8192

// nvrMount is a bind mount. Unlike "Binds", the engine refuses a source
// that is not there instead of making an empty folder: a volume that went
// away between the check and the start never becomes a folder in RAM.
func nvrMount(source, target string, readOnly bool) map[string]any {
	return map[string]any{"Type": "bind", "Source": source, "Target": target, "ReadOnly": readOnly}
}

// nvrCreateBody is the request of POST /containers/create for a spec.
func nvrCreateBody(spec NVRSpec) map[string]any {
	devices := []map[string]string{}
	for _, d := range spec.Devices {
		devices = append(devices, map[string]string{"PathOnHost": d, "PathInContainer": d, "CgroupPermissions": "rw"})
	}
	mounts := []map[string]any{
		nvrMount(spec.Config, "/config", false),
		nvrMount(spec.Media, "/media/frigate", false),
		nvrMount("/etc/localtime", "/etc/localtime", true),
	}
	if spec.USBBus {
		mounts = append(mounts, nvrMount("/dev/bus/usb", "/dev/bus/usb", false))
	}
	pids := spec.PidsLimit
	if pids <= 0 {
		pids = NVRPidsLimit
	}
	host := map[string]any{
		"NetworkMode": NVRName,
		"Mounts":      mounts,
		"Tmpfs":       map[string]string{"/tmp/cache": "rw,nosuid,nodev,size=" + strconv.FormatInt(spec.CacheBytes, 10)},
		"ShmSize":     spec.ShmBytes,
		"Privileged":  false,
		"SecurityOpt": []string{"no-new-privileges"},
		// Nothing in Frigate opens raw sockets (ping, packet capture).
		"CapDrop":   []string{"NET_RAW"},
		"PidsLimit": pids,
		"IpcMode":   "private",
		"Devices":   devices,
		// titand restarts Frigate itself (nvr/watch.go): it counts the
		// restarts and moves the detection to another device when Frigate
		// keeps ending. Docker's own policy would hide that loop.
		"RestartPolicy": map[string]any{"Name": "no"},
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "5m", "max-file": "2"}},
	}
	if spec.USBBus {
		host["DeviceCgroupRules"] = []string{"c 189:* rmw"}
	}
	if len(spec.CDIDevices) > 0 {
		// As for Docker apps and the remote view: Docker's own CDI support
		// (features.cdi) with the descriptions the NVIDIA plugin writes.
		host["DeviceRequests"] = []map[string]any{{"Driver": "cdi", "DeviceIDs": spec.CDIDevices}}
	}
	exposed := map[string]any{}
	bindings := map[string]any{}
	for _, p := range spec.Ports {
		key := fmt.Sprintf("%d/%s", p.Port, p.Proto)
		exposed[key] = map[string]any{}
		// IPv4 only: without a host address Docker also listens on every
		// IPv6 address of the NAS, global ones included. Not the home
		// network addresses alone: one that changes (DHCP) would keep the
		// container from starting until it is made anew.
		bindings[key] = []map[string]string{{"HostIp": "0.0.0.0", "HostPort": strconv.Itoa(p.Port)}}
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

// cdiErrorRE matches the engine's answers about CDI devices: without
// Docker's CDI support `could not select device driver "cdi" with
// capabilities: []` (Docker 26.1.5 answers it at the start, after the
// create succeeded), with it `unresolvable CDI devices nvidia.com/gpu=…`,
// and an NVIDIA plugin whose descriptions went away `…nvidia.com/gpu=…: no
// such file or directory` (as for apps, Manager.Action).
// Only these forms: "cdi" alone also stands in paths (a volume named
// "cdi") and would mark a GPU broken for an error that has nothing to do
// with it.
var cdiErrorRE = regexp.MustCompile(`(?i)(device driver "cdi"|\bCDI devices?\b|nvidia\.com/gpu=)`)

// IsCDIError tells whether the engine refused a create or start because of
// a CDI device: a timeout or a full disk is no such refusal.
func IsCDIError(err error) bool {
	return err != nil && cdiErrorRE.MatchString(err.Error())
}

// ErrNVRNotRunning: the NVR's container is there but does not run.
var ErrNVRNotRunning = errors.New("docker: the container of the NVR does not run")

// NVRExec runs a command in the NVR's own running container – argv, no
// shell, no terminal, no input – and returns what it printed (stdout and
// stderr together, at most limit bytes) and its exit code (-1 while it
// still runs, e.g. when the output was cut). The output is the caller's to
// keep out of logs: what runs there may print addresses with passwords.
func (m *Manager) NVRExec(ctx context.Context, cmd []string, limit int) (string, int, error) {
	if len(cmd) == 0 || limit <= 0 {
		return "", 0, errors.New("docker: no command for the NVR")
	}
	st, err := m.nvrOwn(ctx)
	if err != nil {
		return "", 0, err
	}
	if !st.Running {
		return "", 0, ErrNVRNotRunning
	}
	var created struct {
		ID string `json:"Id"`
	}
	request := map[string]any{"AttachStdin": false, "AttachStdout": true, "AttachStderr": true, "Tty": false, "Cmd": cmd}
	if err := m.api.sendJSON(ctx, http.MethodPost, "/containers/"+st.ID+"/exec", request, &created); err != nil {
		return "", 0, err
	}
	if created.ID == "" {
		return "", 0, errors.New("docker: the engine named no exec")
	}
	// Without "Upgrade" the engine answers 200 and then the output as
	// frames of the multiplexed stream until the command ends and closes
	// the connection.
	res, err := m.api.stream(ctx, http.MethodPost, "/exec/"+url.PathEscape(created.ID)+"/start", map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return "", 0, err
	}
	// Each frame has an 8-byte header; a little more than the limit is read
	// so that short frames do not cut the output early.
	out, rerr := demux(bufio.NewReader(io.LimitReader(res.Body, int64(limit)+64<<10)))
	res.Body.Close()
	if rerr != nil {
		return "", 0, rerr
	}
	if len(out) > limit {
		out = out[:limit]
	}
	// The engine notes the exit code a moment after the stream ends.
	code := -1
	for range 10 {
		var inspect struct {
			Running  bool
			ExitCode int
		}
		if err := m.api.getJSON(ctx, "/exec/"+url.PathEscape(created.ID)+"/json", nil, &inspect); err != nil {
			return out, -1, err
		}
		if !inspect.Running {
			code = inspect.ExitCode
			break
		}
		select {
		case <-ctx.Done():
			return out, -1, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return out, code, nil
}

// stream sends a JSON body and returns the answer for the caller to read
// as it comes and to close; an error answer is an error.
func (c *client) stream(ctx context.Context, method, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker/v1.43"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		var problem struct{ Message string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&problem) // a value that cannot be read leaves the zero value, which the code handles
		return nil, &apiError{Status: res.StatusCode, Message: problem.Message}
	}
	return res, nil
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

// enginePIDFile is where dockerd notes its process (its default
// --pidfile; TitanOS runs it as docker.service without another).
const enginePIDFile = "/run/docker.pid"

// NVREngineStarted tells when Docker's engine (dockerd) started: a
// container that ended before that was stopped with the engine (Docker
// restarted by hand, after a crash of dockerd, or by titand for another
// data folder), not by a fault of its own. Read from /proc (RAM), never a
// disk.
func (m *Manager) NVREngineStarted(context.Context) (time.Time, error) {
	return processStarted("/proc", enginePIDFile, time.Now())
}

// userHZ is the unit of the start time in /proc/<pid>/stat (USER_HZ):
// 100 on every Linux TitanOS runs on (amd64).
const userHZ = 100

// processStarted is when the process named in pidFile started, from field
// 22 of <proc>/<pid>/stat (clock ticks since boot) and the time since boot
// (<proc>/uptime), as of now.
func processStarted(proc, pidFile string, now time.Time) (time.Time, error) {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return time.Time{}, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return time.Time{}, fmt.Errorf("docker: no process in %s", pidFile)
	}
	stat, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return time.Time{}, err
	}
	// "pid (comm) state …": comm may hold spaces and parentheses.
	at := strings.LastIndexByte(string(stat), ')')
	if at < 0 {
		return time.Time{}, errors.New("docker: unreadable process status")
	}
	fields := strings.Fields(string(stat[at+1:]))
	if len(fields) < 20 {
		return time.Time{}, errors.New("docker: short process status")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64) // field 22; fields[0] is field 3
	if err != nil {
		return time.Time{}, err
	}
	uptime, err := os.ReadFile(filepath.Join(proc, "uptime"))
	if err != nil {
		return time.Time{}, err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(uptime)), " ")
	up, err := strconv.ParseFloat(first, 64)
	if err != nil {
		return time.Time{}, err
	}
	boot := now.Add(-time.Duration(up * float64(time.Second)))
	return boot.Add(time.Duration(ticks) * time.Second / userHZ), nil
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
