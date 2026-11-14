package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Speech (PLAN 92, package speech, docs/SPEECH.md): speech to text
// (faster-whisper) and text to speech (Piper) run in two containers that
// TitanOS manages and never shows as apps. They sit in a network of their
// own without a way out (internal: models are files titand downloaded,
// the containers never ask the internet) and publish no port; titand
// reaches them at their address in that network and hands the Wyoming
// connections of Home Assistant through itself (speech/proxy.go).
//
// As for the NVR, every call that changes a container first checks the
// label and the name, so nothing is touched that was not made here.

// SpeechLabel marks the containers and the network of the speech services.
const SpeechLabel = "titanos.speech"

// SpeechNetwork is the name of their network.
const SpeechNetwork = "titanos-speech"

// SpeechSpecLabel holds the hash of what a container was created from.
const SpeechSpecLabel = SpeechLabel + ".spec"

// ErrNotSpeech refuses a container that is not one of the speech services.
var ErrNotSpeech = errors.New("docker: not a container of the speech services")

// speechStopSeconds is how long a service gets to end.
const speechStopSeconds = 10

// SpeechSpec is one service's container.
type SpeechSpec struct {
	// Name is titanos-speech-whisper or titanos-speech-piper.
	Name string
	// Image is the ID (sha256:…) of the pulled image.
	Image string
	// Data is the folder with the models, mounted at /config.
	Data string
	Env  []string
	// CDIDevices are CDI names of an NVIDIA GPU (nvidia.com/gpu=<UUID>).
	CDIDevices []string `json:",omitempty"`
	// Hash names everything above; another hash replaces the container.
	Hash string
}

// SpeechState is what the engine says about a service's container.
type SpeechState struct {
	Exists     bool
	ID         string
	Running    bool
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	// Address in the network of the speech services.
	Address     string
	Hash, Image string
}

func speechName(name string) bool {
	return name == SpeechNetwork+"-whisper" || name == SpeechNetwork+"-piper"
}

func speechCreateBody(spec SpeechSpec) map[string]any {
	host := map[string]any{
		"NetworkMode": SpeechNetwork,
		"Mounts": []map[string]any{
			nvrMount(spec.Data, "/config", false),
			nvrMount("/etc/localtime", "/etc/localtime", true),
		},
		"Privileged":  false,
		"SecurityOpt": []string{"no-new-privileges"},
		"CapDrop":     []string{"NET_RAW"},
		"PidsLimit":   int64(1024),
		"IpcMode":     "private",
		// A service that ends is started again by titand (speech/manager.go),
		// which then also says why it ended.
		"RestartPolicy": map[string]any{"Name": "no"},
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "2m", "max-file": "2"}},
	}
	if len(spec.CDIDevices) > 0 {
		host["DeviceRequests"] = []map[string]any{{"Driver": "cdi", "DeviceIDs": spec.CDIDevices}}
	}
	return map[string]any{
		"Image":            spec.Image,
		"Env":              spec.Env,
		"Labels":           map[string]string{SpeechLabel: "1", SpeechSpecLabel: spec.Hash},
		"StopTimeout":      speechStopSeconds,
		"NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{SpeechNetwork: map[string]any{}}},
		"HostConfig":       host,
	}
}

// SpeechInspect reads the state of a service's container.
func (m *Manager) SpeechInspect(ctx context.Context, name string) (SpeechState, error) {
	if !speechName(name) {
		return SpeechState{}, ErrNotSpeech
	}
	var info nvrInspect
	err := m.api.getJSON(ctx, "/containers/"+name+"/json", nil, &info)
	if notFound(err) {
		return SpeechState{}, nil
	}
	if err != nil {
		return SpeechState{}, err
	}
	if info.Config.Labels[SpeechLabel] != "1" || strings.TrimPrefix(info.Name, "/") != name {
		return SpeechState{}, ErrNotSpeech
	}
	return SpeechState{Exists: true, ID: info.ID, Running: info.State.Running, ExitCode: info.State.ExitCode,
		StartedAt: info.State.StartedAt, FinishedAt: info.State.FinishedAt,
		Address: info.NetworkSettings.Networks[SpeechNetwork].IPAddress, Hash: info.Config.Labels[SpeechSpecLabel], Image: info.Image}, nil
}

// SpeechCreate creates and starts a service's container.
func (m *Manager) SpeechCreate(ctx context.Context, spec SpeechSpec) error {
	if !speechName(spec.Name) || !strings.HasPrefix(spec.Image, "sha256:") || spec.Data == "" {
		return errors.New("docker: incomplete speech container")
	}
	m.noteStart(spec.Name)
	if err := m.api.sendJSON(ctx, http.MethodPost, "/containers/create?name="+spec.Name, speechCreateBody(spec), nil); err != nil {
		return err
	}
	return m.api.post(ctx, "/containers/"+spec.Name+"/start", nil)
}

// SpeechStart starts a service's container that exists.
func (m *Manager) SpeechStart(ctx context.Context, name string) error {
	st, err := m.SpeechInspect(ctx, name)
	if err != nil {
		return err
	}
	if !st.Exists {
		return ErrNotFound
	}
	return m.api.post(ctx, "/containers/"+name+"/start", nil)
}

// SpeechRemove stops and removes a service's container; one that is gone
// counts as removed. The models stay.
func (m *Manager) SpeechRemove(ctx context.Context, name string) error {
	st, err := m.SpeechInspect(ctx, name)
	if err != nil || !st.Exists {
		return err
	}
	if err := m.api.post(ctx, "/containers/"+st.ID+"/stop", url.Values{"t": {fmt.Sprint(speechStopSeconds)}}); err != nil && !notFound(err) {
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

// SpeechNetworkEnsure makes the network of the speech services: a bridge
// without a way out of the NAS.
func (m *Manager) SpeechNetworkEnsure(ctx context.Context) error {
	var net struct {
		Name     string
		Internal bool
		Labels   map[string]string
	}
	err := m.api.getJSON(ctx, "/networks/"+SpeechNetwork, nil, &net)
	if err == nil {
		if net.Labels[SpeechLabel] != "1" || !net.Internal {
			return fmt.Errorf("docker: network %s exists and is not the network of the speech services", SpeechNetwork)
		}
		return nil
	}
	if !notFound(err) {
		return err
	}
	body := map[string]any{"Name": SpeechNetwork, "Driver": "bridge", "Internal": true, "CheckDuplicate": true, "Labels": map[string]string{SpeechLabel: "1"}}
	return m.api.sendJSON(ctx, http.MethodPost, "/networks/create", body, nil)
}

// SpeechLogs are the last lines a service wrote.
func (m *Manager) SpeechLogs(ctx context.Context, name string, tail int) (string, error) {
	st, err := m.SpeechInspect(ctx, name)
	if err != nil {
		return "", err
	}
	if !st.Exists {
		return "", ErrNotFound
	}
	res, err := m.api.do(ctx, http.MethodGet, "/containers/"+st.ID+"/logs", url.Values{"stdout": {"true"}, "stderr": {"true"}, "tail": {strconv.Itoa(tail)}})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return demux(bufio.NewReader(io.LimitReader(res.Body, 1<<20)))
}
