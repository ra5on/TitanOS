package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// resourceEngine is a fake engine with images, volumes and containers:
// whoami (running) and alt (stopped) use img1/img2 and volumes, a viewer
// uses the viewer image, the sandbox image is unused.
type resourceEngine struct {
	mu      sync.Mutex
	calls   []string
	images  map[string]bool // ids still there
	tags    map[string][]string
	volumes map[string]bool
	dfCalls int
	// block holds the disk usage count until it is closed.
	block chan struct{}
}

const (
	img1     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	img2     = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	imgOld   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	imgNone  = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	imgView  = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	imgSand  = "sha256:6666666666666666666666666666666666666666666666666666666666666666"
	imgMulti = "sha256:7777777777777777777777777777777777777777777777777777777777777777"
)

func resourceFixture(t *testing.T) (*Manager, *resourceEngine) {
	t.Helper()
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	e := &resourceEngine{
		images: map[string]bool{img1: true, img2: true, imgOld: true, imgNone: true, imgView: true, imgSand: true, imgMulti: true},
		tags: map[string][]string{
			img1: {"traefik/whoami:latest"}, img2: {"busybox:latest"}, imgOld: {"nginx:1.25"}, imgNone: {"<none>:<none>"},
			imgView: {"titanos-viewer:0.63.0"}, imgSand: {}, imgMulti: {"redis:7", "redis:latest"},
		},
		volumes: map[string]bool{"whoami_data": true, "old_data": true, "kept_data": true, "titanos-thing": true,
			"abababababababababababababababababababababababababababababababab": true, "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd": true},
	}
	server := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	// "kept" is an app that is saved but has no containers right now.
	if err := m.store.Write("apps/kept/compose.yaml", []byte("services: {}\n")); err != nil {
		t.Fatal(err)
	}
	m.appList = nil
	return m, e
}

func (e *resourceEngine) serve(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v1.43")
	e.calls = append(e.calls, r.Method+" "+path+"?"+r.URL.RawQuery)
	switch {
	case r.Method == "GET" && path == "/containers/json":
		w.Write([]byte(`[
		 {"Id":"aaaa","Names":["/whoami-web-1"],"ImageID":"` + img1 + `","State":"running","Labels":{"com.docker.compose.project":"whoami"},"Mounts":[{"Type":"volume","Name":"whoami_data"},{"Type":"bind","Name":""}]},
		 {"Id":"bbbb","Names":["/alt"],"ImageID":"` + img2 + `","State":"exited","Labels":{},"Mounts":[{"Type":"volume","Name":"cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"}]},
		 {"Id":"cccc","Names":["/titanos-viewer-x"],"ImageID":"` + imgView + `","State":"exited","Labels":{"titanos.viewer":"1"},"Mounts":[]}]`))
	case r.Method == "GET" && path == "/images/json":
		if r.URL.Query().Get("shared-size") != "true" {
			http.Error(w, "no shared size", 400)
			return
		}
		var parts []string
		for _, id := range []string{img1, img2, imgOld, imgNone, imgView, imgSand, imgMulti} {
			if !e.images[id] {
				continue
			}
			labels := `{}`
			if id == imgView {
				labels = `{"titanos.viewer.image":"1","titanos.viewer.release":"0.63.0"}`
			}
			digests := `[]`
			if id == imgSand {
				_, d, _ := strings.Cut(SandboxImage, "@")
				digests = `["python@` + d + `"]`
			}
			tags := `[]`
			if len(e.tags[id]) > 0 {
				tags = `["` + strings.Join(e.tags[id], `","`) + `"]`
			}
			parts = append(parts, `{"Id":"`+id+`","RepoTags":`+tags+`,"RepoDigests":`+digests+`,"Created":1700000000,"Size":1000,"SharedSize":400,"Labels":`+labels+`}`)
		}
		w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	case r.Method == "GET" && strings.HasPrefix(path, "/images/") && strings.HasSuffix(path, "/json"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/images/"), "/json")
		if !e.images[id] {
			http.Error(w, `{"message":"No such image"}`, 404)
			return
		}
		w.Write([]byte(`{"Id":"` + id + `"}`))
	case r.Method == "DELETE" && strings.HasPrefix(path, "/images/"):
		ref := strings.TrimPrefix(path, "/images/")
		if r.URL.Query().Get("force") != "" {
			http.Error(w, `{"message":"force used"}`, 500)
			return
		}
		if e.images[ref] {
			if len(e.tags[ref]) > 1 {
				w.WriteHeader(409)
				w.Write([]byte(`{"message":"image is referenced in multiple repositories"}`))
				return
			}
			delete(e.images, ref)
			w.Write([]byte(`[]`))
			return
		}
		for id, tags := range e.tags {
			if i := slices.Index(tags, ref); i >= 0 && e.images[id] {
				e.tags[id] = slices.Delete(tags, i, i+1)
				if len(e.tags[id]) == 0 {
					delete(e.images, id)
				}
				w.Write([]byte(`[]`))
				return
			}
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"No such image"}`))
	case r.Method == "GET" && path == "/volumes":
		var parts []string
		for _, name := range []string{"whoami_data", "old_data", "kept_data", "titanos-thing", strings.Repeat("ab", 32), strings.Repeat("cd", 32)} {
			if !e.volumes[name] {
				continue
			}
			labels := `{}`
			switch name {
			case "whoami_data":
				labels = `{"com.docker.compose.project":"whoami"}`
			case "old_data":
				labels = `{"com.docker.compose.project":"old"}`
			case "kept_data":
				labels = `{"com.docker.compose.project":"kept"}`
			case strings.Repeat("ab", 32), strings.Repeat("cd", 32):
				labels = `{"com.docker.volume.anonymous":""}`
			}
			parts = append(parts, `{"Name":"`+name+`","Driver":"local","Mountpoint":"/var/lib/docker/volumes/`+name+`/_data","CreatedAt":"2026-11-01T10:00:00Z","Labels":`+labels+`}`)
		}
		w.Write([]byte(`{"Volumes":[` + strings.Join(parts, ",") + `]}`))
	case r.Method == "DELETE" && strings.HasPrefix(path, "/volumes/"):
		name := strings.TrimPrefix(path, "/volumes/")
		if r.URL.Query().Get("force") != "" {
			http.Error(w, `{"message":"force used"}`, 500)
			return
		}
		delete(e.volumes, name)
		w.WriteHeader(204)
	case r.Method == "GET" && path == "/system/df":
		e.dfCalls++
		if block := e.block; block != nil {
			e.mu.Unlock()
			<-block
			e.mu.Lock()
		}
		w.Write([]byte(`{"LayersSize":5000,
		 "Images":[{"Id":"` + img1 + `","Size":1000,"SharedSize":400,"Containers":1},{"Id":"` + imgOld + `","Size":1000,"SharedSize":400,"Containers":0},
		   {"Id":"` + imgView + `","Size":3000,"SharedSize":0,"Containers":0,"Labels":{"titanos.viewer.image":"1","titanos.viewer.release":"0.63.0"}}],
		 "Containers":[{"State":"running","SizeRw":10},{"State":"exited","SizeRw":20}],
		 "Volumes":[{"Name":"whoami_data","Labels":{"com.docker.compose.project":"whoami"},"UsageData":{"Size":700,"RefCount":1}},
		   {"Name":"old_data","Labels":{"com.docker.compose.project":"old"},"UsageData":{"Size":300,"RefCount":0}},
		   {"Name":"kept_data","Labels":{"com.docker.compose.project":"kept"},"UsageData":{"Size":900,"RefCount":0}},
		   {"Name":"` + strings.Repeat("ab", 32) + `","Labels":{"com.docker.volume.anonymous":""},"UsageData":{"Size":50,"RefCount":0}},
		   {"Name":"titanos-thing","Labels":{},"UsageData":{"Size":-1,"RefCount":0}}],
		 "BuildCache":[{"Size":100,"InUse":false,"Shared":false},{"Size":40,"InUse":true},{"Size":60,"Shared":true}]}`))
	case r.Method == "POST" && path == "/build/prune":
		if r.URL.Query().Get("all") != "true" {
			http.Error(w, "all expected", 400)
			return
		}
		w.Write([]byte(`{"CachesDeleted":["a","b"],"SpaceReclaimed":4096}`))
	default:
		http.NotFound(w, r)
	}
}

func TestImagesListUsageAndProtection(t *testing.T) {
	m, _ := resourceFixture(t)
	list, err := m.Images(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Image{}
	for _, img := range list.Images {
		byID[img.ID] = img
	}
	if u := byID[img1].UsedBy; len(u) != 1 || u[0].App != "whoami" || u[0].Container != "whoami-web-1" || !u[0].Running {
		t.Errorf("img1 users: %+v", u)
	}
	if u := byID[img2].UsedBy; len(u) != 1 || u[0].Running || u[0].App != "" {
		t.Errorf("a stopped container uses img2: %+v", u)
	}
	if v := byID[imgView]; v.Protected != "viewer" || len(v.UsedBy) != 1 || v.UsedBy[0].App != "" {
		t.Errorf("viewer image: %+v", v)
	}
	if byID[imgSand].Protected != "sandbox" || !byID[imgNone].Dangling || len(byID[imgNone].RepoTags) != 0 || byID[imgOld].Dangling {
		t.Errorf("sandbox/dangling: %+v", list.Images)
	}
	// Unused and not protected: imgOld, imgNone, imgMulti, each freeing 600.
	if list.Totals.Count != 7 || list.Totals.UnusedCount != 3 || list.Totals.UnusedSize != 1800 || list.Totals.DanglingCount != 1 || list.Totals.DanglingSize != 600 {
		t.Errorf("totals: %+v", list.Totals)
	}
}

func TestRemoveImageRefusals(t *testing.T) {
	m, e := resourceFixture(t)
	ctx := context.Background()
	if _, err := m.RemoveImage(ctx, img2); !errors.Is(err, ErrInUse) {
		t.Errorf("stopped container: %v", err)
	}
	if _, err := m.RemoveImage(ctx, imgView); !errors.Is(err, ErrProtected) {
		t.Errorf("viewer: %v", err)
	}
	if _, err := m.RemoveImage(ctx, imgSand); !errors.Is(err, ErrProtected) {
		t.Errorf("sandbox: %v", err)
	}
	if _, err := m.RemoveImage(ctx, "../containers"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
	if _, err := m.RemoveImage(ctx, "999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	// Several names: removed one by one, never forced.
	if _, err := m.RemoveImage(ctx, strings.TrimPrefix(imgMulti, "sha256:")[:12]); err != nil {
		t.Fatal(err)
	}
	if e.images[imgMulti] {
		t.Error("multi-tag image still there")
	}
	// While an app is set up nothing is deleted.
	m.jobs["whoami"] = &Job{State: "running"}
	if _, err := m.RemoveImage(ctx, imgOld); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	if _, err := m.PruneImages(ctx, true); !errors.Is(err, ErrBusy) {
		t.Errorf("busy prune: %v", err)
	}
	delete(m.jobs, "whoami")
	m.applied = ""
	if _, err := m.Images(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("not running: %v", err)
	}
	m.Disabled = true
	if _, err := m.Images(ctx); !errors.Is(err, ErrDisabled) {
		t.Errorf("dev mode: %v", err)
	}
}

func TestPruneImages(t *testing.T) {
	m, e := resourceFixture(t)
	result, err := m.PruneImages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != imgNone || result.Reclaimed != 600 {
		t.Errorf("dangling only: %+v", result)
	}
	result, err = m.PruneImages(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 2 || len(result.Failed) != 0 || result.Reclaimed != 1200 {
		t.Errorf("all unused: %+v", result)
	}
	for _, id := range []string{img1, img2, imgView, imgSand} {
		if !e.images[id] {
			t.Errorf("%s was deleted", id)
		}
	}
	for _, call := range e.calls {
		if strings.Contains(call, "/images/prune") || strings.Contains(call, "force") {
			t.Errorf("engine prune or force used: %s", call)
		}
	}
}

func TestVolumes(t *testing.T) {
	m, e := resourceFixture(t)
	ctx := context.Background()
	list, err := m.Volumes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Volume{}
	for _, v := range list {
		byName[v.Name] = v
	}
	if v := byName["whoami_data"]; v.Size != 700 || len(v.UsedBy) != 1 || v.App != "whoami" || v.Anonymous || v.TitanOS {
		t.Errorf("whoami_data: %+v", v)
	}
	if v := byName[strings.Repeat("cd", 32)]; v.Size != -1 || len(v.UsedBy) != 1 || !v.Anonymous {
		t.Errorf("anonymous in use, size unknown: %+v", v)
	}
	if !byName["titanos-thing"].TitanOS || byName["titanos-thing"].Size != -1 {
		t.Errorf("titanos volume: %+v", byName["titanos-thing"])
	}
	if err := m.RemoveVolume(ctx, "whoami_data"); !errors.Is(err, ErrInUse) {
		t.Errorf("in use: %v", err)
	}
	if err := m.RemoveVolume(ctx, strings.Repeat("cd", 32)); !errors.Is(err, ErrInUse) {
		t.Errorf("stopped container: %v", err)
	}
	if err := m.RemoveVolume(ctx, "titanos-thing"); !errors.Is(err, ErrProtected) {
		t.Errorf("titanos: %v", err)
	}
	if err := m.RemoveVolume(ctx, "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad name: %v", err)
	}
	// Anonymous only (default): just the unused anonymous one.
	result, err := m.PruneVolumes(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != strings.Repeat("ab", 32) || result.Reclaimed != 50 {
		t.Errorf("anonymous prune: %+v", result)
	}
	// All unused: old_data goes, kept_data (saved app) and titanos-thing stay.
	result, err = m.PruneVolumes(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "old_data" {
		t.Errorf("all prune: %+v", result)
	}
	if !e.volumes["kept_data"] || !e.volumes["titanos-thing"] || !e.volumes["whoami_data"] {
		t.Errorf("volumes: %v", e.volumes)
	}
	// A single delete of a saved app's volume is allowed (the UI asks).
	if err := m.RemoveVolume(ctx, "kept_data"); err != nil || e.volumes["kept_data"] {
		t.Errorf("single delete: %v", err)
	}
}

func TestDiskUsageAndBuildCache(t *testing.T) {
	m, e := resourceFixture(t)
	ctx := context.Background()
	usage, err := m.DiskUsage(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Images.Size != 5000 || usage.Images.Reclaimable != 600 {
		t.Errorf("images: %+v", usage.Images)
	}
	if usage.Containers.Size != 30 || usage.Containers.Reclaimable != 20 {
		t.Errorf("containers: %+v", usage.Containers)
	}
	// old_data 300 + anonymous 50; kept_data belongs to a saved app.
	if usage.Volumes.Size != 1950 || usage.Volumes.Reclaimable != 350 || usage.AnonymousReclaimable != 50 {
		t.Errorf("volumes: %+v anon %d", usage.Volumes, usage.AnonymousReclaimable)
	}
	if usage.BuildCache.Size != 200 || usage.BuildCache.Reclaimable != 100 || usage.Reclaimable != 1050 {
		t.Errorf("build cache/total: %+v %d", usage.BuildCache, usage.Reclaimable)
	}
	if _, err := m.DiskUsage(ctx, false); err != nil || e.dfCalls != 1 {
		t.Errorf("cached count asked again: %d", e.dfCalls)
	}
	result, err := m.PruneBuildCache(ctx)
	if err != nil || result.Reclaimed != 4096 || len(result.Deleted) != 2 {
		t.Fatalf("build prune: %+v %v", result, err)
	}
	if _, err := m.DiskUsage(ctx, false); err != nil || e.dfCalls != 2 {
		t.Errorf("count not renewed after a change: %d", e.dfCalls)
	}
}

func TestSlowDiskUsageDoesNotHoldUp(t *testing.T) {
	m, e := resourceFixture(t)
	ctx := context.Background()
	old := dfWaitList
	dfWaitList = 50 * time.Millisecond
	t.Cleanup(func() { dfWaitList = old })
	e.mu.Lock()
	e.block = make(chan struct{})
	e.mu.Unlock()
	start := time.Now()
	list, err := m.Volumes(ctx)
	if err != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("volume list waited for the count: %v %s", err, time.Since(start))
	}
	for _, v := range list {
		if v.Size != -1 {
			t.Errorf("size before the count: %+v", v)
		}
	}
	// A delete is not held up by the running count, and only one runs.
	if err := m.RemoveVolume(ctx, "old_data"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Volumes(ctx); err != nil {
		t.Fatal(err)
	}
	oldPage := dfWaitPage
	dfWaitPage = 50 * time.Millisecond
	t.Cleanup(func() { dfWaitPage = oldPage })
	if usage, err := m.DiskUsage(ctx, false); err != nil || !usage.Measuring || !usage.MeasuredAt.IsZero() {
		t.Errorf("page while counting: %+v %v", usage, err)
	}
	e.mu.Lock()
	calls := e.dfCalls
	close(e.block)
	e.block = nil
	e.mu.Unlock()
	if calls != 1 {
		t.Errorf("counts started: %d", calls)
	}
	// The count started before the delete is not kept; the next one is.
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, _ = m.Volumes(ctx)
		i := slices.IndexFunc(list, func(v Volume) bool { return v.Name == "whoami_data" })
		if i >= 0 && list[i].Size == 700 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no sizes after the count: %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if usage, err := m.DiskUsage(ctx, false); err != nil || usage.Measuring || usage.MeasuredAt.IsZero() {
		t.Errorf("usage: %+v %v", usage, err)
	}
}
