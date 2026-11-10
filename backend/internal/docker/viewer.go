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
	"slices"
	"strconv"
	"strings"
)

// The remote view (Fernansicht, PLAN 63, package viewer) runs a browser per
// account and app in a container of its own. These calls are the only ones
// the viewer makes on the engine. Every call that changes a container
// first checks the label ViewerLabel, so the viewer never stops, removes
// or starts anything it did not create. Viewer containers are no apps:
// client.containers leaves them out.
//
// They work without the manager's settings (Disabled, applied): the viewer
// decides on its own whether it runs (in development mode only with a local
// image), and they fail like any engine call while Docker does not answer.

// ViewerLabel marks the containers and the network of the remote view.
const ViewerLabel = "titanos.viewer"

// ErrNotViewer refuses a container that does not carry ViewerLabel.
var ErrNotViewer = errors.New("docker: not a container of the remote view")

// ViewerPrefix starts the names of viewer containers and networks. Only
// objects with ViewerLabel AND this prefix are the viewer's own.
const ViewerPrefix = "titanos-viewer-"

// viewerPrefix is ViewerPrefix; Manager.ViewerPrefix replaces it (tests
// that run next to real viewers).
func (m *Manager) viewerPrefix() string {
	if m.ViewerNamePrefix != "" {
		return m.ViewerNamePrefix
	}
	return ViewerPrefix
}

// ownViewer tells whether labels and name mark a viewer object of TitanOS.
func (m *Manager) ownViewer(labels map[string]string, name string) bool {
	return labels[ViewerLabel] == "1" && strings.HasPrefix(strings.TrimPrefix(name, "/"), m.viewerPrefix())
}

// isViewerContainer tells whether a container is part of the remote view
// (label OR name): the routes for apps refuse those.
func (m *Manager) isViewerContainer(ctx context.Context, id string) bool {
	info, err := m.inspectAny(ctx, id)
	if err != nil {
		return false
	}
	return info.Config.Labels[ViewerLabel] != "" || strings.HasPrefix(strings.TrimPrefix(info.Name, "/"), ViewerPrefix)
}

// ViewerSpec is one viewer container.
type ViewerSpec struct {
	Name string
	// Image is the image ID (sha256:…), never a tag that could change.
	Image string
	// User and App are the account and the app, kept as labels.
	User, App string
	Env       []string
	// Profile is the host folder mounted as /profile (read and write).
	Profile string
	// Network is the only network the container joins.
	Network string
	// Devices are host device nodes passed in unchanged (/dev/dri/renderD128).
	Devices []string
	// GroupAdd are the groups (numeric) that may open those devices.
	GroupAdd []string
	// CDIDevices are CDI names (nvidia.com/gpu=<UUID>) for NVIDIA.
	CDIDevices  []string
	MemoryBytes int64
	PidsLimit   int64
	ShmBytes    int64
	// NanoCPUs caps the CPU (1e9 = one CPU); CPUShares weighs it against
	// other containers (Docker's default is 1024).
	NanoCPUs  int64
	CPUShares int64
	// Policy is a host file (root-owned, 0444) mounted read-only as
	// Chromium's managed policy (PolicyPath, proto.PolicyPath).
	Policy string
}

// PolicyPath is the managed policy file of Chromium in the image; its
// folder is root's and holds nothing else (viewer/Dockerfile).
const PolicyPath = "/etc/chromium/policies/managed/titanos.json"

// viewerCreateBody is the request of POST /containers/create for a spec.
func viewerCreateBody(spec ViewerSpec) map[string]any {
	devices := []map[string]string{}
	for _, d := range spec.Devices {
		devices = append(devices, map[string]string{"PathOnHost": d, "PathInContainer": d, "CgroupPermissions": "rw"})
	}
	binds := []string{spec.Profile + ":/profile:rw"}
	if spec.Policy != "" {
		// Read-only and owned by root on the host: nothing in the
		// container can change or add a policy.
		binds = append(binds, spec.Policy+":"+PolicyPath+":ro")
	}
	host := map[string]any{
		"NetworkMode":    spec.Network,
		"ReadonlyRootfs": true,
		// The only writable places besides /profile; the agent needs no /run.
		"Tmpfs": map[string]string{
			"/tmp": "rw,nosuid,nodev,size=256m,mode=1777",
		},
		"Binds":       binds,
		"NanoCpus":    spec.NanoCPUs,
		"CpuShares":   spec.CPUShares,
		"Memory":      spec.MemoryBytes,
		"MemorySwap":  spec.MemoryBytes,
		"PidsLimit":   spec.PidsLimit,
		"ShmSize":     spec.ShmBytes,
		"CapDrop":     []string{"ALL"},
		"SecurityOpt": []string{"no-new-privileges"},
		"Privileged":  false,
		"IpcMode":     "private",
		"Devices":     devices,
		"GroupAdd":    spec.GroupAdd,
		// Viewers end with the view or titand; they never come back alone.
		"RestartPolicy": map[string]any{"Name": "no"},
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "1m", "max-file": "1"}},
	}
	if len(spec.CDIDevices) > 0 {
		host["DeviceRequests"] = []map[string]any{{"Driver": "cdi", "DeviceIDs": spec.CDIDevices}}
	}
	return map[string]any{
		"Image":  spec.Image,
		"User":   "1000:1000",
		"Env":    spec.Env,
		"Labels": map[string]string{ViewerLabel: "1", ViewerLabel + ".user": spec.User, ViewerLabel + ".app": spec.App},
		// Only the internal network: the viewer has no way out.
		"NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{spec.Network: map[string]any{}}},
		"HostConfig":       host,
	}
}

// ViewerContainer is a container of the remote view.
type ViewerContainer struct {
	ID, Name, User, App, State string
}

// Viewers lists the containers of the remote view, running or not.
func (m *Manager) Viewers(ctx context.Context) ([]ViewerContainer, error) {
	var list []apiContainer
	filters := `{"label":["` + ViewerLabel + `=1"]}`
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}, "filters": {filters}}, &list); err != nil {
		return nil, err
	}
	out := []ViewerContainer{}
	for _, c := range list {
		if len(c.Names) == 0 || !m.ownViewer(c.Labels, c.Names[0]) {
			continue
		}
		v := ViewerContainer{ID: c.ID, State: c.State, User: c.Labels[ViewerLabel+".user"], App: c.Labels[ViewerLabel+".app"]}
		if len(c.Names) > 0 {
			v.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, v)
	}
	return out, nil
}

// ViewerCreate creates (not starts) a viewer container.
func (m *Manager) ViewerCreate(ctx context.Context, spec ViewerSpec) (string, error) {
	if !strings.HasPrefix(spec.Name, m.viewerPrefix()) || !strings.HasPrefix(spec.Network, m.viewerPrefix()) || !strings.HasPrefix(spec.Image, "sha256:") || spec.Profile == "" {
		return "", errors.New("docker: incomplete viewer")
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := m.api.sendJSON(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(spec.Name), viewerCreateBody(spec), &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// viewerInspect is the part of GET /containers/{id}/json the viewer uses.
type viewerInspect struct {
	ID     string `json:"Id"`
	Name   string
	State  struct{ Running bool }
	Config struct {
		Labels map[string]string
	}
	HostConfig struct {
		NetworkMode string
	}
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string
			Aliases   []string
		}
	}
}

func (m *Manager) inspectAny(ctx context.Context, id string) (viewerInspect, error) {
	var info viewerInspect
	err := m.api.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &info)
	return info, err
}

// inspectViewer inspects a container and refuses one without the label.
func (m *Manager) inspectViewer(ctx context.Context, id string) (viewerInspect, error) {
	info, err := m.inspectAny(ctx, id)
	if err != nil {
		return info, err
	}
	if !m.ownViewer(info.Config.Labels, info.Name) {
		return info, ErrNotViewer
	}
	return info, nil
}

// ViewerStart starts a viewer container.
func (m *Manager) ViewerStart(ctx context.Context, id string) error {
	if _, err := m.inspectViewer(ctx, id); err != nil {
		return err
	}
	return m.api.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil)
}

// ViewerStopSeconds is how long a viewer gets to end on its own before it
// is killed: Chromium writes cookies and the profile on exit.
const ViewerStopSeconds = 10

// ViewerRemove stops a viewer container (giving it ViewerStopSeconds) and
// removes it. A container that is gone already counts as removed. ctx must
// allow for the stop time.
func (m *Manager) ViewerRemove(ctx context.Context, id string) error {
	if _, err := m.inspectViewer(ctx, id); err != nil {
		if notFound(err) {
			return nil
		}
		return err
	}
	// A container that is not running answers 304, which is no error.
	if err := m.api.post(ctx, "/containers/"+url.PathEscape(id)+"/stop", url.Values{"t": {strconv.Itoa(ViewerStopSeconds)}}); err != nil && !notFound(err) {
		return err
	}
	_, err := m.api.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), url.Values{"force": {"true"}})
	if notFound(err) {
		return nil
	}
	return err
}

// ViewerState is the IPv4 address of a viewer container on network and
// whether it runs.
func (m *Manager) ViewerState(ctx context.Context, id, network string) (address string, running bool, err error) {
	info, err := m.inspectViewer(ctx, id)
	if err != nil {
		return "", false, err
	}
	return info.NetworkSettings.Networks[network].IPAddress, info.State.Running, nil
}

// ViewerMemory is the memory a viewer container uses now, without the
// page cache the kernel can drop.
func (m *Manager) ViewerMemory(ctx context.Context, id string) (uint64, error) {
	if _, err := m.inspectViewer(ctx, id); err != nil {
		return 0, err
	}
	s, err := m.api.stats(ctx, id)
	if err != nil {
		return 0, err
	}
	usage := s.MemoryStats.Usage
	if cache := s.MemoryStats.Stats["inactive_file"]; cache < usage {
		usage -= cache
	}
	return usage, nil
}

// ViewerImageID returns the ID of an image, or "" if there is none.
func (m *Manager) ViewerImageID(ctx context.Context, ref string) (string, error) {
	var img struct {
		ID string `json:"Id"`
	}
	err := m.api.getJSON(ctx, "/images/"+url.PathEscape(ref)+"/json", nil, &img)
	if notFound(err) {
		return "", nil
	}
	return img.ID, err
}

// ViewerLoadImage loads an image archive (docker save, optionally zstd,
// gzip or xz compressed: the engine detects it) into the engine.
func (m *Manager) ViewerLoadImage(ctx context.Context, archive io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/v1.43/images/load?quiet=1", archive)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-tar")
	res, err := m.api.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var problem struct{ Message string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&problem) // an unreadable answer leaves the status alone
		return &apiError{Status: res.StatusCode, Message: problem.Message}
	}
	// The answer streams JSON lines; a failure is a line with "error".
	scanner := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	for scanner.Scan() {
		var line struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Error != "" {
			return errors.New("docker: " + clipText(line.Error, 300))
		}
	}
	return scanner.Err()
}

// ViewerNetwork makes sure the internal network of one view exists, with
// a fixed bridge name (bridge, at most 15 characters) for the host rules.
// A network of that name that is not internal or not the viewer's own is
// refused, never changed.
func (m *Manager) ViewerNetwork(ctx context.Context, name, bridge, user, app string) error {
	if !strings.HasPrefix(name, m.viewerPrefix()) || bridge == "" || len(bridge) > 15 {
		return errors.New("docker: invalid viewer network")
	}
	var net struct {
		Name     string
		Internal bool
		Labels   map[string]string
		Options  map[string]string
	}
	err := m.api.getJSON(ctx, "/networks/"+url.PathEscape(name), nil, &net)
	if err == nil {
		if !net.Internal || !m.ownViewer(net.Labels, net.Name) || net.Options["com.docker.network.bridge.name"] != bridge {
			return fmt.Errorf("docker: network %s exists and is not an internal network of the remote view", name)
		}
		return nil
	}
	if !notFound(err) {
		return err
	}
	body := map[string]any{"Name": name, "Driver": "bridge", "Internal": true, "CheckDuplicate": true,
		"Options": map[string]string{"com.docker.network.bridge.name": bridge, "com.docker.network.bridge.enable_icc": "true"},
		"Labels":  map[string]string{ViewerLabel: "1", ViewerLabel + ".user": user, ViewerLabel + ".app": app}}
	return m.api.sendJSON(ctx, http.MethodPost, "/networks/create", body, nil)
}

// ViewerNetworks lists the networks of the remote view (label and prefix).
func (m *Manager) ViewerNetworks(ctx context.Context) ([]string, error) {
	var list []struct {
		Name   string
		Labels map[string]string
	}
	filters := `{"label":["` + ViewerLabel + `=1"]}`
	if err := m.api.getJSON(ctx, "/networks", url.Values{"filters": {filters}}, &list); err != nil {
		return nil, err
	}
	out := []string{}
	for _, n := range list {
		if m.ownViewer(n.Labels, n.Name) {
			out = append(out, n.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// ViewerRemoveNetwork removes a network of the remote view. Docker refuses
// while containers are still on it.
func (m *Manager) ViewerRemoveNetwork(ctx context.Context, name string) error {
	var net struct {
		Name   string
		Labels map[string]string
	}
	if err := m.api.getJSON(ctx, "/networks/"+url.PathEscape(name), nil, &net); err != nil {
		if notFound(err) {
			return nil
		}
		return err
	}
	if !m.ownViewer(net.Labels, net.Name) {
		return errors.New("docker: not a network of the remote view")
	}
	_, err := m.api.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(name), nil)
	if notFound(err) {
		return nil
	}
	return err
}

// AppEndpoint is a container of an app as the viewer needs it.
type AppEndpoint struct {
	ID, Name, State string
	// NetworkMode is Docker's (bridge, host, container:…, a network name).
	NetworkMode string
	// WebUI is the label titanos.webui.
	WebUI string
	// Ports are the published TCP ports.
	Ports []Port
}

// AppEndpoints lists the containers of an app (Compose project).
func (m *Manager) AppEndpoints(ctx context.Context, app string) ([]AppEndpoint, error) {
	var list []apiContainer
	filters, err := json.Marshal(map[string][]string{"label": {"com.docker.compose.project=" + app}})
	if err != nil {
		return nil, err
	}
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}, "filters": {string(filters)}}, &list); err != nil {
		return nil, err
	}
	out := []AppEndpoint{}
	for _, c := range list {
		if c.Labels["com.docker.compose.project"] != app || c.Labels[ViewerLabel] != "" || c.Labels[sandboxLabel] != "" {
			continue
		}
		e := AppEndpoint{ID: c.ID, State: c.State, NetworkMode: c.HostConfig.NetworkMode, WebUI: c.Labels[WebUILabel], Ports: []Port{}}
		if len(c.Names) > 0 {
			e.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, p := range c.Ports {
			port := Port{Host: p.PublicPort, Container: p.PrivatePort, Protocol: p.Type}
			if p.PublicPort != 0 && !slices.Contains(e.Ports, port) {
				e.Ports = append(e.Ports, port)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// ViewerAttach connects an app container to the viewer network, marked
// with alias, and returns its address there. attached is false when the
// container was on the network already: then it is left as it was and
// ViewerDetach will not take it off.
func (m *Manager) ViewerAttach(ctx context.Context, network, id, alias string) (address string, attached bool, err error) {
	info, err := m.inspectAny(ctx, id)
	if err != nil {
		return "", false, err
	}
	if info.Config.Labels[ViewerLabel] != "" || strings.HasPrefix(strings.TrimPrefix(info.Name, "/"), ViewerPrefix) {
		return "", false, errors.New("docker: a viewer is no app")
	}
	if n, ok := info.NetworkSettings.Networks[network]; ok {
		return n.IPAddress, false, nil
	}
	body := map[string]any{"Container": id, "EndpointConfig": map[string]any{"Aliases": []string{alias}}}
	if err := m.api.sendJSON(ctx, http.MethodPost, "/networks/"+url.PathEscape(network)+"/connect", body, nil); err != nil {
		return "", false, err
	}
	info, err = m.inspectAny(ctx, id)
	if err != nil {
		return "", true, err
	}
	return info.NetworkSettings.Networks[network].IPAddress, true, nil
}

// ViewerDetach takes an app container off a viewer network, but only if
// it carries alias there, i.e. ViewerAttach put it there.
func (m *Manager) ViewerDetach(ctx context.Context, network, id, alias string) error {
	if !strings.HasPrefix(network, m.viewerPrefix()) {
		return errors.New("docker: not a network of the remote view")
	}
	info, err := m.inspectAny(ctx, id)
	if notFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	n, ok := info.NetworkSettings.Networks[network]
	if !ok || !slices.Contains(n.Aliases, alias) {
		return nil
	}
	body := map[string]any{"Container": id, "Force": false}
	return m.api.sendJSON(ctx, http.MethodPost, "/networks/"+url.PathEscape(network)+"/disconnect", body, nil)
}

// ViewerAttached lists the containers on the viewer network that carry
// alias there and are no viewers: the apps ViewerAttach connected.
func (m *Manager) ViewerAttached(ctx context.Context, network, alias string) ([]string, error) {
	var net struct {
		Containers map[string]json.RawMessage
	}
	if err := m.api.getJSON(ctx, "/networks/"+url.PathEscape(network), nil, &net); err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	// The network itself only lists containers that run. An app that is
	// stopped right now (by the person, for a backup) still names the
	// network, and would not start again once the network is gone: ask
	// the engine for every container that refers to it, stopped or not.
	ids := map[string]bool{}
	for id := range net.Containers {
		ids[id] = true
	}
	var referring []struct{ Id string }
	filters, _ := json.Marshal(map[string][]string{"network": {network}})
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}, "filters": {string(filters)}}, &referring); err == nil {
		for _, c := range referring {
			ids[c.Id] = true
		}
	}
	out := []string{}
	for id := range ids {
		info, err := m.inspectAny(ctx, id)
		if err != nil || info.Config.Labels[ViewerLabel] != "" || strings.HasPrefix(strings.TrimPrefix(info.Name, "/"), ViewerPrefix) {
			continue
		}
		if n, ok := info.NetworkSettings.Networks[network]; ok && slices.Contains(n.Aliases, alias) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func notFound(err error) bool {
	var api *apiError
	return errors.As(err, &api) && api.Status == http.StatusNotFound
}

// LeasedHardware lists the devices (PCI addresses) handed to a VM right
// now; the remote view does not use them.
func (m *Manager) LeasedHardware() map[string]bool {
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	return m.blockedHardware()
}

// ViewerImageLabel marks images of the remote view (viewer/Dockerfile);
// ViewerReleaseLabel names the release they were built for. Only images
// with both are the viewer's own.
const (
	ViewerImageLabel   = "titanos.viewer.image"
	ViewerReleaseLabel = "titanos.viewer.release"
)

func ownImage(labels map[string]string) bool {
	return labels[ViewerImageLabel] == "1" && labels[ViewerReleaseLabel] != ""
}

// ViewerImage is an image of the remote view.
type ViewerImage struct {
	ID   string
	Tags []string
}

// ViewerImages lists the images that carry ViewerImageLabel.
func (m *Manager) ViewerImages(ctx context.Context) ([]ViewerImage, error) {
	var list []struct {
		ID       string `json:"Id"`
		RepoTags []string
		Labels   map[string]string
	}
	filters := `{"label":["` + ViewerImageLabel + `=1"]}`
	if err := m.api.getJSON(ctx, "/images/json", url.Values{"filters": {filters}}, &list); err != nil {
		return nil, err
	}
	out := []ViewerImage{}
	for _, img := range list {
		if ownImage(img.Labels) {
			out = append(out, ViewerImage{ID: img.ID, Tags: slices.DeleteFunc(img.RepoTags, func(t string) bool { return t == "<none>:<none>" })})
		}
	}
	return out, nil
}

// ViewerRemoveImage removes an image of the remote view, never by force:
// Docker refuses while a container uses it. Other images are refused.
func (m *Manager) ViewerRemoveImage(ctx context.Context, id string) error {
	var img struct {
		ID     string `json:"Id"`
		Config struct{ Labels map[string]string }
	}
	if err := m.api.getJSON(ctx, "/images/"+url.PathEscape(id)+"/json", nil, &img); err != nil {
		if notFound(err) {
			return nil
		}
		return err
	}
	if !ownImage(img.Config.Labels) {
		return errors.New("docker: not an image of the remote view")
	}
	_, err := m.api.do(ctx, http.MethodDelete, "/images/"+url.PathEscape(img.ID), nil)
	if notFound(err) {
		return nil
	}
	return err
}
