package shares

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The Linux user of one account cannot be made (its UID is taken): that
// account is left out and reported once, everything else is applied.
func TestAccountThatCannotBeMadeDoesNotBlockTheShares(t *testing.T) {
	m, runner := newTestManager(t)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	var events []string
	m.SetOnEvent(func(severity, kind, subject string) { events = append(events, severity+" "+kind+" "+subject) })
	m.Create(Share{Name: "fotos", Volume: "data", SMB: true, PerUser: true, Access: map[string]string{"alex": ReadWrite, "gast": ReadWrite}}, volumes, users)
	state := State{Users: []auth.User{{Name: "alex", UID: os.Getuid(), SMBHash: "AA"}, {Name: "gast", UID: 2001, SMBHash: "BB"}}, Mounted: []string{"data"}}
	runner.fail = "useradd --uid 2001"
	ctx := context.Background()
	if err := m.Apply(ctx, state); err != nil {
		t.Fatalf("one account failed the whole round: %v", err)
	}
	conf, _ := os.ReadFile(m.sambaConf)
	if !strings.Contains(string(conf), "[fotos]") {
		t.Fatalf("share not offered:\n%s", conf)
	}
	all := strings.Join(runner.commands, "\n")
	if !strings.Contains(all, "systemctl reload-or-restart smbd.service") || strings.Contains(all, "smbpasswd -e gast") || !strings.Contains(all, "smbpasswd -e alex") {
		t.Errorf("commands:\n%s", all)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data/fotos/alex")); err != nil {
		t.Errorf("folder of alex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data/fotos/gast")); err == nil {
		t.Error("a folder was made for the UID of another user")
	}
	if got := m.SkippedAccounts(); !slices.Equal(got, []string{"gast"}) {
		t.Errorf("skipped: %v", got)
	}
	if !slices.Equal(events, []string{"error share_account_skipped gast"}) {
		t.Errorf("events: %v", events)
	}
	// Not tried again every round, and reported only once.
	runner.commands = nil
	if err := m.Apply(ctx, state); err != nil || len(runner.commands) != 0 {
		t.Errorf("next round ran %v (%v)", runner.commands, err)
	}
	now = now.Add(accountRetry + time.Second)
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("reported again: %v", events)
	}
	// Once the UID is free, the account is made.
	runner.fail = ""
	state.Users[1].UID = os.Getuid() // its folder can be given to it in the test
	now = now.Add(accountRetry + time.Second)
	runner.commands = nil
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(runner.commands, "\n"); !strings.Contains(all, "useradd --uid "+strconv.Itoa(os.Getuid())+" ") || len(m.SkippedAccounts()) != 0 {
		t.Errorf("not tried again: %v / %v", all, m.SkippedAccounts())
	}
}

// hangingRunner lets commands starting with hang wait for their context.
type hangingRunner struct {
	*fakeRunner
	hang string
}

func (r *hangingRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if strings.HasPrefix(name+" "+strings.Join(args, " "), r.hang) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A command of the loop that hangs ends at its limit: the round fails and
// the next one can run.
func TestHangingCommandEndsTheRound(t *testing.T) {
	limit, service := commandLimit, serviceLimit
	commandLimit, serviceLimit = 50*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { commandLimit, serviceLimit = limit, service })
	for _, hang := range []string{"pdbedit --import", "systemctl reload-or-restart smbd.service"} {
		t.Run(hang, func(t *testing.T) {
			m, base := newTestManager(t)
			m.runner = &hangingRunner{fakeRunner: base, hang: hang}
			m.Create(Share{Name: "fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}}, volumes, users)
			state := State{Users: []auth.User{{Name: "alex", UID: 2000, SMBHash: "AA"}}, Mounted: []string{"data"}}
			done := make(chan error, 1)
			go func() { done <- m.Apply(context.Background(), state) }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("a command cut off at its limit counted as done")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("a hanging command holds the loop of the shares")
			}
		})
	}
}
