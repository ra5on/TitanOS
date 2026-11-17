package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
	"github.com/ra5on/TitanOS/backend/internal/tmail/tmailtest"
)

// newMailServer is a test server with T-Mail against mail servers in the
// process, set up with the owner alex.
func newMailServer(t *testing.T) (*testServer, *tmailtest.Servers, *tmail.Manager) {
	t.Helper()
	srv := tmailtest.Start(t)
	volume := t.TempDir()
	var m *tmail.Manager
	ts := newTestServerWith(t, func(o *Options) {
		users := o.Users
		m = tmail.New(o.Store)
		m.Account = func(name string) (tmail.Account, bool) {
			u, ok := users.Get(name)
			return tmail.Account{Name: u.Name, Created: u.Created, Disabled: u.Disabled, Admin: u.Role.CanAdminister()}, ok
		}
		m.RootCAs = srv.Pool
		m.Targets = func(netip.AddrPort) error { return nil }
		m.Volume = func() (*os.Root, func(), error) {
			root, err := os.OpenRoot(volume)
			if err != nil {
				return nil, nil, err
			}
			return root, func() { root.Close() }, nil
		}
		if err := m.Load(); err != nil {
			t.Fatal(err)
		}
		o.Mail = m
	})
	ts.setup()
	return ts, srv, m
}

func mailInput(srv *tmailtest.Servers, password string) map[string]any {
	ih, ip, _ := net.SplitHostPort(srv.IMAPAddr)
	sh, sp, _ := net.SplitHostPort(srv.SMTPAddr)
	iport, _ := strconv.Atoi(ip)
	sport, _ := strconv.Atoi(sp)
	return map[string]any{"name": "Privat", "email": tmailtest.User, "user": tmailtest.User, "password": password,
		"imap": map[string]any{"host": ih, "port": iport, "security": "tls"}, "smtp": map[string]any{"host": sh, "port": sport, "security": "tls"}}
}

// grantMail gives an account T-Mail as the owner alex.
func (ts *testServer) grantMail(name string) {
	ts.t.Helper()
	if res, body := ts.request("PUT", "/api/v1/users/"+name+"/mail", map[string]bool{"enabled": true}); res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":true`) {
		ts.t.Fatalf("grant %s: %d %s", name, res.StatusCode, body)
	}
}

func (ts *testServer) addMailbox(client *http.Client, srv *tmailtest.Servers) string {
	ts.t.Helper()
	res, body := ts.do(client, "POST", "/api/v1/mail/accounts", mailInput(srv, tmailtest.Password))
	if res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("add mailbox: %d %s", res.StatusCode, body)
	}
	var view tmail.MailboxView
	json.Unmarshal(body, &view)
	return view.ID
}

// mailRoutes are the paths of every T-Mail route, with values for their
// parameters.
func mailRoutes(ts *testServer) [][2]string {
	var out [][2]string
	for _, pattern := range ts.server.patterns {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok || !(strings.HasPrefix(path, "/api/v1/mail/") || strings.HasSuffix(path, "/{name}/mail")) {
			continue
		}
		path = strings.NewReplacer("{id}", "x", "{name}", "alex").Replace(path)
		out = append(out, [2]string{method, path})
	}
	return out
}

func TestMailRights(t *testing.T) {
	ts, srv, _ := newMailServer(t)
	routes := mailRoutes(ts)
	if len(routes) < 25 {
		t.Fatalf("only %d mail routes", len(routes))
	}
	// Off by default, also for the owner.
	if _, body := ts.request("GET", "/api/v1/mail/access", nil); !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("access: %s", body)
	}
	for _, rt := range routes {
		if strings.HasSuffix(rt[1], "/access") || strings.HasPrefix(rt[1], "/api/v1/users/") {
			continue
		}
		if res, body := ts.request(rt[0], rt[1], map[string]any{}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "mail_disabled" {
			t.Errorf("%s %s without the grant: %d %s", rt[0], rt[1], res.StatusCode, body)
		}
	}
	ts.grantMail("alex")
	id := ts.addMailbox(ts.client, srv)

	// A normal account: no grant, then its own mailboxes only.
	bob := ts.guest("bob")
	if res, body := ts.do(bob, "GET", "/api/v1/mail/accounts", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "mail_disabled" {
		t.Fatalf("bob without grant: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(bob, "PUT", "/api/v1/users/bob/mail", map[string]bool{"enabled": true}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("bob granted himself: %d", res.StatusCode)
	}
	ts.grantMail("bob")
	if _, body := ts.do(bob, "GET", "/api/v1/mail/accounts", nil); string(bytes.TrimSpace(body)) != "[]" {
		t.Fatalf("bob sees: %s", body)
	}
	for _, path := range []string{"/folders", "/messages?folder=INBOX", "/message?folder=INBOX&uid=1", "/part?folder=INBOX&uid=1&part=1"} {
		if res, body := ts.do(bob, "GET", "/api/v1/mail/accounts/"+id+path, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "mail_not_found" {
			t.Errorf("bob on alex's mailbox %s: %d %s", path, res.StatusCode, body)
		}
	}
	if res, _ := ts.do(bob, "DELETE", "/api/v1/mail/accounts/"+id, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("bob deleted alex's mailbox: %d", res.StatusCode)
	}
	// An administrator does not manage the owner's grant.
	if _, err := ts.users.Create("carol", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	carol := ts.newClient()
	ts.do(carol, "POST", "/api/v1/login", credentials{Name: "carol", Password: testPassword})
	if res, _ := ts.do(carol, "PUT", "/api/v1/users/alex/mail", map[string]bool{"enabled": false}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin changed the owner's grant: %d", res.StatusCode)
	}

	// Never in-process (MCP, the agent, Home Assistant), never a token,
	// never through another node.
	owner, _ := ts.users.Get("alex")
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write")
	for _, rt := range routes {
		code, body := ts.server.internalAs(context.Background(), owner, rt[0], rt[1], map[string]any{})
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt[0], rt[1], code, body)
		}
		req, _ := http.NewRequest(rt[0], ts.url+rt[1], strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a token: %d", rt[0], rt[1], res.StatusCode)
		}
		creq := httptest.NewRequest(rt[0], rt[1], strings.NewReader("{}"))
		creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, creq)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
			t.Errorf("%s %s through the cluster: %d %s", rt[0], rt[1], rec.Code, rec.Body)
		}
	}
	// Taking the grant shuts T-Mail.
	if res, _ := ts.request("PUT", "/api/v1/users/bob/mail", map[string]bool{"enabled": false}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.do(bob, "GET", "/api/v1/mail/accounts", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("after the grant was taken: %d", res.StatusCode)
	}
}

// The KI-Agent gets no access (docs/TMAIL.md): no route of T-Mail is in
// publicRoutes, no tool of the catalog names it, and no file that makes
// tools mentions its paths.
func TestMailIsNoToolAndNoPublicRoute(t *testing.T) {
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/mail") || strings.HasSuffix(r.Path, "/mail") || strings.Contains(strings.ToLower(r.Tool), "mail") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	ts := newTestServer(t)
	ts.setup()
	for _, role := range []auth.Role{auth.RoleOwner, auth.RoleAdmin, auth.RoleUser} {
		for _, tool := range ts.server.catalogTools(auth.User{Name: "alex", Role: role}) {
			if strings.Contains(strings.ToLower(tool.Name), "mail") || strings.Contains(tool.Description, "/mail") || strings.Contains(tool.Description, "T-Mail") {
				t.Errorf("tool %s", tool.Name)
			}
		}
	}
	files, _ := filepath.Glob("*.go")
	pattern := regexp.MustCompile(`/api/v1/mail|"/mail/|tmail\.`)
	for _, f := range files {
		if !regexp.MustCompile(`^(catalog|mcp|agent|public_routes|tool_env)`).MatchString(f) || strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if pattern.Match(data) {
			t.Errorf("%s mentions T-Mail", f)
		}
	}
}

const testMailHTML = `From: "Bob" <bob@example.net>
To: alice@example.org
Subject: Bild
Message-ID: <api-1@example.net>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="o"

--o
Content-Type: multipart/related; boundary="r"

--r
Content-Type: text/html; charset=utf-8

<p onclick="x()">Hallo</p><script>alert(1)</script><img src="cid:pic@x"><img src="https://t.example/p.gif">
--r
Content-Type: image/png
Content-Transfer-Encoding: base64
Content-ID: <pic@x>

iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==
--r--

--o
Content-Type: text/html; name="evil.html"
Content-Disposition: attachment; filename="evil.html"

<script>alert(document.cookie)</script>
--o--
`

func TestMailReadSendAndFiles(t *testing.T) {
	ts, srv, _ := newMailServer(t)
	dir := ts.withShare()
	ts.grantMail("alex")
	id := ts.addMailbox(ts.client, srv)
	base := "/api/v1/mail/accounts/" + id
	srv.Deliver("INBOX", testMailHTML)

	// The event stream of the open program.
	events := make(chan string, 16)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.url+"/api/v1/mail/events", nil)
	stream, err := ts.client.Do(req)
	if err != nil || stream.StatusCode != 200 || stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %v %v", stream, err)
	}
	go func() {
		defer stream.Body.Close()
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: ") {
				events <- strings.TrimPrefix(sc.Text(), "event: ")
			}
		}
	}()

	_, body := ts.request("GET", "/api/v1/mail/accounts", nil)
	if strings.Contains(string(body), tmailtest.Password) || !strings.Contains(string(body), `"passwordSet":true`) {
		t.Fatalf("accounts: %s", body)
	}
	res, body := ts.request("GET", base+"/folders", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"role":"sent"`) {
		t.Fatalf("folders: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", base+"/messages?folder=INBOX&limit=10", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"total":1`) || !strings.Contains(string(body), `"attachments":true`) {
		t.Fatalf("messages: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", base+"/message?folder=INBOX&uid=1", nil)
	var msg tmail.Message
	json.Unmarshal(body, &msg)
	if res.StatusCode != 200 || strings.Contains(msg.HTML, "<script") || strings.Contains(msg.HTML, "onclick") || !strings.Contains(msg.HTML, "data:image/png;base64") || !msg.ExternalImages {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	var evil string
	for _, a := range msg.Attachments {
		if a.Name == "evil.html" {
			evil = a.Part
		}
	}
	// Attachments only as downloads, never run by the browser.
	res, body = ts.request("GET", base+"/part?folder=INBOX&uid=1&part="+evil, nil)
	h := res.Header
	if res.StatusCode != 200 || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") || h.Get("Content-Type") != "application/octet-stream" || !strings.Contains(string(body), "alert(document.cookie)") {
		t.Fatalf("part: %d %v %s", res.StatusCode, h, body)
	}
	res, body = ts.request("GET", base+"/cid?folder=INBOX&uid=1&cid=pic@x", nil)
	h = res.Header
	if res.StatusCode != 200 || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("cid: %d %v", res.StatusCode, h)
	}
	if res, body := ts.request("GET", base+"/part?folder=INBOX&uid=99&part=1", nil); res.StatusCode != 404 || errorCode(t, body) != "mail_not_found" || res.Header.Get("Content-Disposition") != "" {
		t.Fatalf("missing part: %d %v %s", res.StatusCode, res.Header, body)
	}

	// Flags reach the open program.
	if res, body := ts.request("POST", base+"/flags", map[string]any{"folder": "INBOX", "uids": []int{1}, "add": []string{"flagged"}}); res.StatusCode != 204 {
		t.Fatalf("flags: %d %s", res.StatusCode, body)
	}
	select {
	case ev := <-events:
		if ev != "flags" {
			t.Fatalf("event %q", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no flags event")
	}

	// Saving an attachment into the files of the NAS, with the account's
	// rights; never over a file that is there.
	save := map[string]any{"folder": "INBOX", "uid": 1, "part": evil, "root": "share/Docs", "path": "evil.html"}
	if res, body := ts.request("POST", base+"/part/save", save); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "evil.html")); err != nil || !strings.Contains(string(data), "alert") {
		t.Fatalf("saved: %q %v", data, err)
	}
	if res, _ := ts.request("POST", base+"/part/save", save); res.StatusCode < 400 {
		t.Fatalf("saved over a file: %d", res.StatusCode)
	}
	bob := ts.guest("bob")
	ts.grantMail("bob")
	bobID := ts.addMailbox(bob, srv)
	srv.Deliver("INBOX", testMailHTML)
	if res, body := ts.do(bob, "POST", "/api/v1/mail/accounts/"+bobID+"/part/save", map[string]any{"folder": "INBOX", "uid": 1, "part": evil, "root": "share/Docs", "path": "bob.html"}); res.StatusCode != 404 || errorCode(t, body) != "file_not_found" {
		t.Fatalf("bob saved into a share he has no right to: %d %s", res.StatusCode, body)
	}

	// Sending with an upload and a file of the NAS.
	if err := os.WriteFile(filepath.Join(dir, "plan.txt"), []byte("Der Plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	send := func(client *http.Client, mailbox string, nas string) (*http.Response, []byte) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		meta, _ := json.Marshal(map[string]any{"to": []any{"Bob <bob@example.net>", map[string]string{"name": "Carol", "addr": "carol@example.net"}}, "subject": "Mit Anhang", "text": "Siehe Anhang."})
		mw.WriteField("meta", string(meta))
		fw, _ := mw.CreateFormFile("file", "notiz.txt")
		fw.Write([]byte("hochgeladen"))
		mw.WriteField("nasFiles", nas)
		mw.Close()
		req, _ := http.NewRequest("POST", ts.url+"/api/v1/mail/accounts/"+mailbox+"/send", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out bytes.Buffer
		out.ReadFrom(res.Body)
		return res, out.Bytes()
	}
	res, body = send(ts.client, id, `[{"root":"share/Docs","path":"plan.txt"}]`)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"sentCopy":true`) {
		t.Fatalf("send: %d %s", res.StatusCode, body)
	}
	sent := srv.Sent()
	if len(sent) != 1 || !strings.Contains(string(sent[0].Data), "notiz.txt") || !strings.Contains(string(sent[0].Data), "plan.txt") || strings.Join(sent[0].To, ",") != "bob@example.net,carol@example.net" {
		t.Fatalf("sent: %+v", sent)
	}
	if srv.Count("Sent") != 1 {
		t.Fatalf("copy in Sent: %d", srv.Count("Sent"))
	}
	if _, body := ts.request("GET", "/api/v1/mail/contacts?q=car", nil); !strings.Contains(string(body), "carol@example.net") {
		t.Fatalf("contacts: %s", body)
	}
	// Bob may not attach a file of a share he has no right to.
	if res, body := send(bob, bobID, `[{"root":"share/Docs","path":"plan.txt"}]`); res.StatusCode != 404 || errorCode(t, body) != "file_not_found" {
		t.Fatalf("bob attached alex's file: %d %s", res.StatusCode, body)
	}
	if len(srv.Sent()) != 1 {
		t.Fatal("bob's mail went out")
	}
	// A draft.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("meta", `{"to":["bob@example.net"],"subject":"Entwurf","text":"halb fertig"}`)
	mw.Close()
	dreq, _ := http.NewRequest("POST", ts.url+base+"/draft", &buf)
	dreq.Header.Set("Content-Type", mw.FormDataContentType())
	dres, err := ts.client.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	dbody := new(bytes.Buffer)
	dbody.ReadFrom(dres.Body)
	dres.Body.Close()
	if dres.StatusCode != 200 || !regexp.MustCompile(`^\{"uid":[1-9]`).Match(dbody.Bytes()) || srv.Count("Drafts") != 1 {
		t.Fatalf("draft: %d %s", dres.StatusCode, dbody)
	}
}

// Deleting an account takes T-Mail along, here and from another node; a
// new account of the same name inherits nothing.
func TestDeleteAccountTakesMail(t *testing.T) {
	ts, srv, m := newMailServer(t)
	bob := ts.guest("bob")
	ts.grantMail("bob")
	ts.addMailbox(bob, srv)
	file := filepath.Join(ts.store.Dir(), "tmail", "bob.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if res, _ := ts.request("DELETE", "/api/v1/users/bob", nil); res.StatusCode != http.StatusNoContent {
		t.Fatal(res.StatusCode)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("settings left: %v", err)
	}
	access, _ := os.ReadFile(filepath.Join(ts.store.Dir(), "tmail", "access.json"))
	if strings.Contains(string(access), "bob") {
		t.Fatalf("grant left: %s", access)
	}
	bob = ts.guest("bob")
	if _, body := ts.do(bob, "GET", "/api/v1/mail/access", nil); !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("new bob: %s", body)
	}
	// Deleted by another node of the cluster.
	ts.grantMail("bob")
	ts.addMailbox(bob, srv)
	before, _ := ts.users.Get("bob")
	if _, _, err := ts.users.Forget("bob"); err != nil {
		t.Fatal(err)
	}
	ts.server.clusterArea().clusterApplied(before, true, auth.User{}, true)
	if _, err := os.Stat(file); !os.IsNotExist(err) || m.Granted("bob") {
		t.Fatalf("after the cluster deleted bob: %v", err)
	}
}

// Mail passwords never leave the NAS: not in the diagnostic bundle, not in
// the account the cluster passes on.
func TestMailPasswordsStayOnTheNAS(t *testing.T) {
	ts, srv, _ := newMailServer(t)
	ts.grantMail("alex")
	ts.addMailbox(ts.client, srv)
	data, _ := os.ReadFile(filepath.Join(ts.store.Dir(), "tmail", "alex.json"))
	var stored struct {
		Accounts []struct{ Password string } `json:"accounts"`
	}
	json.Unmarshal(data, &stored)
	sealed := stored.Accounts[0].Password
	if sealed == "" || strings.Contains(string(data), tmailtest.Password) {
		t.Fatalf("stored: %s", data)
	}
	// The cluster passes on the account record (auth.User), nothing of
	// T-Mail.
	user, _ := ts.users.Get("alex")
	record, _ := json.Marshal(user)
	for _, secret := range []string{tmailtest.Password, sealed, "tmail"} {
		if strings.Contains(string(record), secret) {
			t.Errorf("account record carries %q", secret)
		}
	}
	// The diagnostic bundle.
	res, body := ts.request("GET", "/api/v1/diagnostics/bundle", nil)
	if res.StatusCode != 200 {
		t.Fatalf("bundle: %d", res.StatusCode)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.File {
		if strings.Contains(f.Name, "tmail") {
			t.Errorf("bundle has %s", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(r)
		r.Close()
		for _, secret := range []string{tmailtest.Password, sealed} {
			if bytes.Contains(content, []byte(secret)) {
				t.Errorf("%s carries the mail password", f.Name)
			}
		}
	}
}
