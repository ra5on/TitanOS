package hardware

import (
	"context"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// heldRunner answers nvidia-smi only once release is closed.
type heldRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *heldRunner) Run(context.Context, string, string, ...string) (string, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	return "100, 250, 200", nil
}

// N-102-31: while the loop sets the power limit of a GPU that came back
// (nvidia-smi, up to 30 s), the page still gets the status.
func TestPowerLimitRoundDoesNotHoldTheStatus(t *testing.T) {
	sys := t.TempDir()
	address := "0000:01:00.0"
	device(t, sys, address, "030000", "10de", 1, nil)
	bindDriver(t, sys, address, "nvidia")
	n := NewNvidiaControl(config.Open(t.TempDir()), &Manager{sys: sys}, false)
	r := &heldRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	n.runner = r
	n.loaded = true
	n.settings[address] = NvidiaSettings{PowerPercent: 75}
	done := make(chan struct{})
	go func() { n.applyRound(context.Background()); close(done) }()
	<-r.started
	status := make(chan map[string]any, 1)
	go func() { status <- n.Status() }()
	select {
	case got := <-status:
		if got["settings"].(map[string]NvidiaSettings)[address].PowerPercent != 75 {
			t.Errorf("status: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Error("the status waited for nvidia-smi")
		close(r.release)
		<-status
		<-done
		return
	}
	close(r.release)
	<-done
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.applied[address] || n.problems[address] != "" {
		t.Fatalf("applied %v problems %v", n.applied, n.problems)
	}
}
