package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pausedRunner tells libvirt's reason for the state of the VM.
type pausedRunner struct {
	*fakeRunner
	state string
}

func (r *pausedRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "virsh" && len(args) > 1 && args[1] == "domstate" {
		return r.state, nil
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

func TestDiskErrorPausesAndIsReportedOnce(t *testing.T) {
	m, base := newTestManager(t)
	runner := &pausedRunner{fakeRunner: base, state: "running (booted)\n"}
	m.runner = runner
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	var notes []string
	m.Notify = func(severity, kind, subject string) { notes = append(notes, severity+" "+kind+" "+subject) }
	m.vms["ha"] = VM{Name: "ha"}
	os.WriteFile(filepath.Join(m.pidDir, "ha.pid"), []byte(fmt.Sprint(os.Getpid())), 0o644)

	look := func(state string) {
		runner.state = state
		clock = clock.Add(31 * time.Second)
		m.checkIOErrors(context.Background())
	}
	look("running (booted)\n")
	look("paused (I/O error)\n")
	look("paused (I/O error)\n")
	if len(notes) != 1 || notes[0] != "error vm_io_error ha" {
		t.Fatalf("notes: %v", notes)
	}
	status := m.List(nil)[0]
	if !status.IOError {
		t.Fatal("pause after a disk error not shown")
	}
	if err := m.Action(context.Background(), "ha", "resume"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(base.commands, "\n"), "virsh --quiet resume ha") {
		t.Fatal(base.commands)
	}
	look("running (unpaused)\n")
	if m.List(nil)[0].IOError {
		t.Fatal("still shown after resume")
	}
}

func TestDisksStopOnErrors(t *testing.T) {
	m, _ := newTestManager(t)
	xml, err := m.domainXML(VM{Name: "ha", Template: "other", MemoryMiB: 512, VCPUs: 1, Disks: []Disk{{Format: "qcow2", Path: "/mnt/data/vms/ha/disk.qcow2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, `error_policy="stop"`) && !strings.Contains(xml, `error_policy='stop'`) {
		t.Fatalf("disks without error_policy stop:\n%s", xml)
	}
}
