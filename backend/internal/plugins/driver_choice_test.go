package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestChooseDriverQueuesAndStores(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	m.LocalBuilds = true
	for _, bad := range []string{"latest", "580.105.08; rm", "../1", "580"} {
		if err := m.ChooseDriver(bad, []string{"data"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := m.ChooseDriver("580.105.08", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if m.current().NvidiaDriver != "580.105.08" || len(f.runner.commands) != 0 {
		t.Fatalf("stored %q, ran %v", m.current().NvidiaDriver, f.runner.commands)
	}
	// One build at a time; the stored choice stays.
	if err := m.ChooseDriver("590.44.01", []string{"data"}); !errors.Is(err, ErrBusy) || m.current().NvidiaDriver != "580.105.08" {
		t.Fatalf("second choice: %v, %q", err, m.current().NvidiaDriver)
	}
	if request := <-m.driverRequests; request != "choose:580.105.08" {
		t.Fatal(request)
	}
	// Back to the shipped driver, which runs already: nothing to select.
	if err := m.ChooseDriver("", []string{"data"}); err != nil || m.current().NvidiaDriver != "" || m.current().PendingDriver != nil {
		t.Fatalf("shipped: %v %+v", err, m.current())
	}
	if err := m.ChooseDriver("580.105.08", []string{"other"}); !errors.Is(err, ErrUnknownVolume) {
		t.Errorf("unmounted volume: %v", err)
	}
	m.LocalBuilds = false
	if err := m.ChooseDriver("580.105.08", []string{"data"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("without local builds: %v", err)
	}
}

func TestBuildCommandNamesTheChosenDriver(t *testing.T) {
	if args := strings.Join(buildCommand("/r", "check", "1.0.0", "580.105.08"), "\n"); !strings.Contains(args, "--setenv=TITANOS_NVIDIA_DRIVER=580.105.08") {
		t.Errorf("chosen driver missing: %s", args)
	}
	if args := strings.Join(buildCommand("/r", "check", "1.0.0", ""), "\n"); strings.Contains(args, "TITANOS_NVIDIA_DRIVER") {
		t.Errorf("newest driver named: %s", args)
	}
}

func TestReadDriversKeepsOnlyVersions(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	root := sandboxRoot(t)
	m.readDrivers(root)
	if len(m.driverSnapshot(m.current()).Drivers) != 0 {
		t.Fatal("drivers without versions.json")
	}
	os.WriteFile(filepath.Join(root, "work/versions.json"), []byte(`{"drivers":["615.71.09","590.44.01","x; rm","590.44.01","../1"]}`), 0o600)
	m.readDrivers(root)
	if got := m.driverSnapshot(m.current()).Drivers; !slices.Equal(got, []string{"615.71.09", "590.44.01"}) {
		t.Errorf("drivers: %v", got)
	}
}

func TestChosenDriverIsRebuiltOncePerRelease(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	m.LocalBuilds = true
	var built []string
	m.DriverBuilt = func(version string, err error) {
		if err == nil {
			t.Error("build without a build environment succeeded")
		}
		built = append(built, version)
	}
	settings := m.current()
	settings.NvidiaDriver = "580.105.08"
	if err := m.save(settings); err != nil {
		t.Fatal(err)
	}
	// The shipped driver runs, the chosen one is not built for this release:
	// one attempt, which fails here (no build environment), then no repeat.
	m.rebuildChoice(context.Background(), []string{"data"})
	m.rebuildChoice(context.Background(), []string{"data"})
	if !slices.Equal(built, []string{"580.105.08"}) {
		t.Fatalf("builds: %v", built)
	}
	status := m.driverSnapshot(m.current())
	if status.Target != "580.105.08" || status.State != "error" || status.Chosen != "580.105.08" {
		t.Errorf("status: %+v", status)
	}
	// A new kernel tries again.
	m.kernel = "6.12.117+deb13-amd64"
	m.rebuildChoice(context.Background(), []string{"data"})
	if len(built) != 2 {
		t.Errorf("after an update: %v", built)
	}
	// Nothing chosen: nothing built.
	settings.NvidiaDriver = ""
	m.save(settings)
	m.kernel = "6.12.118+deb13-amd64"
	m.rebuildChoice(context.Background(), []string{"data"})
	if len(built) != 2 {
		t.Errorf("without a choice: %v", built)
	}
}
