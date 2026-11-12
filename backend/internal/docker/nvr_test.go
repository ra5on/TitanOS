package docker

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNVRCreateBody(t *testing.T) {
	spec := NVRSpec{
		Image: "sha256:abc", Config: "/mnt/tank/.titanos/nvr/config", Media: "/mnt/tank/.titanos/nvr/media",
		ShmBytes: 384 << 20, CacheBytes: 1 << 30, Devices: []string{"/dev/dri/renderD128", "/dev/accel/accel0"},
		Ports: []NVRPort{{8555, "tcp"}, {8555, "udp"}}, Hash: "h1",
	}
	data, err := json.Marshal(nvrCreateBody(spec))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Image        string
		Labels       map[string]string
		ExposedPorts map[string]any
		StopTimeout  int
		HostConfig   struct {
			NetworkMode string
			Binds       []string
			Mounts      []struct {
				Type, Source, Target string
				ReadOnly             bool
			}
			CapDrop           []string
			PidsLimit         int64
			Tmpfs             map[string]string
			ShmSize           int64
			Privileged        bool
			Devices           []struct{ PathOnHost, PathInContainer, CgroupPermissions string }
			DeviceCgroupRules []string
			RestartPolicy     struct{ Name string }
			PortBindings      map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string
			}
		}
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	h := body.HostConfig
	if body.Image != "sha256:abc" || body.Labels[NVRLabel] != "1" || body.Labels[NVRSpecLabel] != "h1" || body.StopTimeout != 30 {
		t.Errorf("%+v", body)
	}
	// Not privileged, on its own network, and Docker never restarts it:
	// titand counts the ends of Frigate itself.
	if h.Privileged || h.NetworkMode != NVRName || h.RestartPolicy.Name != "no" || h.ShmSize != 384<<20 {
		t.Errorf("%+v", h)
	}
	// Bind mounts, never "Binds": a missing folder fails the start
	// instead of becoming an empty folder in RAM.
	mounts := []string{}
	for _, m := range h.Mounts {
		mounts = append(mounts, fmt.Sprintf("%s %s>%s %v", m.Type, m.Source, m.Target, m.ReadOnly))
	}
	wantMounts := []string{"bind /mnt/tank/.titanos/nvr/config>/config false", "bind /mnt/tank/.titanos/nvr/media>/media/frigate false", "bind /etc/localtime>/etc/localtime true"}
	if len(h.Binds) != 0 || strings.Join(mounts, ", ") != strings.Join(wantMounts, ", ") || h.Tmpfs["/tmp/cache"] != "rw,nosuid,nodev,size=1073741824" {
		t.Errorf("binds %v mounts %v tmpfs %v", h.Binds, mounts, h.Tmpfs)
	}
	if strings.Join(h.CapDrop, ",") != "NET_RAW" || h.PidsLimit != NVRPidsLimit {
		t.Errorf("capdrop %v pids %d", h.CapDrop, h.PidsLimit)
	}
	if len(h.Devices) != 2 || h.Devices[1].PathOnHost != "/dev/accel/accel0" || h.Devices[1].PathInContainer != "/dev/accel/accel0" || len(h.DeviceCgroupRules) != 0 {
		t.Errorf("devices %+v rules %v", h.Devices, h.DeviceCgroupRules)
	}
	// Only WebRTC is published: never 5000, 8971 or 1984; IPv4 only.
	if len(h.PortBindings) != 2 || h.PortBindings["8555/tcp"][0].HostPort != "8555" || h.PortBindings["8555/udp"][0].HostPort != "8555" || len(body.ExposedPorts) != 2 {
		t.Errorf("ports %+v", h.PortBindings)
	}
	for key, binding := range h.PortBindings {
		if binding[0].HostIP != "0.0.0.0" {
			t.Errorf("%s published on %q", key, binding[0].HostIP)
		}
	}

	// A Coral: the USB bus as a folder with its device rule.
	spec.USBBus, spec.Devices = true, nil
	data, _ = json.Marshal(nvrCreateBody(spec))
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	h = body.HostConfig
	if len(h.Mounts) != 4 || h.Mounts[3].Source != "/dev/bus/usb" || h.Mounts[3].Target != "/dev/bus/usb" || len(h.DeviceCgroupRules) != 1 || h.DeviceCgroupRules[0] != "c 189:* rmw" || h.Privileged {
		t.Errorf("coral: mounts %+v rules %v", h.Mounts, h.DeviceCgroupRules)
	}
}

func TestOwnNVR(t *testing.T) {
	if !ownNVR(map[string]string{NVRLabel: "1"}, "/titanos-nvr") {
		t.Error("the NVR's container is its own")
	}
	// A container someone named like it, or one with the label under
	// another name, is never touched.
	if ownNVR(nil, "/titanos-nvr") || ownNVR(map[string]string{NVRLabel: "1"}, "/frigate") {
		t.Error("label and name are both needed")
	}
}

func TestReadPull(t *testing.T) {
	stream := `{"status":"Pulling from blakeblackshear/frigate","id":"0.18.2"}
{"status":"Pulling fs layer","progressDetail":{},"id":"aaa"}
{"status":"Pulling fs layer","progressDetail":{},"id":"bbb"}
{"status":"Downloading","progressDetail":{"current":100,"total":1000},"id":"aaa"}
{"status":"Downloading","progressDetail":{"current":200,"total":500},"id":"bbb"}
{"status":"Downloading","progressDetail":{"current":900,"total":1000},"id":"aaa"}
{"status":"Download complete","progressDetail":{},"id":"aaa"}
{"status":"Extracting","progressDetail":{"current":50,"total":1000},"id":"aaa"}
{"status":"Download complete","progressDetail":{},"id":"bbb"}
{"status":"Pull complete","progressDetail":{},"id":"aaa"}
{"status":"Digest: sha256:9eba"}
`
	var steps [][2]int64
	if err := readPull(strings.NewReader(stream), func(done, total int64) { steps = append(steps, [2]int64{done, total}) }); err != nil {
		t.Fatal(err)
	}
	last := steps[len(steps)-1]
	if steps[0] != [2]int64{100, 1000} || steps[1] != [2]int64{300, 1500} || last != [2]int64{1500, 1500} {
		t.Errorf("progress: %v", steps)
	}
	// Extracting must not pull the count back.
	for i := 1; i < len(steps); i++ {
		if steps[i][0] < steps[i-1][0] {
			t.Errorf("progress went back: %v", steps)
		}
	}
	err := readPull(strings.NewReader(`{"status":"Pulling"}`+"\n"+`{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}`+"\n"), nil)
	if err == nil || !strings.Contains(err.Error(), "manifest unknown") {
		t.Errorf("error line: %v", err)
	}
}
