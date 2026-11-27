package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/webapps"
	"github.com/ra5on/TitanOS/backend/internal/webui"
)

const liveOfficeRoot = "share/Docs"

// The normal test uses real HTTP and the worker socket. TITANOS_OFFICE_BROWSER=1
// additionally serves the embedded UI and the supplied editor packages to Playwright.
func TestOfficeEditorSelectionLive(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the personal Agent worker requires an unprivileged account")
	}
	browser := os.Getenv("TITANOS_OFFICE_BROWSER") == "1"
	if browser && (!webui.Embedded || os.Getenv("UI_OFFICE_PACKAGES") == "" || os.Getenv("TITAND_BIN") == "") {
		t.Fatal("the browser test requires -tags embedui, UI_OFFICE_PACKAGES and TITAND_BIN")
	}
	ts := testServerRoutes(t, func(o *Options) {
		o.PersonalDataDir = t.TempDir()
		if browser {
			o.UI = webui.FS()
			m := webapps.New(o.Store)
			m.Dev, m.DevDir = true, os.Getenv("UI_OFFICE_PACKAGES")
			if err := m.Load(); err != nil {
				t.Fatal(err)
			}
			o.Webapps = m
			t.Cleanup(m.Close)
		}
	}, false)
	if r, _ := ts.request("POST", "/api/v1/setup", credentials{Name: "alex", Password: rand.Text()}); r.StatusCode != http.StatusCreated {
		t.Fatalf("temporary owner setup: %d", r.StatusCode)
	}
	dir := ts.withShare()
	checks := []struct {
		file, title, text string
		region            map[string]string
	}{
		{"Bereiche.docx", "Absatz 3", "Dritter Absatz", map[string]string{"paragraph": "3"}},
		{"Bereiche.xlsx", "Planung · B2:C3", "2\t20\n3\t5", map[string]string{"sheet": "Planung", "range": "B2:C3"}},
		{"Bereiche.pdf", "Seite 2", "Seite zwei", map[string]string{"page": "2"}},
		{"Bereiche.pptx", "Folie 2 · Absatz 2", "Ziel", map[string]string{"slide": "2", "paragraph": "2"}},
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(repo, "scripts/testdata/editor-selection", check.file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, check.file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	levels := map[string]pa.Level{"office.inspect": pa.Auto}
	if os.Getenv("UI_OFFICE_COPY") == "1" {
		levels["office.copy"] = pa.Ask
	}
	rights := map[string]any{"rights": levels}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", rights)
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", rights)
	liveOfficeWorker(t, ts.server, browser)
	if browser {
		runOfficeBrowser(t, ts, repo)
		if os.Getenv("UI_OFFICE_COPY") == "1" {
			checkLiveOfficeCopies(t, dir, os.Getenv("UI_OFFICE_APP"))
		}
		return
	}
	for _, check := range checks {
		t.Run(check.file, func(t *testing.T) {
			query := "?root=" + url.QueryEscape(liveOfficeRoot) + "&path=" + url.QueryEscape(check.file)
			resp, body := ts.request("GET", "/api/v1/files/properties"+query, nil)
			var props files.Properties
			if resp.StatusCode != 200 || json.Unmarshal(body, &props) != nil || props.Version == "" || props.Modified.IsZero() {
				t.Fatalf("file properties: %d %s", resp.StatusCode, body)
			}
			resp, body = ts.request("GET", "/api/v1/files/download"+query, nil)
			original, err := os.ReadFile(filepath.Join(dir, check.file))
			if err != nil || resp.StatusCode != 200 || resp.Header.Get("X-TitanOS-File-Version") != props.Version || !bytes.Equal(original, body) {
				t.Fatalf("download revision or contents differ: %d %v", resp.StatusCode, err)
			}
			ref := check.region
			ref["root"], ref["version"], ref["mtime"] = liveOfficeRoot, props.Version, strconv.FormatInt(props.Modified.UnixNano(), 10)
			selection := personalContext{Tool: "office.inspect", Sources: []pa.Source{{Kind: "office.region", ID: check.file, Label: check.title, Ref: ref}}}
			resp, body = ts.request("POST", personalBase+"/contexts", selection)
			var id struct{ ID string }
			if resp.StatusCode != 201 || json.Unmarshal(body, &id) != nil || id.ID == "" {
				t.Fatalf("handoff: %d %s", resp.StatusCode, body)
			}
			got := personalHTTP[personalContext](t, ts, ts.client, "POST", "/contexts/"+id.ID+"/consume", nil)
			if !reflect.DeepEqual(got, selection) {
				t.Fatal("selection changed during handoff")
			}
			job := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: got.Tool, Sources: got.Sources, RequestID: rand.Text()})
			deadline := time.Now().Add(15 * time.Second)
			for job.Status != "done" && job.Status != "failed" && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
				job = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+job.ID, nil)
			}
			if job.Result == nil || job.Status != "done" || job.Result.Title != check.title || job.Result.Text != check.text || !reflect.DeepEqual(job.Result.Sources, selection.Sources) {
				t.Fatalf("worker returned %s/%s error=%s result=%+v", job.Status, job.Phase, job.Error, job.Result)
			}
		})
	}
}

// Read back the actual files after the browser has opened each new copy.
func checkLiveOfficeCopies(t *testing.T, dir, only string) {
	t.Helper()
	if only == "" || only == "t-word" {
		copyBytes, err := os.ReadFile(filepath.Join(dir, "Überarbeitet.docx"))
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"Erster Absatz", "Der Termin wurde bestätigt.", "Dritter Absatz", "Vierter Absatz", "Fünfter Absatz", "Sechster Absatz"} {
			text, _, err := office.Paragraph(copyBytes, ".docx", 0, i+1, nil)
			if err != nil || text != want {
				t.Fatalf("Word paragraph %d after opening copy: %q, %v", i+1, text, err)
			}
		}
	}
	if only == "" || only == "t-exel" {
		copyBytes, err := os.ReadFile(filepath.Join(dir, "Überarbeitet.xlsx"))
		if err != nil {
			t.Fatal(err)
		}
		const want = "Position\tStück\tPreis\tSumme\nA\t21\t22\t23\nB\t31\t32\t33\nC\t41\t42\t43\nD\t51\t52\t53\nE\t61\t62\t63\nF\t71\t72\t73"
		text, _, err := office.CellRange(copyBytes, "Planung", "A1:D7", nil, "")
		if err != nil || text != want {
			t.Fatalf("spreadsheet values inside or outside the selected range differ: %q, %v", text, err)
		}
	}
}

func liveOfficeWorker(t *testing.T, server *Server, process bool) {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	socket := filepath.Join(t.TempDir(), "worker.sock")
	broker, worker := make(chan error, 1), make(chan error, 1)
	go func() { broker <- server.ServePersonalBroker(ctx, socket, os.Geteuid()) }()
	go func() {
		if process {
			cmd := exec.CommandContext(ctx, os.Getenv("TITAND_BIN"), "-personal-agent-worker", "-personal-agent-socket", socket)
			runErr := cmd.Run()
			if ctx.Err() != nil {
				runErr = nil
			}
			worker <- runErr
		} else {
			worker <- pa.RunWorker(ctx, socket)
		}
	}()
	t.Cleanup(func() {
		stop()
		if err := <-worker; err != nil {
			t.Error(err)
		}
		if err := <-broker; err != nil {
			t.Error(err)
		}
	})
}

func runOfficeBrowser(t *testing.T, ts *testServer, repo string) {
	t.Helper()
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Minute)
	defer stop()
	ids := []string{"t-word", "t-exel", "t-pdf", "t-deck"}
	if os.Getenv("UI_OFFICE_COPY") == "1" {
		ids = []string{"t-word", "t-exel"}
	}
	if id := os.Getenv("UI_OFFICE_APP"); id != "" {
		if !slices.Contains(ids, id) {
			t.Fatalf("unknown UI_OFFICE_APP %q", id)
		}
		ids = []string{id}
	}
	if resp, body := ts.request("POST", "/api/v1/webapps/defaults/install", map[string]any{"apps": ids}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("install: %d %s", resp.StatusCode, body)
	}
	for _, id := range ids {
		if err := ts.server.opts.Webapps.Await(ctx, id); err != nil {
			t.Fatal(id, err)
		}
	}
	if resp, _ := ts.request("POST", "/api/v1/system/disclaimer", map[string]int{"version": 1}); resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("disclaimer: %d", resp.StatusCode)
	}
	// The owner has already chosen the editors to install above. Persist that
	// choice through the same endpoint as the optional migration dialog.
	if resp, _ := ts.request("PUT", "/api/v1/preferences/hints", map[string]bool{"webapps-0.63": true}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("dismiss optional Office migration dialog: %d", resp.StatusCode)
	}
	origin, err := url.Parse(ts.url)
	if err != nil {
		t.Fatal(err)
	}
	var cookies []map[string]any
	for _, c := range ts.client.Jar.Cookies(origin) {
		cookies = append(cookies, map[string]any{"name": c.Name, "value": c.Value, "domain": origin.Hostname(), "path": "/", "httpOnly": true, "sameSite": "Strict", "secure": false, "expires": -1})
	}
	if len(cookies) == 0 {
		t.Fatal("missing temporary session")
	}
	state, err := json.Marshal(map[string]any{"cookies": cookies, "origins": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(t.TempDir(), "browser.json")
	if err := os.WriteFile(session, state, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(repo, "scripts/test-ui-office-native.mjs"))
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "UI_URL="+ts.url, "UI_STORAGE_STATE="+session, "UI_OFFICE_ROOT="+liveOfficeRoot)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
