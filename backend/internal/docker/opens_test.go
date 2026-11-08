package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const bambuCompose = `x-titanos:
  name: Bambu Studio
  main: bambustudio
  opens:
    types: [3MF, .stl, step]
    command: [bambu-studio, "{file}"]
    user: abc
    env: [DISPLAY=:1]
services:
  bambustudio:
    image: lscr.io/linuxserver/bambustudio:latest
    # Konfiguration, Projekte, Druckprofile
    volumes:
      - ./config:/config
`

func TestParseOpener(t *testing.T) {
	op, name, err := parseOpener(bambuCompose)
	if err != nil || name != "Bambu Studio" {
		t.Fatal(err, name)
	}
	if !slices.Equal(op.Types, []string{"3mf", "stl", "step"}) || op.Service != "bambustudio" || op.User != "abc" {
		t.Fatalf("%+v", op)
	}
	if !op.Opens("3MF") || op.Opens("docx") {
		t.Fatal("types")
	}
	if got := op.argv("/shares/Documents/a b.3mf"); !slices.Equal(got, []string{"bambu-studio", "/shares/Documents/a b.3mf"}) {
		t.Fatalf("argv %q", got)
	}
	// Without opens: no opener, no error.
	if op, _, err := parseOpener("services:\n  a:\n    image: x\n"); op != nil || err != nil {
		t.Fatal(op, err)
	}
	// The only service is the default without x-titanos.main.
	if op, _, err := parseOpener("x-titanos:\n  opens:\n    types: [stl]\n    command: [p, \"{file}\"]\nservices:\n  a:\n    image: x\n"); err != nil || op.Service != "a" {
		t.Fatal(op, err)
	}
}

func TestOpenerValidateRefusesShellAndOddValues(t *testing.T) {
	for name, op := range map[string]Opener{
		"no types":           {Command: []string{"p", "{file}"}},
		"dot type":           {Types: []string{"a.b"}, Command: []string{"p", "{file}"}},
		"no placeholder":     {Types: []string{"stl"}, Command: []string{"p", "x"}},
		"twice":              {Types: []string{"stl"}, Command: []string{"p", "{file}", "{file}"}},
		"inside an argument": {Types: []string{"stl"}, Command: []string{"sh", "-c", "open {file}"}},
		"as program":         {Types: []string{"stl"}, Command: []string{"{file}", "x"}},
		"newline":            {Types: []string{"stl"}, Command: []string{"p\nrm", "{file}"}},
		"user":               {Types: []string{"stl"}, Command: []string{"p", "{file}"}, User: "root;id"},
		"env":                {Types: []string{"stl"}, Command: []string{"p", "{file}"}, Env: []string{"no value"}},
	} {
		if err := op.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := Opener{Types: []string{"stl"}, Command: []string{"p", "--open", "{file}"}, User: "1000:1000", Env: []string{"WAYLAND_DISPLAY=wayland-1"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestContainerPath(t *testing.T) {
	mounts := []Mount{
		{Source: "/mnt/Data/appdata/bambu/config", Target: "/config"},
		{Source: "/mnt/Data/Documents", Target: "/shares/Documents"},
		{Source: "/mnt/Data/Documents/Druck", Target: "/druck", ReadOnly: true},
	}
	cases := map[string]string{
		"/mnt/Data/Documents/a.3mf":             "/shares/Documents/a.3mf",
		"/mnt/Data/Documents/x/../b.stl":        "/shares/Documents/b.stl",
		"/mnt/Data/Documents/Druck/teil.stl":    "/druck/teil.stl", // the longest source wins
		"/mnt/Data/DocumentsOther/c.stl":        "",
		"/mnt/Data/Documents":                   "", // the folder itself is no file
		"/mnt/Data/Projects/d.stl":              "",
		"relative/a.stl":                        "",
		"/mnt/Data/Documents/../Projects/e.3mf": "",
	}
	for host, want := range cases {
		got, _, ok := ContainerPath(mounts, host)
		if (want == "") == ok || got != want {
			t.Errorf("%s: %q %v, want %q", host, got, ok, want)
		}
	}
}

func TestEditVolumesKeepsCommentsAndRefusesDoubleTargets(t *testing.T) {
	add := func(list *yaml.Node) error {
		for _, item := range list.Content {
			if volumeTarget(item) == "/shares/Documents" {
				return ErrExists
			}
		}
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "/mnt/Data/Documents:/shares/Documents:ro"})
		return nil
	}
	out, err := editVolumes(bambuCompose, "bambustudio", add)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/mnt/Data/Documents:/shares/Documents:ro") || !strings.Contains(out, "# Konfiguration, Projekte") ||
		!strings.Contains(out, "./config:/config") || !strings.Contains(out, "opens:") {
		t.Fatalf("compose:\n%s", out)
	}
	if _, err := editVolumes(out, "bambustudio", add); !errors.Is(err, ErrExists) {
		t.Fatal("mounted twice", err)
	}
	// A service without volumes gets the list.
	out, err = editVolumes("services:\n  a:\n    image: x\n", "a", add)
	if err != nil || !strings.Contains(out, "volumes:") {
		t.Fatal(out, err)
	}
	if _, err := editVolumes(bambuCompose, "other", add); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown service", err)
	}
	// Long form entries count by their target.
	long := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "type"}, {Kind: yaml.ScalarNode, Value: "bind"},
		{Kind: yaml.ScalarNode, Value: "target"}, {Kind: yaml.ScalarNode, Value: "/shares/Documents/"},
	}}
	if volumeTarget(long) != "/shares/Documents" {
		t.Fatal(volumeTarget(long))
	}
}

// openEngine is a Docker Engine with the container of the Bambu Studio app.
func openEngine(t *testing.T, running *bool, mounts string) (string, *[]string) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		state := "exited"
		if *running {
			state = "running"
		}
		fmt.Fprintf(w, `[{"Id":"bbbbbbbbbbbb1111","Names":["/bambu-bambustudio-1"],"State":%q,"Labels":{"com.docker.compose.project":"bambu","com.docker.compose.service":"bambustudio"}}]`, state)
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"State":{"Running":%v},"Mounts":%s}`, *running, mounts)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/exec", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		note("exec " + r.PathValue("id") + " " + string(body))
		w.Write([]byte(`{"Id":"ex1"}`))
	})
	mux.HandleFunc("POST /v1.43/exec/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		note("start " + r.PathValue("id") + " " + string(body))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		note("container start " + r.PathValue("id"))
		*running = true
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return socket, &log
}

func TestOpenFileRunsTheProgramWithThePathInTheContainer(t *testing.T) {
	m, _, _ := newTestManager(t)
	running := true
	socket, log := openEngine(t, &running, `[{"Type":"bind","Source":"/mnt/Data/appdata/bambu/config","Destination":"/config","RW":true},{"Type":"bind","Source":"/mnt/Data/Documents","Destination":"/shares/Documents","RW":true}]`)
	m.api = newClient(socket)
	m.store.Write("apps/bambu/compose.yaml", []byte(bambuCompose))
	ctx := context.Background()

	list, err := m.Openers(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Bambu Studio" || !list[0].Running || len(list[0].Mounts) != 2 || !list[0].Opens("stl") {
		t.Fatalf("%+v %v", list, err)
	}
	if err := m.OpenFile(ctx, "bambu", "/mnt/Data/Documents/Druck/Gehäuse Deckel.3mf"); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(*log, "\n")
	for _, want := range []string{`"Cmd":["bambu-studio","/shares/Documents/Druck/Gehäuse Deckel.3mf"]`, `"User":"abc"`, `"Env":["DISPLAY=:1"]`, `"Tty":false`, `start ex1 {"Detach":true`} {
		if !strings.Contains(all, want) {
			t.Errorf("%s missing in\n%s", want, all)
		}
	}
	if err := m.OpenFile(ctx, "bambu", "/mnt/Data/Projects/teil.stl"); !errors.Is(err, ErrNotMounted) {
		t.Errorf("outside the mounts: %v", err)
	}
	if err := m.OpenFile(ctx, "bambu", "/mnt/Data/Documents/brief.docx"); !errors.Is(err, ErrNoOpener) {
		t.Errorf("other type: %v", err)
	}
	if err := m.OpenFile(ctx, "../x", "/mnt/Data/Documents/a.stl"); !errors.Is(err, ErrNotFound) {
		t.Errorf("odd app name: %v", err)
	}
	// A stopped app is started first.
	defer func(d time.Duration) { openStartAfter = d }(openStartAfter)
	openStartAfter = 0
	running = false
	*log = nil
	if err := m.OpenFile(ctx, "bambu", "/mnt/Data/Documents/a.stl"); err != nil {
		t.Fatal(err)
	}
	if len(*log) < 2 || (*log)[0] != "container start bbbbbbbbbbbb1111" || !strings.HasPrefix((*log)[1], "exec bbbbbbbbbbbb1111") {
		t.Errorf("stopped app: %q", *log)
	}
}

func TestMountShareChangesTheComposeFileOnce(t *testing.T) {
	m, _, _ := newTestManager(t)
	running := true
	socket, _ := openEngine(t, &running, `[]`)
	m.api = newClient(socket)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.store.Write("apps/bambu/compose.yaml", []byte(bambuCompose))
	ctx := context.Background()

	if err := m.MountShare(ctx, "bambu", "Documents", "relative/path", false); !errors.Is(err, ErrInvalid) {
		t.Errorf("relative source: %v", err)
	}
	if err := m.MountShare(ctx, "bambu", "../etc", "/mnt/Data/x", false); !errors.Is(err, ErrInvalid) {
		t.Errorf("odd share name: %v", err)
	}
	if err := m.MountShare(ctx, "bambu", "Documents", "/mnt/Data/Documents", true); err != nil {
		t.Fatal(err)
	}
	stored, _ := m.Compose("bambu")
	if !strings.Contains(stored, "- /mnt/Data/Documents:/shares/Documents:ro") || !strings.Contains(stored, "# Konfiguration, Projekte") {
		t.Fatalf("compose:\n%s", stored)
	}
	// The cache follows the new file (no stale opens block).
	if e := m.opener("bambu"); e.op == nil || e.err != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	delete(m.jobs, "bambu")
	m.mu.Unlock()
	if err := m.MountShare(ctx, "bambu", "Documents", "/mnt/Data/Documents", false); !errors.Is(err, ErrMounted) {
		t.Errorf("twice: %v", err)
	}
	if err := m.UnmountShare(ctx, "bambu", "Documents"); err != nil {
		t.Fatal(err)
	}
	if stored, _ := m.Compose("bambu"); strings.Contains(stored, "/shares/Documents") || !strings.Contains(stored, "./config:/config") {
		t.Fatalf("after removing:\n%s", stored)
	}
	m.mu.Lock()
	delete(m.jobs, "bambu")
	m.mu.Unlock()
	if err := m.UnmountShare(ctx, "bambu", "Documents"); !errors.Is(err, ErrNotFound) {
		t.Errorf("not mounted: %v", err)
	}
}

func TestSetOpenerWritesTheTemplateBlock(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.store.Write("apps/bambu/compose.yaml", []byte(bambuCompose))
	ctx := context.Background()
	op := Opener{Types: []string{".STL", "obj"}, Command: []string{"bambu-studio", "{file}"}, Service: "bambustudio", Hidden: true}
	if err := m.SetOpener(ctx, "bambu", op); err != nil {
		t.Fatal(err)
	}
	e := m.opener("bambu")
	if e.err != nil || !slices.Equal(e.op.Types, []string{"stl", "obj"}) || !e.op.Hidden || e.op.User != "" || e.name != "Bambu Studio" {
		t.Fatalf("%+v %v", e.op, e.err)
	}
	m.mu.Lock()
	delete(m.jobs, "bambu")
	m.mu.Unlock()
	bad := Opener{Types: []string{"stl"}, Command: []string{"sh", "-c", "x {file}"}, Service: "bambustudio"}
	if err := m.SetOpener(ctx, "bambu", bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("shell line: %v", err)
	}
	if err := m.SetOpener(ctx, "bambu", Opener{Types: []string{"stl"}, Command: []string{"p", "{file}"}, Service: "nope"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown service: %v", err)
	}
}
