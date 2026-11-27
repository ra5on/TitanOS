package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const personalBase = "/api/v1/personal-agent"

func personalFixture(t *testing.T) *testServer {
	t.Helper()
	ts := newTestServerWith(t, func(o *Options) { o.PersonalDataDir = t.TempDir() })
	ts.setup()
	return ts
}

// The installed image resolves the private directory through volumedata. The
// developer directory used by most Agent tests does not exercise that path.
func TestPersonalAgentUsesMountedSystemVolume(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if r, _ := ts.request("GET", personalBase+"/state", nil); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unmounted personal state: %d", r.StatusCode)
	}
	share := ts.withShare()
	volume := filepath.Dir(share)
	home := &volumedata.Home{MountRoot: filepath.Dir(volume), Mounted: ts.disks.Mounted, Choose: func() string { return "tank" }, NoLegacy: []string{pa.Feature}}
	home.Locations = map[string]*volumedata.Location{pa.Feature: volumedata.NewLocation(ts.store, "system/agent-volume.json")}
	ts.server.opts.SystemData = home
	t.Cleanup(home.Wait)
	personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
	req := pa.Request{Tool: "agent.example", RequestID: "volume-smoke"}
	job := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	if job.ID == "" {
		t.Fatal("job was not persisted")
	}
	user, ok := ts.users.Get("alex")
	if !ok {
		t.Fatal("missing fixture account")
	}
	if _, err := os.Stat(filepath.Join(volume, ".titanos", pa.Feature, personalKey(user)+".json")); err != nil {
		t.Fatalf("account state on the selected volume: %v", err)
	}
}
func personalHTTP[T any](t *testing.T, ts *testServer, client *http.Client, method, path string, in any) T {
	t.Helper()
	resp, data := ts.do(client, method, personalBase+path, in)
	var out T
	if resp.StatusCode != 200 || json.Unmarshal(data, &out) != nil {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
	}
	return out
}
func workOne(t *testing.T, s *Server) {
	t.Helper()
	lease, err := s.personalClaim()
	if err != nil || lease == "" {
		t.Fatalf("claim: %q %v", lease, err)
	}
	apply, err := s.personalStep(t.Context(), lease, false)
	if err != nil {
		t.Fatal(err)
	}
	if apply {
		again, err := s.personalStep(t.Context(), lease, true)
		if err != nil || again {
			t.Fatal(again, err)
		}
	}
}
func TestPersonalAgentIsolatesAccountsAndPersistsRestart(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	guest, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	client := ts.newClient()
	if res, _ := ts.do(client, "POST", "/api/v1/login", credentials{Name: guest.Name, Password: testPassword}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	req := pa.Request{Tool: "agent.example", Prompt: "private demo", RequestID: "test-1"}
	job := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	replay := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	if replay.ID != job.ID {
		t.Fatal("request replay created another job")
	}
	if res, _ := ts.do(client, "GET", personalBase+"/jobs/"+job.ID, nil); res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.do(client, "PUT", personalBase+"/limits/alex", map[string]any{"rights": map[string]string{}}); res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	if code, _ := s.internalAs(t.Context(), guest, "GET", personalBase+"/state", nil); code != 403 {
		t.Fatalf("indirect access: %d", code)
	}
	if jobs := personalHTTP[pa.State](t, ts, client, "GET", "/state", nil).Jobs; len(jobs) != 0 {
		t.Fatal(jobs)
	}
	if res, _ := ts.request("POST", personalBase+"/jobs", map[string]any{"account": "bea", "tool": "agent.example", "requestId": "injected"}); res.StatusCode != 400 {
		t.Fatal("unexpected principal selector", res.StatusCode)
	}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/memory", map[string]any{"items": []string{"private"}})
	if mem := personalHTTP[struct{ Items []string }](t, ts, client, "GET", "/memory", nil); len(mem.Items) != 0 {
		t.Fatal(mem)
	}
	s.initPersonalTools() // fresh broker process against the same persisted account state
	workOne(t, s)
	j := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+job.ID, nil)
	if j.Status != "done" || j.Result == nil || j.Result.Text != "2 + 3 = 5" {
		t.Fatal(j)
	}
	if len(personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil).Chats) != 1 {
		t.Fatal("lost or duplicated chat")
	}
}
func TestPersonalAgentFreshPreviewRevocationAndDurableFence(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	u, _ := ts.users.Get("alex")
	content := "A"
	effects := 0
	tool := personalTool{ID: "fixture.publish", Title: "Fixture", App: "fixture", Default: pa.Auto, Sensitive: true,
		Prepare: func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
			return pa.Preview{Title: "Preview", Text: content}, nil
		},
		Apply: func(_ context.Context, _ auth.User, _ pa.Request, p pa.Preview) (pa.Result, error) {
			effects++
			return pa.Result{Text: p.Text}, nil
		}}
	s.personal.tools[tool.ID] = tool
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]any{tool.ID: "auto"}})
	job := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool.ID, RequestID: "publish-1"})
	get := func() pa.Job { return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+job.ID, nil) }
	approve := func(token string) {
		personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+job.ID+"/approve", map[string]string{"token": token})
	}
	workOne(t, s)
	j := get()
	if j.Phase != "approval" || j.Token == "" {
		t.Fatal(j)
	}
	oldToken := j.Token
	approve(oldToken)
	content = "B"
	workOne(t, s)
	j = get()
	if j.Phase != "approval" || j.Preview.Text != "B" || j.Token == oldToken || effects != 0 {
		t.Fatal(j, effects)
	}
	if res, _ := ts.request("POST", personalBase+"/jobs/"+job.ID+"/approve", map[string]string{"token": oldToken}); res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
	approve(j.Token)
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]string{tool.ID: "never"}})
	workOne(t, s)
	if j = get(); j.Status != "failed" || effects != 0 {
		t.Fatal(j, effects)
	}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]string{tool.ID: "auto"}})
	job = personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool.ID, RequestID: "publish-2"})
	workOne(t, s)
	approve(get().Token)
	workOne(t, s)
	if j = get(); j.Status != "done" || j.Result.Text != "B" || effects != 1 {
		t.Fatal(j, effects)
	}
	// Simulate loss of the reply after the native write, as recorded by the pre-write fence.
	if err := s.personalTxn(u, true, func(a *personalAccount) error {
		j, _ := a.find(job.ID)
		j.Status = "running"
		j.Phase = "executing"
		a.Plans[j.ID].Claim = "former-process"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.initPersonalTools()
	s.personal.tools[tool.ID] = tool
	lease, err := s.personalClaim()
	if err != nil || lease != "" {
		t.Fatal(lease, err)
	}
	j = get()
	if j.Phase != "uncertain" || j.Status != "failed" || effects != 1 {
		t.Fatal(j, effects)
	}
}
func TestPersonalAgentPauseAndEntitlementDuringRead(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	var allowed atomic.Bool
	allowed.Store(true)
	started := make(chan struct{})
	tool := personalTool{ID: "fixture.read", Title: "Read", App: "fixture", Default: pa.Auto,
		Available: func(auth.User) error {
			if !allowed.Load() {
				return errors.New("app disabled")
			}
			return nil
		},
		Prepare: func(ctx context.Context, _ auth.User, _ pa.Request) (pa.Preview, error) {
			close(started)
			<-ctx.Done()
			return pa.Preview{}, ctx.Err()
		}}
	s.personal.tools[tool.ID] = tool
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{tool.ID: "auto"}, "paused": true})
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool.ID, RequestID: "reader"})
	if token, err := s.personalClaim(); token != "" || err != nil {
		t.Fatal(token, err)
	}
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"paused": false})
	lease, err := s.personalClaim()
	if err != nil || lease == "" {
		t.Fatal(lease, err)
	}
	finish := make(chan error)
	go func() { _, err := s.personalStep(t.Context(), lease, false); finish <- err }()
	<-started
	allowed.Store(false)
	select {
	case err := <-finish:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("did not stop after grant was revoked")
	}
	if got := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil); got.Status != "failed" || got.Result != nil {
		t.Fatal(got)
	}
}
func TestPersonalBrokerExecutesUsingPrivateUnixSocket(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a production worker refuses to run as root")
	}
	ts := personalFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	dir, err := os.MkdirTemp("", "pa-broker-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dir)
	path := filepath.Join(dir, "agent.sock")
	broker := make(chan error, 1)
	go func() { broker <- ts.server.ServePersonalBroker(ctx, path, os.Geteuid()) }()
	worker := make(chan error, 1)
	go func() { worker <- pa.RunWorker(ctx, path) }()
	defer func() {
		cancel()
		if err := <-broker; err != nil {
			t.Fatal(err)
		}
		if err := <-worker; err != nil {
			t.Fatal(err)
		}
	}()
	req := pa.Request{Tool: "agent.example", RequestID: "private-worker"}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		v := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
		if v.Status == "done" {
			st, err := os.Stat(path)
			if err != nil || st.Mode().Perm() != 0600 {
				t.Fatal(st, err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("private worker did not complete job")
}
func TestPersonalAgentRecipeScopesAndSchedules(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	check := func(id, kind string) personalTool {
		return personalTool{ID: id, Title: id, App: "test", SourceKinds: []string{kind}, Default: pa.Auto, Check: func(_ context.Context, _ auth.User, r pa.Request) error {
			if len(r.Sources) != 1 || r.Sources[0].Kind != kind {
				return errors.New("unexpected source")
			}
			return nil
		}, Prepare: func(_ context.Context, _ auth.User, r pa.Request) (pa.Preview, error) {
			if kind == "folder" && r.PreviousText != "plain mail body" {
				return pa.Preview{}, errors.New("missing previous result")
			}
			return pa.Preview{Text: "plain mail body"}, nil
		}}
	}
	s.personal.tools["test.mail"] = check("test.mail", "mail")
	s.personal.tools["test.table"] = check("test.table", "folder")
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"test.mail": "auto", "test.table": "auto"}})
	sk := pa.Skill{ID: "my-personal-skill-0001", Title: "Rechnung", Trigger: "schedule", RunAt: time.Now().Add(time.Hour).UTC(), Approval: "inherit", Steps: []string{"test.mail", "test.table"}, Sources: []pa.Source{{Kind: "mail", ID: "1"}, {Kind: "folder", ID: "2"}}}
	for range 2 {
		personalHTTP[pa.Skill](t, ts, ts.client, "POST", "/skills", sk)
	}
	jobs := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil).Jobs
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	if token, err := s.personalClaim(); token != "" || err != nil {
		t.Fatal(token, err)
	}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/skills/"+sk.ID+"/run", pa.Request{RequestID: "once"})
	workOne(t, s)
	workOne(t, s)
	got := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if got.Status != "done" || got.Result == nil {
		t.Fatal(got)
	}
	personalHTTP[map[string]bool](t, ts, ts.client, "DELETE", "/skills/"+sk.ID, nil)
	jobs = personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil).Jobs
	if !slices.ContainsFunc(jobs, func(j pa.Job) bool { return j.Error == "schedule_removed" }) {
		t.Fatal(jobs)
	}
}

func TestPersonalAgentObjectFenceAcrossDifferentJobs(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	n := 0
	tool := personalTool{ID: "fixture.native", Title: "Native", App: "fixture", Default: pa.Auto, Sensitive: true,
		EffectKey: func(auth.User, pa.Request, pa.Preview) string { return "same-native-draft" },
		Prepare: func(_ context.Context, _ auth.User, r pa.Request) (pa.Preview, error) {
			return pa.Preview{Title: "Native", Text: string(r.Args)}, nil
		},
		Apply: func(context.Context, auth.User, pa.Request, pa.Preview) (pa.Result, error) {
			n++
			return pa.Result{Title: "Accepted"}, nil
		}}
	s.personal.tools[tool.ID] = tool
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{tool.ID: "auto"}})
	run := func(id string, revise bool) pa.Job {
		j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool.ID, Args: json.RawMessage(`{"text":"first"}`), RequestID: id})
		path := "/jobs/" + j.ID
		workOne(t, s)
		j = personalHTTP[pa.Job](t, ts, ts.client, "GET", path, nil)
		if revise {
			old := j.Token
			personalHTTP[pa.Job](t, ts, ts.client, "POST", path+"/revise", map[string]any{"token": old, "args": map[string]string{"text": "edited"}})
			workOne(t, s)
			j = personalHTTP[pa.Job](t, ts, ts.client, "GET", path, nil)
			if j.Token == old || !strings.Contains(j.Preview.Text, "edited") {
				t.Fatal(j)
			}
			res, _ := ts.request("POST", personalBase+path+"/approve", map[string]string{"token": old})
			if res.StatusCode != 409 {
				t.Fatal(res.StatusCode)
			}
		}
		personalHTTP[pa.Job](t, ts, ts.client, "POST", path+"/approve", map[string]string{"token": j.Token})
		workOne(t, s)
		return personalHTTP[pa.Job](t, ts, ts.client, "GET", path, nil)
	}
	first := run("native-1", true)
	other := run("native-2", false)
	if first.CompletedSteps != 1 || len(first.Receipts) != 1 || n != 1 || other.Error != "action_already_attempted" {
		t.Fatal(first, other, n)
	}
}

func TestPersonalAgentCannotForwardRevokedSource(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	allowed := true
	input := personalTool{ID: "fixture.inbox", App: "mail", Title: "Inbox", Default: pa.Auto,
		Check: func(context.Context, auth.User, pa.Request) error {
			if !allowed {
				return errPersonalDenied
			}
			return nil
		},
		Prepare: func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
			return pa.Preview{Text: "private extracted data"}, nil
		},
	}
	var forwarded bool
	output := personalTool{ID: "fixture.table", App: "files", Title: "Table", Default: pa.Auto,
		Prepare: func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
			forwarded = true
			return pa.Preview{}, nil
		},
	}
	s.personal.tools[input.ID], s.personal.tools[output.ID] = input, output
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{input.ID: "auto", output.ID: "auto"}})
	sk := pa.Skill{ID: "revoked-source-recipe", Title: "Rechnung", Trigger: "manual", Approval: "inherit", Steps: []string{input.ID, output.ID}}
	personalHTTP[pa.Skill](t, ts, ts.client, "POST", "/skills", sk)
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/skills/"+sk.ID+"/run", pa.Request{RequestID: "data-flow"})
	workOne(t, s)
	allowed = false
	workOne(t, s)
	finished := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if forwarded || finished.Status != "failed" || finished.Error != "permission_revoked" || finished.Result != nil {
		t.Fatal(finished, forwarded)
	}
}
func TestPersonalAgentBrokenStoreDoesNotStarveOtherAccounts(t *testing.T) {
	ts := personalFixture(t)
	u, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := ts.users.Get("alex")
	if err := os.WriteFile(filepath.Join(ts.server.opts.PersonalDataDir, personalKey(admin)+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	j, err := ts.server.personalNewJob(u, pa.Request{Tool: "agent.example", RequestID: "independent"}, []string{"agent.example"}, false)
	if err != nil {
		t.Fatal(err)
	}
	workOne(t, ts.server)
	err = ts.server.personalTxn(u, false, func(a *personalAccount) error {
		got, err := a.find(j.ID)
		if err == nil && got.Status != "done" {
			t.Fatalf("other account blocked: %+v", got)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPersonalAgentConsentAtNativeBoundaryAndVerifiedReceipt(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		name := "before commit"
		if accepted {
			name = "after accepted"
		}
		t.Run(name, func(t *testing.T) {
			ts := personalFixture(t)
			s := ts.server
			checkpoint := make(chan struct{})
			proceed := make(chan struct{})
			var effects atomic.Int32
			tool := personalTool{ID: "fixture.auth", Title: "private role action", App: "fixture", Default: pa.Auto,
				Prepare: func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
					return pa.Preview{Title: "exact object"}, nil
				},
				Apply: func(ctx context.Context, _ auth.User, _ pa.Request, _ pa.Preview) (pa.Result, error) {
					if accepted {
						if err := pa.Reauthorize(ctx); err != nil {
							return pa.Result{}, err
						}
						effects.Add(1)
					}
					close(checkpoint)
					<-proceed
					if !accepted {
						if err := pa.Reauthorize(ctx); err != nil {
							return pa.Result{}, err
						}
						effects.Add(1)
					}
					return pa.Result{Title: "Sensitive acceptance", Text: "private account data"}, nil
				}}
			s.personal.tools[tool.ID] = tool
			personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{tool.ID: "auto"}})
			j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool.ID, Prompt: "confidential-subject", RequestID: "precise-authority"})
			lease, err := s.personalClaim()
			if err != nil || lease == "" {
				t.Fatal(lease, err)
			}
			ready, err := s.personalStep(t.Context(), lease, false)
			if !ready || err != nil {
				t.Fatal(ready, err)
			}
			finished := make(chan error, 1)
			go func() { _, err := s.personalStep(t.Context(), lease, true); finished <- err }()
			<-checkpoint
			personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]string{tool.ID: "never"}})
			close(proceed)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			got := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
			if accepted && (effects.Load() != 1 || got.Status != "done" || got.CompletedSteps != 1) || !accepted && (effects.Load() != 0 || got.Status != "failed" || got.Error != "permission_revoked") {
				t.Fatal(effects.Load(), got)
			}
			state := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
			raw, _ := json.Marshal(state)
			for _, leak := range []string{"confidential-subject", "private account data", "Sensitive acceptance"} {
				if strings.Contains(string(raw), leak) {
					t.Fatalf("revoked state showed %s", leak)
				}
			}
		})
	}
}
