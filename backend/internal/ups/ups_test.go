package ups

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

type fakeSystem struct {
	upsc  string
	calls []string
}

func (f *fakeSystem) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "upsc":
		if f.upsc == "" {
			return "Error: Connection failure: Connection refused", &run.CommandError{Command: name, Err: errors.New("exit status 1")}
		}
		return f.upsc, nil
	case "nut-scanner":
		return "[nutdev1]\n\tdriver = \"usbhid-ups\"\n\tport = \"auto\"\n\tvendorid = \"051D\"\n\tproduct = \"Back-UPS ES 700G\"\n", nil
	}
	return "", nil
}

func (f *fakeSystem) ran(part string) bool {
	for _, call := range f.calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func newManager(t *testing.T) (*Manager, *fakeSystem, *[]string) {
	t.Helper()
	root := t.TempDir()
	system := &fakeSystem{}
	m := NewManager(config.Open(filepath.Join(root, "config")))
	m.runner, m.etcDir = system, filepath.Join(root, "nut")
	events := &[]string{}
	m.OnEvent = func(kind, subject string) { *events = append(*events, kind) }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, system, events
}

func read(t *testing.T, m *Manager, name string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(m.etcDir, name))
	return string(data)
}

func TestLocalUPS(t *testing.T) {
	m, system, _ := newManager(t)
	if err := m.SetSettings(Settings{Mode: "local", ShutdownAfter: 5}); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, m, "ups.conf"); !strings.Contains(got, "[ups]\n\tdriver = usbhid-ups\n\tport = auto") {
		t.Errorf("ups.conf: %s", got)
	}
	if got := read(t, m, "nut.conf"); !strings.Contains(got, "MODE=standalone") {
		t.Errorf("nut.conf: %s", got)
	}
	// Nobody else is let in.
	if got := read(t, m, "upsd.conf"); strings.Contains(got, "0.0.0.0") || !strings.Contains(got, "LISTEN 127.0.0.1 3493") {
		t.Errorf("upsd.conf: %s", got)
	}
	users, monitor := read(t, m, "upsd.users"), read(t, m, "upsmon.conf")
	if !strings.Contains(users, "[local]") || strings.Contains(users, "[monitor]") || !strings.Contains(monitor, "MONITOR ups@localhost 1 local ") || !strings.Contains(monitor, " primary") || !strings.Contains(monitor, `SHUTDOWNCMD "/usr/bin/systemctl poweroff"`) {
		t.Errorf("upsd.users:\n%s\nupsmon.conf:\n%s", users, monitor)
	}
	if info, _ := os.Stat(filepath.Join(m.etcDir, "upsd.users")); info.Mode().Perm() != 0o640 {
		t.Errorf("upsd.users is readable for others: %v", info.Mode())
	}
	for _, want := range []string{"upsdrvsvcctl resync", "systemctl start nut-driver@ups.service", "systemctl start nut-server.service nut-monitor.service", "chgrp nut"} {
		if !system.ran(want) {
			t.Errorf("%q not run: %v", want, system.calls)
		}
	}
	// The password of the local account is the same in both files.
	password := strings.Fields(strings.Split(monitor, "\n")[1])[4]
	if !strings.Contains(users, "password = "+password) || len(password) < 20 {
		t.Errorf("local password %q not in upsd.users", password)
	}
}

func TestServingTheUPS(t *testing.T) {
	m, _, _ := newManager(t)
	if err := m.SetSettings(Settings{Mode: "local", Serve: true}); err != nil {
		t.Fatal(err)
	}
	m.Apply(context.Background())
	settings, _ := m.Settings()
	if settings.RemotePassword == "" || !strings.Contains(read(t, m, "upsd.users"), "[monitor]\n\tpassword = "+settings.RemotePassword+"\n\tupsmon secondary") {
		t.Errorf("users: %s", read(t, m, "upsd.users"))
	}
	if !strings.Contains(read(t, m, "upsd.conf"), "LISTEN 0.0.0.0 3493") || !strings.Contains(read(t, m, "nut.conf"), "MODE=netserver") {
		t.Error("not served to the network")
	}
	// The password for others stays when the settings change.
	m.SetSettings(Settings{Mode: "local", Serve: true, ShutdownAfter: 3})
	if again, _ := m.Settings(); again.RemotePassword != settings.RemotePassword {
		t.Error("the password for other machines changed")
	}
}

func TestNetworkUPS(t *testing.T) {
	m, system, _ := newManager(t)
	if err := m.SetSettings(Settings{Mode: "network", Host: "nas2.local", Name: "eaton", User: "monuser", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	m.Apply(context.Background())
	if got := read(t, m, "upsmon.conf"); !strings.Contains(got, "MONITOR eaton@nas2.local 1 monuser secret secondary") || !strings.Contains(read(t, m, "nut.conf"), "MODE=netclient") {
		t.Errorf("upsmon.conf: %s", got)
	}
	if read(t, m, "ups.conf") != "" || system.ran("nut-server.service nut-monitor") {
		t.Error("a driver or server was started for a UPS on the network")
	}
	settings, set := m.Settings()
	if settings.Password != "" || !set {
		t.Errorf("the password is given out: %+v", settings)
	}
	// Saving what the web UI got back keeps the password.
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.files(m.settings, "x")["upsmon.conf"], "monuser secret secondary") {
		t.Error("the password was lost")
	}
	m.SetSettings(Settings{Mode: "off"})
	m.Apply(context.Background())
	if read(t, m, "upsmon.conf") != "" || !strings.Contains(read(t, m, "nut.conf"), "MODE=none") {
		t.Error("NUT still set up after switching off")
	}
}

func TestInvalidSettings(t *testing.T) {
	m, _, _ := newManager(t)
	for name, settings := range map[string]Settings{
		"mode":              {Mode: "solar"},
		"driver":            {Mode: "local", Driver: "../../bin/sh"},
		"usb port":          {Mode: "local", Driver: "usbhid-ups", Port: "/dev/sda"},
		"serial port":       {Mode: "local", Driver: "apcsmart", Port: "/dev/sda"},
		"dummy file":        {Mode: "local", Driver: "dummy-ups", Port: "/etc/shadow"},
		"host":              {Mode: "network", Host: "a b", Name: "ups", User: "u", Password: "p"},
		"name":              {Mode: "network", Host: "nas", Name: "ups\n[evil]", User: "u", Password: "p"},
		"password newline":  {Mode: "network", Host: "nas", Name: "ups", User: "u", Password: "p\nMONITOR x"},
		"shutdown too late": {Mode: "local", ShutdownAfter: 1000},
	} {
		if err := m.SetSettings(settings); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWatchingAndShuttingDown(t *testing.T) {
	m, system, events := newManager(t)
	m.SetSettings(Settings{Mode: "local", ShutdownAfter: 5})
	clock := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	powered := false
	m.Poweroff = func() error { powered = true; return nil }
	look := func(upsc string, after time.Duration) Status {
		clock = clock.Add(after)
		system.upsc = upsc
		m.look(context.Background())
		return m.Status()
	}
	online := "battery.charge: 100\nbattery.runtime: 1800\nups.load: 23\ndevice.mfr: APC\ndevice.model: Back-UPS ES 700G\nups.status: OL\n"
	status := look(online, 0)
	if !status.Connected || status.OnBattery || status.Charge != 100 || status.Runtime != 1800 || status.Load != 23 || status.Model != "APC Back-UPS ES 700G" {
		t.Errorf("online: %+v", status)
	}
	battery := strings.Replace(online, "OL", "OB DISCHRG", 1)
	look(battery, time.Minute)
	status = look(battery, 4*time.Minute)
	if !status.OnBattery || status.ShuttingDown || system.ran("upsmon -c fsd") {
		t.Errorf("after four minutes: %+v", status)
	}
	look(online, 30*time.Second)
	if status := look(battery, 30*time.Second); status.Since == nil || !status.Since.Equal(clock) {
		t.Errorf("the time on battery did not start again: %+v", status)
	}
	look(battery, 4*time.Minute)
	status = look(battery, time.Minute+time.Second)
	if !status.ShuttingDown || !system.ran("upsmon -c fsd") || powered {
		t.Errorf("after five minutes: %+v %v", status, system.calls)
	}
	look(strings.Replace(online, "OL", "OB LB", 1), time.Second)
	look("", time.Second)
	if got := strings.Join(*events, ","); got != "ups_on_battery,ups_online,ups_on_battery,ups_shutdown,ups_low_battery,ups_lost" {
		t.Errorf("events: %s", got)
	}

	// A NAS that only watches a UPS on the network shuts itself down.
	m.SetSettings(Settings{Mode: "network", Host: "nas2", Name: "ups", User: "u", Password: "p", ShutdownAfter: 1})
	m.mu.Lock()
	m.status = Status{}
	m.mu.Unlock()
	look(battery, 0)
	look(battery, time.Minute)
	if !powered {
		t.Error("not shut down on battery")
	}
}

func TestScan(t *testing.T) {
	m, _, _ := newManager(t)
	found, err := m.Scan(context.Background())
	if err != nil || len(found) != 1 || found[0].Driver != "usbhid-ups" || found[0].Port != "auto" || found[0].Product != "Back-UPS ES 700G" {
		t.Errorf("found: %+v %v", found, err)
	}
}

func TestDevelopmentMode(t *testing.T) {
	m, system, _ := newManager(t)
	m.Disabled = true
	if err := m.SetSettings(Settings{Mode: "local"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("settings: %v", err)
	}
	m.Apply(context.Background())
	if len(system.calls) != 0 {
		t.Errorf("calls: %v", system.calls)
	}
}

func TestRestartOfTitandKeepsWatchingTheUPS(t *testing.T) {
	m, system, _ := newManager(t)
	if err := m.SetSettings(Settings{Mode: "local", ShutdownAfter: 5}); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	users := read(t, m, "upsd.users")

	// titand starts again, as after an app update; NUT runs.
	again := NewManager(m.store)
	again.runner, again.etcDir = system, m.etcDir
	again.Load()
	system.calls = nil
	if err := again.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if system.ran("systemctl stop") || system.ran("systemctl start") {
		t.Errorf("NUT was restarted with titand: %v", system.calls)
	}
	if read(t, m, "upsd.users") != users {
		t.Error("the local password changed")
	}
}

// A comment sign, quote or backslash in a password must not cut the line
// upsmon reads: it would not start and the NAS would not shut down in time.
func TestPasswordsWithNUTSyntax(t *testing.T) {
	m := &Manager{}
	settings := Settings{Mode: "network", Host: "nas2.local", Name: "eaton", User: "monuser", Password: `ab#c"d\e`}
	got := m.files(settings, "x")["upsmon.conf"]
	if !strings.Contains(got, `MONITOR eaton@nas2.local 1 monuser "ab#c\"d\\e" secondary`) {
		t.Fatalf("upsmon.conf:\n%s", got)
	}
	if quote("plain-Base64_value") != "plain-Base64_value" {
		t.Fatal("a plain password changed")
	}
}
