package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// answerRunner answers commands that contain a key with its text.
type answerRunner struct {
	answers  map[string]string
	commands []string
}

func (r *answerRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, line)
	for key, answer := range r.answers {
		if strings.Contains(line, key) {
			if strings.HasPrefix(answer, "error:") {
				return "", errors.New(answer)
			}
			return answer, nil
		}
	}
	return "", nil
}

func running(t *testing.T, m *Manager, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.pidDir, name+".pid"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCDDriveAlwaysThere(t *testing.T) {
	m, _ := newTestManager(t)
	for _, vm := range []VM{
		{Name: "linux", Template: "linux", Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2"}}},
		{Name: "win", Template: "windows", Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2"}, {File: "vdisk2.img", Format: "raw"}}},
	} {
		vm.Volume, vm.MemoryMiB, vm.VCPUs, vm.MAC = "fast", 1024, 1, "52:54:00:00:00:01"
		xml, err := m.domainXML(vm)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"linux": `<target dev="sda" bus="sata">`, "win": `<target dev="sdc" bus="sata">`}[vm.Name]
		if !strings.Contains(xml, `device="cdrom"`) || !strings.Contains(xml, want) || strings.Contains(xml, `<source file=""`) {
			t.Errorf("%s: no empty CD drive at %s:\n%s", vm.Name, want, xml)
		}
		if !strings.Contains(xml, "org.qemu.guest_agent.0") {
			t.Errorf("%s: no channel for the guest agent", vm.Name)
		}
	}
}

func TestSetMedium(t *testing.T) {
	m, _ := newTestManager(t)
	runner := &answerRunner{}
	m.runner = runner
	ctx := context.Background()
	iso := filepath.Join(m.mountRoot, "data", "media", "virtio-win.iso")
	os.WriteFile(iso, []byte("iso"), 0o644)
	m.vms["win"] = VM{Name: "win", Template: "windows", Volume: "fast", Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2"}}}
	m.vms["live"] = VM{Name: "live", Template: "linux", Volume: "fast", ISO: iso}

	if err := m.SetMedium(ctx, "win", "/etc/hostname"); !errors.Is(err, ErrBadISO) {
		t.Errorf("medium outside the volumes: %v", err)
	}
	if err := m.SetMedium(ctx, "live", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("ejecting the only boot medium: %v", err)
	}
	// Stopped: only the settings change.
	if err := m.SetMedium(ctx, "win", iso); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 || m.vms["win"].ISO != iso {
		t.Errorf("stopped VM: commands %v, ISO %q", runner.commands, m.vms["win"].ISO)
	}
	var stored VM
	if err := m.store.ReadJSON(configDir+"/win.json", &stored); err != nil || stored.ISO != iso {
		t.Errorf("not stored: %v %q", err, stored.ISO)
	}
	// Running: the drive changes at once.
	running(t, m, "win")
	if err := m.SetMedium(ctx, "win", ""); err != nil {
		t.Fatal(err)
	}
	if want := "virsh --quiet change-media win sdb --update --live --force"; !slices.Contains(runner.commands, want) || m.vms["win"].ISO != "" {
		t.Errorf("eject: %v, ISO %q", runner.commands, m.vms["win"].ISO)
	}
	if err := m.SetMedium(ctx, "win", iso); err != nil {
		t.Fatal(err)
	}
	if want := "virsh --quiet change-media win sdb " + iso + " --update --live --force"; runner.commands[len(runner.commands)-1] != want {
		t.Errorf("insert: %v", runner.commands)
	}
	// What libvirt refuses is not stored.
	runner.answers = map[string]string{"change-media": "error: no drive"}
	if err := m.SetMedium(ctx, "win", ""); err == nil || m.vms["win"].ISO != iso {
		t.Errorf("failed change: %v, ISO %q", err, m.vms["win"].ISO)
	}
}

func TestAddresses(t *testing.T) {
	m, _ := newTestManager(t)
	runner := &answerRunner{answers: map[string]string{}}
	m.runner = runner
	m.procDir = t.TempDir()
	os.MkdirAll(filepath.Join(m.procDir, "net"), 0o755)
	os.MkdirAll(filepath.Join(m.procDir, fmt.Sprint(os.Getpid())), 0o755)
	ctx := context.Background()
	mac := "52:54:00:aa:bb:cc"
	m.vms["vm"] = VM{Name: "vm", Template: "linux", Network: "bridge", MAC: mac}

	if _, err := m.Addresses(ctx, "vm"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("stopped VM: %v", err)
	}
	running(t, m, "vm")

	runner.answers["domifaddr"] = ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 lo         00:00:00:00:00:00    ipv4         127.0.0.1/8
 -          -                    ipv6         ::1/128
 enp1s0     52:54:00:AA:BB:CC    ipv4         192.168.1.42/24
 -          -                    ipv6         fd00::42/64
 -          -                    ipv6         fe80::5054:ff:feaa:bbcc/64
 docker0    02:42:00:00:00:01    ipv4         172.17.0.1/16
`
	got, err := m.Addresses(ctx, "vm")
	if err != nil || got.Source != "agent" || !slices.Equal(got.Addresses, []string{"192.168.1.42", "fd00::42"}) {
		t.Errorf("agent: %+v %v", got, err)
	}

	// Asked again soon, the answer is remembered.
	calls := len(runner.commands)
	m.Addresses(ctx, "vm")
	if len(runner.commands) != calls {
		t.Error("the guest agent was asked again right away")
	}

	// Without the agent, the neighbour table of the NAS.
	delete(m.addresses, "vm")
	runner.answers["domifaddr"] = "error: guest agent is not responding"
	os.WriteFile(filepath.Join(m.procDir, "net/arp"), []byte(`IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         10:00:00:00:00:01     *        br0
192.168.1.43     0x1         0x2         52:54:00:aa:bb:cc     *        br0
192.168.1.44     0x1         0x0         52:54:00:aa:bb:cc     *        br0
`), 0o644)
	got, err = m.Addresses(ctx, "vm")
	if err != nil || got.Source != "arp" || !slices.Equal(got.Addresses, []string{"192.168.1.43"}) {
		t.Errorf("neighbours: %+v %v", got, err)
	}

	// Outgoing connections only: no address in the network.
	m.vms["user"] = VM{Name: "user", Template: "linux", Network: "user", MAC: mac}
	running(t, m, "user")
	if got, err := m.Addresses(ctx, "user"); err != nil || got.Source != "" || len(got.Addresses) != 0 {
		t.Errorf("user network: %+v %v", got, err)
	}
}
