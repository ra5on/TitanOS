package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// fakeSource stands in for fanotify: the test sends the events.
type fakeSource struct{ ch chan protect.Event }

func (f *fakeSource) Watch(ctx context.Context, _ []string, out chan<- protect.Event) error {
	for {
		select {
		case e := <-f.ch:
			out <- e
		case <-ctx.Done():
			return nil
		}
	}
}

// protectRunner records commands; smbstatus names lena's PC, a btrfs
// snapshot of a snapshot is a copy of the folder.
type protectRunner struct {
	mu    sync.Mutex
	calls []string
}

func (r *protectRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	r.mu.Unlock()
	switch {
	case name == "smbstatus":
		return `{"sessions":{"1":{"server_id":{"pid":"4242"},"username":"lena","remote_machine":"PC-Wohnzimmer","hostname":"ipv4:192.168.1.57:50000"}}}`, nil
	case name == "btrfs" && len(args) == 5 && args[1] == "snapshot" && strings.Contains(args[3], "/snapshots/"):
		return "", os.CopyFS(args[4], os.DirFS(args[3]))
	case name == "btrfs" && len(args) == 5 && args[1] == "snapshot":
		// A snapshot of the volume: the share as it is now.
		return "", os.CopyFS(filepath.Join(args[4], "Documents"), os.DirFS(filepath.Join(args[3], "Documents")))
	case name == "lsblk":
		return "", errors.New("no lsblk in tests")
	}
	return "", nil
}

func (r *protectRunner) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type protectEnv struct {
	ts     *testServer
	mnt    string
	src    *fakeSource
	runner *protectRunner
	now    time.Time
	mu     sync.Mutex
}

func (e *protectEnv) clock() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now }
func (e *protectEnv) set(t time.Time)  { e.mu.Lock(); e.now = t; e.mu.Unlock() }

// protectServer: an agent server with a Btrfs volume "fast" and an ext4
// volume "tank" below a temporary folder, a share Documents on fast.
func protectServer(t *testing.T) *protectEnv {
	t.Helper()
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	mnt := t.TempDir()
	if err := ts.store.WriteJSON("storage/volumes.json", []storage.VolumeConfig{
		{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: "a", FilesystemUUID: "b"},
		{Name: "fast", Level: "raid1", Filesystem: "btrfs", ArrayUUID: "c", FilesystemUUID: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ts.disks.Load(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"tank", "fast"} {
		os.MkdirAll(filepath.Join(mnt, v), 0o755)
	}
	ts.disks.UseMounts(mnt, func() map[string]bool { return map[string]bool{mnt + "/tank": true, mnt + "/fast": true} })
	ts.shares.UseMountRoot(mnt)
	runner := &protectRunner{}
	ts.disks.UseRunner(runner)
	for _, u := range []string{"lena", "max"} {
		ts.users.Create(u, testPassword, auth.RoleUser)
	}
	for _, sh := range []map[string]any{
		{"name": "Documents", "volume": "fast", "smb": true, "access": map[string]string{"alex": "rw", "lena": "rw", "max": "ro"}},
		{"name": "Fotos", "volume": "tank", "smb": true, "access": map[string]string{"alex": "rw", "max": "rw"}},
	} {
		if res, body := ts.request("POST", "/api/v1/shares", sh); res.StatusCode != http.StatusCreated {
			t.Fatalf("share: %d %s", res.StatusCode, body)
		}
	}
	env := &protectEnv{ts: ts, mnt: mnt, src: &fakeSource{ch: make(chan protect.Event, 100)}, runner: runner, now: time.Date(2026, 11, 6, 10, 42, 0, 0, time.Local)}
	ps := ts.server.protect()
	ps.source, ps.runner, ps.now = env.src, runner, env.clock
	t.Cleanup(func() { ts.server.guardStop(); time.Sleep(50 * time.Millisecond) })
	return env
}

func (e *protectEnv) features(t *testing.T, f map[string]any) {
	t.Helper()
	if res, body := e.ts.request("PUT", "/api/v1/agent/features", f); res.StatusCode != http.StatusOK {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
	time.Sleep(80 * time.Millisecond) // the reconcile after a change
}

func (e *protectEnv) login(t *testing.T, name string) *http.Client {
	t.Helper()
	c := e.ts.newClient()
	if res, _ := e.ts.do(c, "POST", "/api/v1/login", credentials{Name: name, Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d", name, res.StatusCode)
	}
	return c
}

func (e *protectEnv) access(name string) map[string]string {
	sh, _ := e.ts.server.shareByName(name)
	return sh.Access
}

func TestProtectOffByDefault(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	_, body := ts.request("GET", "/api/v1/agent/features", nil)
	var f agentFeatures
	json.Unmarshal(body, &f)
	if f.Guard.Enabled || f.Handbook.Enabled || f.Emergency.Enabled || f.Handbook.Probes || f.Handbook.WithContacts || f.Emergency.Code {
		t.Fatalf("on by default: %s", body)
	}
	if f.Guard.Mode != guardLock || f.Guard.Known != 5 || f.Guard.Renames != 30 || f.Guard.WindowSec != 60 {
		t.Fatalf("defaults not shown: %+v", f.Guard)
	}
	_, body = ts.request("GET", "/api/v1/agent/protect/me", nil)
	if !strings.Contains(string(body), `"emergency":false`) || !strings.Contains(string(body), `"guard":false`) {
		t.Fatalf("me: %s", body)
	}
	// The tick does nothing while off: no watcher.
	ts.server.agentProtectTick(env.clock())
	if ts.server.protect().watchCancel != nil {
		t.Fatal("watching while off")
	}
	lena := env.login(t, "lena")
	for _, p := range []string{"/api/v1/agent/protect/guard", "/api/v1/agent/protect/emergency"} {
		if res, _ := ts.do(lena, "GET", p, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s for a user: %d", p, res.StatusCode)
		}
	}
	if res, _ := ts.do(lena, "PUT", "/api/v1/agent/features", map[string]any{"guard": map[string]any{"enabled": true}}); res.StatusCode != http.StatusForbidden {
		t.Fatal("a user switched the guard on")
	}
	if res, _ := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x"}); res.StatusCode == http.StatusCreated {
		t.Fatal("request while off")
	}
	// Bad settings.
	for _, bad := range []map[string]any{
		{"guard": map[string]any{"mode": "delete-everything"}},
		{"guard": map[string]any{"exclude": []string{"Nirgends"}}},
		{"emergency": map[string]any{"persons": []map[string]any{{"account": "fremd", "shares": []string{"Documents"}}}}},
		{"emergency": map[string]any{"persons": []map[string]any{{"account": "lena", "shares": []string{}}}}},
		{"emergency": map[string]any{"persons": []map[string]any{{"account": "lena", "waitDays": 90, "shares": []string{"Documents"}}}}},
	} {
		if res, _ := ts.request("PUT", "/api/v1/agent/features", bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("accepted %v: %d", bad, res.StatusCode)
		}
	}
}

// fixture: 40 files of Documents were renamed to .locked by lena's PC; the
// snapshot from 10:30 has the originals.
func (e *protectEnv) attack(t *testing.T, n int, snapshot bool) {
	t.Helper()
	e.attackFiles(t, n, snapshot)
	e.attackEvents(n)
}

func (e *protectEnv) attackFiles(t *testing.T, n int, snapshot bool) {
	t.Helper()
	share := filepath.Join(e.mnt, "fast", "Documents")
	snap := filepath.Join(e.mnt, "fast", ".titanos", "snapshots", "agent-20261106-103000", "Documents")
	for i := range n {
		rel := fmt.Sprintf("Post/Brief%02d.pdf", i)
		os.MkdirAll(filepath.Join(share, "Post"), 0o755)
		os.WriteFile(filepath.Join(share, rel+".locked"), []byte("ENCRYPTED"), 0o644)
		if snapshot {
			os.MkdirAll(filepath.Join(snap, "Post"), 0o755)
			os.WriteFile(filepath.Join(snap, rel), []byte("original "+rel), 0o644)
		}
	}
	os.Chmod(filepath.Join(e.mnt, "fast", ".titanos"), 0o700)
	os.Chmod(filepath.Join(e.mnt, "fast", ".titanos", "snapshots"), 0o700)
}

func (e *protectEnv) attackEvents(n int) {
	share := filepath.Join(e.mnt, "fast", "Documents")
	for i := range n {
		rel := fmt.Sprintf("Post/Brief%02d.pdf", i)
		e.src.ch <- protect.Event{At: e.clock().Add(time.Duration(i) * 100 * time.Millisecond), Op: protect.OpRename, From: filepath.Join(share, rel), Path: filepath.Join(share, rel+".locked"), PID: 4242}
	}
}

func (e *protectEnv) waitAlert(t *testing.T) guardAlert {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ps := e.ts.server.protect()
		ps.mu.Lock()
		s := e.ts.server
		s.guardLoadLocked(ps)
		var a *guardAlert
		if len(ps.alerts) > 0 {
			x := ps.alerts[len(ps.alerts)-1]
			a = &x
		}
		ps.mu.Unlock()
		if a != nil {
			return *a
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no alert")
	return guardAlert{}
}

// sperren und melden: the share becomes read-only, lena's PC is cut off,
// the snapshot from before is held; after the attack the files come back
// from it, the .locked files go to the recycle bin; reopening gives back
// exactly the access the guard took.
func TestGuardLockRestoreReopen(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{"guard": map[string]any{"enabled": true}})
	ts.server.agentProtectTick(env.clock())
	// A harmless write 10 s before the attack takes the hourly snapshot;
	// the files are already encrypted on disk when it is copied.
	share0 := filepath.Join(env.mnt, "fast", "Documents")
	os.MkdirAll(share0, 0o755)
	env.attackFiles(t, 40, true)
	env.src.ch <- protect.Event{At: env.clock().Add(-10 * time.Second), Op: protect.OpWrite, Path: filepath.Join(share0, "notiz.bin"), PID: 1}
	pre := filepath.Join(env.mnt, "fast", ".titanos", "snapshots", "guard-pre-"+env.clock().Add(-10*time.Second).UTC().Format("20060102-150405"))
	deadline0 := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline0) {
		if _, err := os.Stat(pre); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(pre); err != nil {
		t.Fatalf("no pre-snapshot: %v %v", err, env.runner.calls)
	}
	env.attackEvents(40)
	a := env.waitAlert(t)
	if a.Share != "Documents" || a.Reason != "known-extension" || !a.Locked || a.Client.User != "lena" || a.Client.IP != "192.168.1.57" || !a.Btrfs {
		t.Fatalf("alert: %+v", a)
	}
	if a.Snapshot != "agent-20261106-103000" || a.Held == "" {
		t.Fatalf("snapshot: %q held %q", a.Snapshot, a.Held)
	}
	acc := env.access("Documents")
	if acc["alex"] != shares.ReadOnly || acc["lena"] != shares.ReadOnly || acc["max"] != shares.ReadOnly {
		t.Fatalf("not locked: %v", acc)
	}
	if !env.runner.has("smbcontrol smbd close-share Documents") || !env.runner.has("smbcontrol smbd kill-client-ip 192.168.1.57") {
		t.Fatalf("not disconnected: %v", env.runner.calls)
	}
	if env.access("Fotos")["max"] != shares.ReadWrite {
		t.Fatal("another share locked")
	}
	// The attack's own events took no snapshot.
	if n := strings.Count(strings.Join(env.runner.calls, "\n"), "guard-pre-"); n != 1 {
		t.Fatalf("pre-snapshots: %d %v", n, env.runner.calls)
	}
	// While it goes on, the alert collects the files; restoring waits.
	time.Sleep(100 * time.Millisecond)
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/restore", nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("restore during the attack: %d %s", res.StatusCode, body)
	}
	ts.server.agentProtectTick(env.clock().Add(time.Minute))
	_, body := ts.request("GET", "/api/v1/agent/protect/guard", nil)
	var st struct {
		Watching bool         `json:"watching"`
		Alerts   []guardAlert `json:"alerts"`
	}
	json.Unmarshal(body, &st)
	if !st.Watching || len(st.Alerts) != 1 || st.Alerts[0].Count != 40 || len(st.Alerts[0].Files) != 40 {
		t.Fatalf("status: %s", body)
	}
	// An administrator takes lena out of the share during the lock.
	sh, _ := ts.server.shareByName("Documents")
	delete(sh.Access, "lena")
	ts.server.updateShare(sh)

	env.set(env.clock().Add(5 * time.Minute))
	ts.server.agentProtectTick(env.clock())
	if a, _ = ts.server.guardAlert(a.ID); a.State != "stopped" {
		t.Fatalf("not stopped: %s", a.State)
	}
	res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/restore", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("restore: %d %s", res.StatusCode, body)
	}
	json.Unmarshal(body, &a)
	if a.Restored == nil || a.Restored.Restored != 40 || a.Restored.Recycled != 40 || a.State != "restored" {
		t.Fatalf("restored: %+v", a.Restored)
	}
	share := filepath.Join(env.mnt, "fast", "Documents")
	if b, _ := os.ReadFile(filepath.Join(share, "Post/Brief07.pdf")); string(b) != "original Post/Brief07.pdf" {
		t.Fatalf("file: %q", b)
	}
	if _, err := os.Stat(filepath.Join(share, "Post/Brief07.pdf.locked")); err == nil {
		t.Fatal(".locked file stayed")
	}
	matches, _ := filepath.Glob(filepath.Join(share, ".recycle", "titanos-guard", "*", "Post", "Brief07.pdf.locked"))
	if len(matches) != 1 {
		t.Fatal(".locked file not in the recycle bin")
	}
	// Still read-only until reopened.
	if env.access("Documents")["alex"] != shares.ReadOnly {
		t.Fatal("reopened by the restore")
	}
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/reopen", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("reopen: %d %s", res.StatusCode, body)
	}
	acc = env.access("Documents")
	if acc["alex"] != shares.ReadWrite || acc["max"] != shares.ReadOnly {
		t.Fatalf("access after reopen: %v", acc)
	}
	if _, back := acc["lena"]; back {
		t.Fatal("reopen gave back what an administrator took")
	}
	// The guard's state survives a restart of titand.
	protectStates.Delete(ts.server)
	if a, ok := ts.server.guardAlert(a.ID); !ok || a.Restored == nil {
		t.Fatal("alert lost after restart")
	}
	t.Cleanup(func() { protectStates.Delete(ts.server) })
}

// nur melden: nothing is locked. Fehlalarm on a locked share reopens it,
// restores nothing and mutes that device for an hour.
func TestGuardReportAndFalseAlarm(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{"guard": map[string]any{"enabled": true, "mode": "report"}})
	ts.server.agentProtectTick(env.clock())
	env.attack(t, 10, false)
	a := env.waitAlert(t)
	if a.Locked || env.access("Documents")["alex"] != shares.ReadWrite || env.runner.has("smbcontrol") {
		t.Fatalf("report mode locked: %+v", a)
	}
	ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/dismiss", nil)
	// The share is quiet for an hour after a false alarm.
	env.set(env.clock().Add(61 * time.Minute))

	env.features(t, map[string]any{"guard": map[string]any{"enabled": true}})
	ts.server.agentProtectTick(env.clock())
	share := filepath.Join(env.mnt, "fast", "Documents")
	for i := range 10 {
		rel := fmt.Sprintf("x%02d.docx", i)
		env.src.ch <- protect.Event{At: env.clock().Add(2*time.Minute + time.Duration(i)*time.Second), Op: protect.OpRename, From: filepath.Join(share, rel), Path: filepath.Join(share, rel+".locked"), PID: 77}
	}
	deadline := time.Now().Add(3 * time.Second)
	var b guardAlert
	for time.Now().Before(deadline) {
		if b = env.waitAlert(t); b.ID != a.ID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b.ID == a.ID || !b.Locked {
		t.Fatalf("second alert: %+v", b)
	}
	// Without a snapshot from before, there is nothing to restore from.
	ts.server.agentProtectTick(env.clock().Add(10 * time.Minute))
	if res, _ := ts.request("POST", "/api/v1/agent/protect/guard/"+b.ID+"/restore", nil); res.StatusCode != http.StatusConflict {
		t.Fatal("restore without snapshot")
	}
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+b.ID+"/dismiss", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("dismiss: %d %s", res.StatusCode, body)
	}
	if env.access("Documents")["alex"] != shares.ReadWrite || env.access("Documents")["lena"] != shares.ReadWrite {
		t.Fatal("not reopened")
	}
	b, _ = ts.server.guardAlert(b.ID)
	if b.State != "dismissed" {
		t.Fatalf("state: %s", b.State)
	}
	// The device again within the hour (reconnected: a new process): no alarm.
	before := len(ts.server.guardStatusFor(20)["alerts"].([]guardAlert))
	for i := range 10 {
		rel := fmt.Sprintf("y%02d.docx", i)
		env.src.ch <- protect.Event{At: env.clock().Add(12*time.Minute + time.Duration(i)*time.Second), Op: protect.OpRename, From: filepath.Join(share, rel), Path: filepath.Join(share, rel+".locked"), PID: 78}
	}
	time.Sleep(300 * time.Millisecond)
	if after := len(ts.server.guardStatusFor(20)["alerts"].([]guardAlert)); after != before {
		t.Fatal("muted device alarmed again")
	}
	// Writes of titand itself (.titanos, recycle bin) and of excluded
	// shares are never judged.
	if _, _, ok := ts.server.guardShareOf(filepath.Join(share, ".recycle/x.locked"), agentGuard{}); ok {
		t.Fatal("recycle bin judged")
	}
	if _, _, ok := ts.server.guardShareOf(filepath.Join(share, "a.txt"), agentGuard{Exclude: []string{"Documents"}}); ok {
		t.Fatal("excluded share judged")
	}
}

// Notfallzugang: only registered persons ask; the waiting time holds
// across a restart with a fake clock; the grant is read-only, never more
// than the person had, and ends after 30 days; a decline is final.
func TestEmergencyAccess(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{
		"handbook":  map[string]any{"enabled": true},
		"emergency": map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "waitDays": 3, "shares": []string{"Documents", "Fotos"}}}},
	})
	lena, maxc := env.login(t, "lena"), env.login(t, "max")
	if res, _ := ts.do(maxc, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x"}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("not registered asked: %d", res.StatusCode)
	}
	if res, _ := ts.do(lena, "GET", "/api/v1/agent/protect/handbook", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("handbook before the grant")
	}
	res, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "Papa ist im Krankenhaus."})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("request: %d %s", res.StatusCode, body)
	}
	var req protect.Request
	json.Unmarshal(body, &req)
	if !req.Due.Equal(env.clock().UTC().Add(72 * time.Hour)) {
		t.Fatalf("due: %v", req.Due)
	}
	// lena cannot decide on her own request; max (a user) decides nothing.
	if res, _ := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/"+req.ID+"/grant", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("own grant")
	}
	if res, _ := ts.do(maxc, "POST", "/api/v1/agent/protect/emergency/"+req.ID+"/decline", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("user declined")
	}
	_, body = ts.request("GET", "/api/v1/agent/protect/emergency", nil)
	if !strings.Contains(string(body), "Papa ist im Krankenhaus.") {
		t.Fatalf("owner view: %s", body)
	}
	e := ts.server.agentSettings().Features.Emergency
	ts.server.emergencyTick(e, env.clock().Add(71*time.Hour))
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatal("granted early")
	}
	// Restart: the state comes from the volume.
	protectStates.Delete(ts.server)
	ps := ts.server.protect()
	ps.source, ps.runner, ps.now = env.src, env.runner, env.clock
	env.set(env.clock().Add(72 * time.Hour))
	ts.server.emergencyTick(e, env.clock())
	if env.access("Fotos")["lena"] != shares.ReadOnly {
		t.Fatalf("not granted: %v", env.access("Fotos"))
	}
	// lena had write access on Documents: the grant gives nothing there.
	if env.access("Documents")["lena"] != shares.ReadWrite {
		t.Fatal("existing access changed")
	}
	got, _ := ts.server.emergencyLatest("lena")
	if got.State != protect.StateGranted || len(got.Added) != 1 || got.Added[0] != "Fotos" {
		t.Fatalf("grant: %+v", got)
	}
	for _, sh := range ts.shares.List() {
		if sh.Access["lena"] == shares.ReadWrite && sh.Name != "Documents" {
			t.Fatalf("writable through the grant: %s", sh.Name)
		}
	}
	if res, body := ts.do(lena, "GET", "/api/v1/agent/protect/handbook", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("handbook during the grant: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(maxc, "GET", "/api/v1/agent/protect/handbook", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("handbook for someone else")
	}
	// 30 days later it ends by itself, across another restart.
	protectStates.Delete(ts.server)
	ps = ts.server.protect()
	ps.source, ps.runner, ps.now = env.src, env.runner, env.clock
	env.set(env.clock().Add(protect.GrantFor))
	ts.server.emergencyTick(e, env.clock())
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatal("grant did not end")
	}
	if env.access("Documents")["lena"] != shares.ReadWrite {
		t.Fatal("end took what the grant did not give")
	}
	if res, _ := ts.do(lena, "GET", "/api/v1/agent/protect/handbook", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("handbook after the end")
	}
	// A decline is final.
	if res, body = ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "Noch einmal"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("second request: %d %s", res.StatusCode, body)
	}
	json.Unmarshal(body, &req)
	if res, body := ts.request("POST", "/api/v1/agent/protect/emergency/"+req.ID+"/decline", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("decline: %d %s", res.StatusCode, body)
	}
	ts.server.emergencyTick(e, env.clock().Add(30*24*time.Hour))
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatal("declined request granted")
	}
	// The owner grants at once; removing lena ends it.
	_, body = ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "Dritter Versuch"})
	json.Unmarshal(body, &req)
	if res, body := ts.request("POST", "/api/v1/agent/protect/emergency/"+req.ID+"/grant", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("grant now: %d %s", res.StatusCode, body)
	}
	if env.access("Fotos")["lena"] != shares.ReadOnly {
		t.Fatal("not granted now")
	}
	env.features(t, map[string]any{"handbook": map[string]any{"enabled": true}, "emergency": map[string]any{"enabled": true}})
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatal("removed person kept the access")
	}
}

func TestEmergencyCode(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{"emergency": map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "shares": []string{"Fotos"}}}}})
	if res, _ := ts.request("POST", "/api/v1/agent/protect/emergency/code/lena", nil); res.StatusCode != http.StatusBadRequest {
		t.Fatal("code while codes are off")
	}
	env.features(t, map[string]any{"emergency": map[string]any{"enabled": true, "code": true, "persons": []map[string]any{{"account": "lena", "shares": []string{"Fotos"}}}}})
	_, body := ts.request("POST", "/api/v1/agent/protect/emergency/code/lena", nil)
	var c struct{ Code string }
	json.Unmarshal(body, &c)
	if len(c.Code) != 11 {
		t.Fatalf("code: %s", body)
	}
	_, body = ts.request("GET", "/api/v1/agent/features", nil)
	_, body2 := ts.request("GET", "/api/v1/agent/protect/emergency", nil)
	if strings.Contains(string(body)+string(body2), c.Code) || !strings.Contains(string(body2), `"codeSet":true`) {
		t.Fatalf("code shown again: %s", body2)
	}
	lena := env.login(t, "lena")
	if res, _ := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x", "code": "AAAAA-BBBBB"}); res.StatusCode != http.StatusForbidden {
		t.Fatal("wrong code")
	}
	res, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x", "code": c.Code})
	if res.StatusCode != http.StatusCreated || env.access("Fotos")["lena"] != shares.ReadOnly {
		t.Fatalf("code request: %d %s", res.StatusCode, body)
	}
}

func TestHandbook(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	if res, _ := ts.request("GET", "/api/v1/agent/protect/handbook", nil); res.StatusCode != http.StatusNotFound {
		t.Fatal("handbook while off")
	}
	env.features(t, map[string]any{"handbook": map[string]any{"enabled": true, "withContacts": true, "contacts": []map[string]any{{"name": "Onkel Peter", "phone": "0171 123456"}}}})
	res, body := ts.request("GET", "/api/v1/agent/protect/handbook", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"sections"`) || !strings.Contains(string(body), "Documents") {
		t.Fatalf("handbook: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/agent/protect/handbook/download", nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "%PDF") {
		t.Fatalf("pdf: %d", res.StatusCode)
	}
	_, md := ts.request("GET", "/api/v1/agent/protect/handbook/download?format=md", nil)
	for _, want := range []string{"Notfallhandbuch", "Was tun, wenn", "Onkel Peter", "Documents"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("missing %q", want)
		}
	}
	lena := env.login(t, "lena")
	if res, _ := ts.do(lena, "GET", "/api/v1/agent/protect/handbook/download", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("pdf for a user")
	}
	// Made again only when something changed.
	first, _ := ts.server.handbookStored()
	again, _ := ts.server.handbookRefresh(t.Context(), false)
	if again.Made != first.Made {
		t.Fatal("made again without a change")
	}
	ts.request("POST", "/api/v1/shares", map[string]any{"name": "Neu", "volume": "tank", "smb": true})
	changed, _ := ts.server.handbookRefresh(t.Context(), false)
	if changed.Hash == first.Hash {
		t.Fatal("not made again after a change")
	}
	// Destinations never carry secrets.
	for _, d := range []backup.Destination{
		{Type: "rest", URL: "https://user:geheim@backup.example/repo?token=abc", Password: "pw"},
		{Type: "s3", Endpoint: "s3.example", Bucket: "nas", AccessKey: "AKIA", SecretKey: "sk"},
	} {
		w := backupWhere(d)
		for _, secret := range []string{"geheim", "abc", "pw", "AKIA", "sk"} {
			if strings.Contains(w, secret) {
				t.Errorf("%s in %q", secret, w)
			}
		}
	}
	// The nightly tick only works 03:20–03:59.
	ts.server.protect().handbookTried = time.Time{}
	ts.server.handbookTick(ts.server.agentSettings().Features.Handbook, time.Date(2026, 11, 7, 2, 30, 0, 0, time.Local))
	if !ts.server.protect().handbookTried.IsZero() {
		t.Fatal("ran outside the night window")
	}
}
