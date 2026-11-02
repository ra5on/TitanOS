package hwcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the expected results of the snapshots")

// A snapshot of a machine, unpacked, gives the same results as the machine.
func TestSnapshotRoundTrip(t *testing.T) {
	tr := newTree(t)
	tr.file("proc/sys/kernel/osrelease", "6.12.113+deb13-amd64")
	tr.file("proc/cpuinfo", cpuinfoNew)
	tr.pci("0000:00:0e.0", "8086", "ad0b", "010400", "")
	tr.pci("0000:00:02.0", "8086", "7d67", "030000", "i915")
	tr.pci("0000:80:1f.5", "8086", "ae23", "0c8000", "")
	tr.file("modules/6.12.113+deb13-amd64/modules.alias", "alias pci:v00008086d0000AD0Bsv*sd*bc*sc*i* vmd\nalias acpi*:X:* other")
	tr.file("modprobe.d/x.conf", "blacklist nouveau")
	tr.file("sys/class/dmi/id/board_name", "Z890 Test")
	tr.file("sys/class/dmi/id/board_serial", "SECRET-BOARD")
	c := tr.checker()
	c.Runner = &fakeRunner{output: "vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)\nusb 1-3: SerialNumber: SECRET123\nr8169 0000:03:00.0 eth0: 10:20:30:40:50:60, IRQ 120\nnet: got 192.168.10.5"}
	var archive bytes.Buffer
	if err := c.Snapshot(context.Background(), &archive, "0.45.0"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Unpack(bytes.NewReader(archive.Bytes()), dir); err != nil {
		t.Fatal(err)
	}
	snap := OpenSnapshot(dir)
	snap.PCIIDs = c.PCIIDs
	want, got := c.Report(context.Background()), snap.Report(context.Background())
	got.Generated = want.Generated
	if !reflect.DeepEqual(want.Unbound, got.Unbound) || !reflect.DeepEqual(want.Graphics, got.Graphics) || want.Board != got.Board || want.Kernel != got.Kernel {
		t.Errorf("snapshot differs:\nmachine  %+v\nsnapshot %+v", want, got)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "kernel.log"))
	for _, secret := range []string{"SECRET123", "10:20:30:40:50:60", "192.168.10.5", "fd7a:115c", "2001:db8"} {
		if strings.Contains(string(log), secret) {
			t.Errorf("%s in the kernel log of the snapshot", secret)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sys/class/dmi/id/board_serial")); err == nil {
		t.Error("board serial in the snapshot")
	}
}

func TestUnpackRefusesEscapingPaths(t *testing.T) {
	for _, archive := range []string{"../evil", "/abs"} {
		var buf bytes.Buffer
		s := &snapshot{files: map[string][]byte{archive: []byte("x")}, links: map[string]string{}, dirs: map[string]bool{}}
		s.write(&buf)
		if err := Unpack(&buf, t.TempDir()); err == nil {
			t.Errorf("%s unpacked", archive)
		}
	}
	var buf bytes.Buffer
	s := &snapshot{files: map[string][]byte{}, links: map[string]string{"sys/link": "../../../etc"}, dirs: map[string]bool{}}
	s.write(&buf)
	if err := Unpack(&buf, t.TempDir()); err == nil {
		t.Error("escaping link unpacked")
	}
}

// snapshotResult is what a snapshot is checked against.
type snapshotResult struct {
	Kernel   string            `json:"kernel"`
	Board    string            `json:"board"`
	Unbound  map[string]string `json:"unbound"`
	Graphics map[string]string `json:"graphics"`
	Disks    map[string]string `json:"disks"`
}

// Every snapshot of testdata/snapshots: the hardware page says what it
// said when the snapshot was added (or last updated on purpose), and never
// "not supported" for a device a module of the snapshot's kernel matches.
func TestSnapshots(t *testing.T) {
	archives, _ := filepath.Glob("testdata/snapshots/*.tar.gz")
	if len(archives) == 0 {
		t.Fatal("no snapshots")
	}
	for _, path := range archives {
		name := strings.TrimSuffix(filepath.Base(path), ".tar.gz")
		t.Run(name, func(t *testing.T) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			dir := t.TempDir()
			if err := Unpack(file, dir); err != nil {
				t.Fatal(err)
			}
			c := OpenSnapshot(dir)
			report := c.Report(context.Background())
			aliases := c.moduleAliases(report.Kernel)
			result := snapshotResult{Kernel: report.Kernel, Board: report.Board, Unbound: map[string]string{}, Graphics: map[string]string{}, Disks: map[string]string{}}
			for _, device := range report.Unbound {
				result.Unbound[device.Bus+" "+device.Address+" "+device.ID] = device.Hint + " " + device.Driver
				if device.Hint == "unsupported" && aliases.module(device.modalias) != "" {
					t.Errorf("%s: unsupported although %s matches", device.Address, aliases.module(device.modalias))
				}
			}
			for _, device := range report.Graphics {
				result.Graphics[device.Address+" "+device.ID] = device.Driver
			}
			for _, disk := range report.Disks {
				result.Disks[disk.Name] = disk.Transport
			}
			expected := strings.TrimSuffix(path, ".tar.gz") + ".json"
			if *update {
				data, _ := json.MarshalIndent(result, "", "  ")
				os.WriteFile(expected, append(data, '\n'), 0o644)
				return
			}
			data, err := os.ReadFile(expected)
			if err != nil {
				t.Fatalf("%v (run with -update to write it)", err)
			}
			var want snapshotResult
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, result) {
				got, _ := json.MarshalIndent(result, "", "  ")
				t.Errorf("result changed (run with -update if on purpose):\n%s", got)
			}
		})
	}
}
