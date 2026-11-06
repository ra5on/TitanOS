package api

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/imapmini"
	"github.com/ra5on/TitanOS/backend/internal/office"
)

// docsServer is an agent with the share KI-Agent on a test volume; it
// returns alex's folder in the share.
func docsServer(t *testing.T) (*testServer, string) {
	t.Helper()
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	ts.server.agentSyncShare()
	home := filepath.Join(mnt, "tank", agentShareName, "alex")
	if _, err := os.Stat(home); err != nil {
		t.Fatal(err)
	}
	return ts, home
}

func docsNAS(t *testing.T, ts *testServer, nas map[string]any) {
	t.Helper()
	if res, body := ts.request("PUT", "/api/v1/agent/features", map[string]any{"docs": nas}); res.StatusCode != http.StatusOK {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
}

func docsMine(t *testing.T, ts *testServer, client *http.Client, mine map[string]any) []byte {
	t.Helper()
	res, body := ts.do(client, "PUT", "/api/v1/agent/features/me", mine)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mine: %d %s", res.StatusCode, body)
	}
	return body
}

func hasTool(ts *testServer, user auth.User, name string) bool {
	box := ts.server.newAgentToolbox(user, ts.server.agentSettings())
	for _, spec := range box.Specs(nil) {
		if spec.Function.Name == name {
			return true
		}
	}
	return false
}

func TestDocsOffByDefault(t *testing.T) {
	ts, _ := docsServer(t)
	alex, _ := ts.users.Get("alex")
	for _, route := range []struct{ method, path string }{{"GET", "/agent/docs"}, {"POST", "/agent/docs/run"}, {"GET", "/agent/docs/search?q=x"}, {"GET", "/agent/paper"}, {"POST", "/agent/paper/run"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusConflict {
			t.Errorf("%s while off: %d", route.path, res.StatusCode)
		}
	}
	if hasTool(ts, alex, "documents_search") || hasTool(ts, alex, "paperwork_summary") {
		t.Fatal("tools while off")
	}
	// The NAS switch alone does not switch it on for an account.
	docsNAS(t, ts, map[string]any{"enabled": true, "deadlines": true, "paper": true})
	if hasTool(ts, alex, "documents_search") {
		t.Fatal("tool before the account switched it on")
	}
	if res, _ := ts.request("GET", "/api/v1/agent/paper", nil); res.StatusCode != http.StatusConflict {
		t.Fatal("paper before the account switched it on")
	}
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true}, "paper": map[string]any{"on": true}})
	if !hasTool(ts, alex, "documents_search") || !hasTool(ts, alex, "paperwork_summary") {
		t.Fatal("tools missing when on")
	}
	// The NAS switch is for owners and administrators only.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "PUT", "/api/v1/agent/features", map[string]any{"docs": map[string]any{"enabled": false}}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user set the NAS switch: %d", res.StatusCode)
	}
}

func TestDocsFolderRights(t *testing.T) {
	ts, _ := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true})
	ts.users.Create("gast", testPassword, auth.RoleUser)
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	// gast has no access to the share Docs, and a volume is never a folder
	// of the memory; also not for the owner.
	for _, c := range []struct {
		client *http.Client
		root   string
	}{{guest, "share/Docs"}, {ts.client, "volume/tank"}, {guest, "share/Nope"}} {
		res, body := ts.do(c.client, "PUT", "/api/v1/agent/features/me", map[string]any{"docs": map[string]any{"on": true, "custom": true, "folders": []map[string]string{{"root": c.root, "path": ""}}}})
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s accepted: %d %s", c.root, res.StatusCode, body)
		}
	}
	for _, bad := range []string{"../alex", ".titanos", "a/../../b"} {
		res, _ := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"docs": map[string]any{"custom": true, "folders": []map[string]string{{"root": "share/Docs", "path": bad}}}})
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("path %q accepted", bad)
		}
	}
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true, "custom": true, "folders": []map[string]string{{"root": "share/Docs", "path": "Verträge"}}}})
	// The target of the filing must be writable for the account.
	res, _ := ts.do(guest, "PUT", "/api/v1/agent/features/me", map[string]any{"paper": map[string]any{"on": true, "target": map[string]string{"root": "share/Docs", "path": "Post"}}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatal("filing into a share without access")
	}
}

func TestPaperMailSecretNotShown(t *testing.T) {
	ts, _ := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	body := docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.example.org", "user": "alex", "password": "sehr-geheim-123"}}})
	if strings.Contains(string(body), "sehr-geheim") || !strings.Contains(string(body), `"passwordSet":true`) {
		t.Fatalf("answer: %s", body)
	}
	_, body = ts.request("GET", "/api/v1/agent/features/me", nil)
	if strings.Contains(string(body), "sehr-geheim") || !strings.Contains(string(body), `"passwordSet":true`) {
		t.Fatalf("view: %s", body)
	}
	// Saving without the password keeps it.
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.example.org", "user": "alex", "mode": "attachments"}}})
	m, _ := ts.server.agentFeaturesOf("alex")
	if m.Paper.Mail.Password != "sehr-geheim-123" || m.Paper.Mail.Mode != "attachments" {
		t.Fatalf("kept: %+v", m.Paper.Mail)
	}
	// Without a password the mailbox cannot be switched on.
	if res, _ := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"paper": map[string]any{"mail": map[string]any{"enabled": true, "host": "imap.other.org", "user": "x"}}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("mailbox without password")
	}
	// The NAS-wide view has no secrets of the group either.
	_, body = ts.request("GET", "/api/v1/agent/features", nil)
	if strings.Contains(string(body), "geheim") {
		t.Fatal(string(body))
	}
}

func waitDocs(t *testing.T, ts *testServer, user string) {
	t.Helper()
	rt := ts.server.docsRT(user)
	deadline := time.Now().Add(10 * time.Second)
	for {
		rt.mu.Lock()
		busy := rt.running || rt.paperRun
		rt.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not end")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDocsIndexSearchAndDeadlines(t *testing.T) {
	ts, home := docsServer(t)
	alex, _ := ts.users.Get("alex")
	docsNAS(t, ts, map[string]any{"enabled": true, "deadlines": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true, "deadlines": true}})
	year := time.Now().Year() + 2
	os.MkdirAll(filepath.Join(home, "Dokumente", "Verträge"), 0o755)
	os.WriteFile(filepath.Join(home, "Dokumente", "Verträge", "HUK Beitragsrechnung.md"), []byte(fmt.Sprintf("# HUK-COBURG\n\nIhre Kfz-Versicherung.\nKündigung spätestens einen Monat vor Ablauf (31.12.%d) in Textform.\n", year)), 0o644)
	os.WriteFile(filepath.Join(home, "Dokumente", "Einkauf.txt"), []byte("Milch, Brot"), 0o644)
	os.WriteFile(filepath.Join(home, "Dokumente", ".versteckt.txt"), []byte("Kfz-Versicherung geheim"), 0o644)
	os.WriteFile(filepath.Join(home, "Chats", "x.md"), []byte("Kfz-Versicherung im Chat"), 0o644)

	if res, body := ts.request("POST", "/api/v1/agent/docs/run", nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %d %s", res.StatusCode, body)
	}
	waitDocs(t, ts, "alex")
	res, body := ts.request("GET", "/api/v1/agent/docs", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"state":"done"`) || !strings.Contains(string(body), `"files":2`) {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/agent/docs/search?q="+urlQuery("Wann läuft die Kfz-Versicherung aus?"), nil)
	var found struct {
		Hits []struct{ Root, Path, Snippet string }
	}
	json.Unmarshal(body, &found)
	if res.StatusCode != 200 || len(found.Hits) != 1 || found.Hits[0].Path != "Dokumente/Verträge/HUK Beitragsrechnung.md" || !strings.Contains(found.Hits[0].Snippet, "Kündigung") {
		t.Fatalf("search: %d %s", res.StatusCode, body)
	}
	// The tool answers with file and page.
	box := ts.server.newAgentToolbox(alex, ts.server.agentSettings())
	out, err := box.tools["documents_search"].own(context.Background(), nil, json.RawMessage(`{"query":"Kfz-Versicherung"}`))
	if err != nil || !strings.Contains(out.Text, "HUK Beitragsrechnung.md, Seite 1") || !box.tools["documents_search"].untrusted {
		t.Fatalf("tool: %v %s", err, out.Text)
	}

	// The deadline waits in Vorschläge with its source.
	_, body = ts.request("GET", "/api/v1/agent/inbox", nil)
	var inbox struct{ Items []agentInboxItem }
	json.Unmarshal(body, &inbox)
	var item *agentInboxItem
	for i := range inbox.Items {
		if inbox.Items[i].Kind == "deadline" {
			item = &inbox.Items[i]
		}
	}
	want := fmt.Sprintf("Kündigung bis 30.11.%d", year)
	if item == nil || !strings.HasPrefix(item.Title, want) || !strings.Contains(item.Body, "HUK Beitragsrechnung.md") || item.Data["path"] != "Dokumente/Verträge/HUK Beitragsrechnung.md" {
		t.Fatalf("inbox: %s", body)
	}
	// Übernehmen makes reminders 14 and 2 days before, at 09:00.
	if res, body := ts.request("POST", "/api/v1/agent/inbox/"+item.ID, map[string]any{"accept": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("accept: %d %s", res.StatusCode, body)
	}
	list, _ := ts.server.agentStore().Schedules("alex")
	dates := []string{}
	for _, sc := range list {
		if sc.Kind == "reminder" && sc.Every == "once" && sc.At == "09:00" && strings.HasPrefix(sc.Name, "Frist: ") {
			dates = append(dates, sc.Date)
		}
	}
	if strings.Join(dates, ",") != fmt.Sprintf("%d-11-16,%d-11-28", year, year) {
		t.Fatalf("reminders: %v", dates)
	}
	// A second run offers nothing again.
	os.WriteFile(filepath.Join(home, "Dokumente", "Verträge", "HUK Beitragsrechnung.md"), []byte(fmt.Sprintf("# HUK\nKündigung spätestens einen Monat vor Ablauf (31.12.%d).\n", year)), 0o644)
	os.Chtimes(filepath.Join(home, "Dokumente", "Verträge", "HUK Beitragsrechnung.md"), time.Now(), time.Now().Add(time.Hour))
	ts.request("POST", "/api/v1/agent/docs/run", nil)
	waitDocs(t, ts, "alex")
	if items := ts.server.agentInbox("alex"); len(items) != 0 {
		t.Fatalf("offered again: %+v", items)
	}

	// Another account sees nothing of alex's documents.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	docsMine(t, ts, guest, map[string]any{"docs": map[string]any{"on": true}})
	_, body = ts.do(guest, "GET", "/api/v1/agent/docs/search?q=Kfz", nil)
	if strings.Contains(string(body), "HUK") {
		t.Fatalf("other account: %s", body)
	}
}

func urlQuery(s string) string {
	r := strings.NewReplacer(" ", "%20", "?", "%3F", "ä", "%C3%A4", "ü", "%C3%BC")
	return r.Replace(s)
}

func TestDocsRemindRightAway(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "deadlines": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true, "deadlines": true, "mode": "remind", "lead": []int{7}}})
	year := time.Now().Year() + 1
	os.WriteFile(filepath.Join(home, "Dokumente", "Garantie.txt"), []byte(fmt.Sprintf("Die Garantie endet am 14.03.%d.", year)), 0o644)
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	if err := ts.server.docsRun(context.Background(), alex, m.Docs, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	list, _ := ts.server.agentStore().Schedules("alex")
	if len(list) != 1 || list[0].Date != fmt.Sprintf("%d-03-07", year) || len(ts.server.agentInbox("alex")) != 0 {
		t.Fatalf("reminder: %+v", list)
	}
}

// ---------- paperwork ----------

func TestPaperInboxFilingUndoAssign(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "deadlines": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true, "deadlines": true}, "paper": map[string]any{"on": true}})
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	inbox := filepath.Join(home, paperInbox)
	os.MkdirAll(inbox, 0o755)
	letter, _ := os.ReadFile("../docindex/testdata/briefe/stadtwerke.txt")
	due := time.Now().AddDate(0, 0, 9).Format("02.01.2006")
	letter = []byte(strings.Replace(string(letter), "15.11.2026", due, 1))
	old := time.Now().Add(-5 * time.Minute)
	os.WriteFile(filepath.Join(inbox, "scan1.txt"), letter, 0o644)
	os.Chtimes(filepath.Join(inbox, "scan1.txt"), old, old)
	os.WriteFile(filepath.Join(inbox, "Scan_0042.txt"), []byte("Bitte bringen Sie die Unterlagen mit."), 0o644)
	os.Chtimes(filepath.Join(inbox, "Scan_0042.txt"), old, old)
	os.WriteFile(filepath.Join(inbox, "noch-im-scanner.txt"), letter, 0o644)

	if err := ts.server.paperRun(context.Background(), alex, m, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	filed := filepath.Join(home, "Dokumente", "Ablage", "Stadtwerke Musterstadt", "2026-11-03 Stadtwerke Musterstadt Abschlag.txt")
	if _, err := os.Stat(filed); err != nil {
		t.Fatalf("not filed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inbox, "Scan_0042.txt")); err != nil {
		t.Fatal("unsure document moved")
	}
	if _, err := os.Stat(filepath.Join(inbox, "noch-im-scanner.txt")); err != nil {
		t.Fatal("fresh file moved")
	}
	res, body := ts.request("GET", "/api/v1/agent/paper?month=2026-11", nil)
	var sum struct {
		Total  int64
		Count  int
		Types  []paperType
		Open   []paperView
		Recent []paperView
	}
	json.Unmarshal(body, &sum)
	if res.StatusCode != 200 || sum.Total != 8600 || sum.Count != 1 || len(sum.Types) != 1 || sum.Types[0].Type != "energy" || len(sum.Open) != 1 || len(sum.Recent) != 2 {
		t.Fatalf("summary: %d %s", res.StatusCode, body)
	}
	// The due date waits as a suggestion with the entry.
	items := ts.server.agentInbox("alex")
	if len(items) != 1 || items[0].Data["paper"] == "" || !strings.Contains(items[0].Title, "86,00 €") {
		t.Fatalf("due: %+v", items)
	}
	var ask, done paperView
	for _, v := range sum.Recent {
		if v.State == "ask" {
			ask = v
		} else {
			done = v
		}
	}
	// Rückgängig: back into the inbox, waiting for Zuordnen.
	if res, body := ts.request("POST", "/api/v1/agent/paper/"+done.ID+"/undo", map[string]any{}); res.StatusCode != 200 {
		t.Fatalf("undo: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(inbox, "scan1.txt")); err != nil {
		t.Fatal("not back")
	}
	if _, err := os.Stat(filed); err == nil {
		t.Fatal("still filed")
	}
	// Zuordnen of the unsure one.
	if res, body := ts.request("POST", "/api/v1/agent/paper/"+ask.ID+"/assign", map[string]any{"sender": "Zahnarzt Dr. Weber", "type": "health", "kind": "Brief", "date": "2026-11-04"}); res.StatusCode != 200 || !strings.Contains(string(body), "Gesundheit") && !strings.Contains(string(body), "Zahnarzt Dr. Weber") {
		t.Fatalf("assign: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(home, "Dokumente", "Ablage", "Zahnarzt Dr. Weber", "2026-11-04 Zahnarzt Dr. Weber Brief.txt")); err != nil {
		t.Fatal("assigned not filed")
	}
	if res, _ := ts.request("POST", "/api/v1/agent/paper/"+ask.ID+"/assign", map[string]any{"sender": "x", "type": "health"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("assigned twice")
	}
	if res, _ := ts.request("POST", "/api/v1/agent/paper/"+done.ID+"/paid", map[string]any{"paid": true}); res.StatusCode != 200 {
		t.Fatal("paid")
	}
	// "Jedes Mal vorschlagen": nothing is filed by itself.
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "filing": "ask"}})
	m, _ = ts.server.agentFeaturesOf("alex")
	os.WriteFile(filepath.Join(inbox, "scan2.txt"), letter, 0o644)
	os.Chtimes(filepath.Join(inbox, "scan2.txt"), old, old)
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	if _, err := os.Stat(filepath.Join(inbox, "scan2.txt")); err != nil {
		t.Fatal("filed although asked to propose")
	}
}

// A scan waits in the inbox while the picture model is not there.
func TestPaperScanWaitsForModel(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true}})
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	inbox := filepath.Join(home, paperInbox)
	os.MkdirAll(inbox, 0o755)
	old := time.Now().Add(-5 * time.Minute)
	os.WriteFile(filepath.Join(inbox, "foto.jpg"), []byte("\xff\xd8\xff"), 0o644)
	os.Chtimes(filepath.Join(inbox, "foto.jpg"), old, old)
	look := docsLook
	t.Cleanup(func() { docsLook = look })
	docsLook = func(context.Context, *Server, []byte) (string, error) { return "", errDocsWait }
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	lg, _ := ts.server.paperLoad("alex")
	if len(lg.Entries) != 1 || lg.Entries[0].State != "waiting" {
		t.Fatalf("%+v", lg.Entries)
	}
	// With the model back, the scan is read and filed.
	docsLook = func(context.Context, *Server, []byte) (string, error) {
		return "Telekom Deutschland GmbH\nDatum 04.11.2026\nRechnungsbetrag 44,95 €\nIhre Rechnung Internet", nil
	}
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	if _, err := os.Stat(filepath.Join(home, "Dokumente", "Ablage", "Telekom", "2026-11-04 Telekom Rechnung.jpg")); err != nil {
		lg, _ = ts.server.paperLoad("alex")
		t.Fatalf("not filed: %+v", lg.Entries)
	}
}

// ---------- mailbox ----------

type fakeIMAP struct {
	addr string
	pool *x509.CertPool
	cert tls.Certificate
	mu   sync.Mutex
	mail map[uint32]string
	seen map[uint32]bool
	cmds []string
	// lie: the size the server reports for a mail (a broken server).
	lie map[uint32]int
}

func startFakeIMAP(t *testing.T) *fakeIMAP {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "imap.test"}, DNSNames: []string{"imap.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	leaf, _ := x509.ParseCertificate(der)
	f := &fakeIMAP{pool: x509.NewCertPool(), cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, mail: map[uint32]string{}, seen: map[uint32]bool{}, lie: map[uint32]int{}}
	f.pool.AddCert(leaf)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{f.cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeIMAP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	fmt.Fprint(c, "* OK ready\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		tag, cmd, _ := strings.Cut(strings.TrimSpace(line), " ")
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(cmd, "LOGIN "):
			if cmd == `LOGIN "alex" "pw-geheim"` {
				fmt.Fprintf(c, "%s OK\r\n", tag)
			} else {
				fmt.Fprintf(c, "%s NO wrong\r\n", tag)
			}
		case cmd == "SELECT INBOX":
			fmt.Fprintf(c, "* OK [UIDVALIDITY 42] ok\r\n%s OK\r\n", tag)
		case cmd == "UID SEARCH UNSEEN":
			f.mu.Lock()
			ids := []string{}
			for uid := range f.mail {
				if !f.seen[uid] {
					ids = append(ids, fmt.Sprint(uid))
				}
			}
			f.mu.Unlock()
			fmt.Fprintf(c, "* SEARCH %s\r\n%s OK\r\n", strings.Join(ids, " "), tag)
		case strings.HasSuffix(cmd, "(RFC822.SIZE)"):
			var uid uint32
			fmt.Sscanf(cmd, "UID FETCH %d", &uid)
			f.mu.Lock()
			size, lied := f.lie[uid]
			if !lied {
				size = len(f.mail[uid])
			}
			f.mu.Unlock()
			fmt.Fprintf(c, "* 1 FETCH (UID %d RFC822.SIZE %d)\r\n%s OK\r\n", uid, size, tag)
		case strings.HasPrefix(cmd, "UID FETCH "):
			var uid uint32
			fmt.Sscanf(cmd, "UID FETCH %d", &uid)
			body := f.mail[uid]
			fmt.Fprintf(c, "* 1 FETCH (UID %d BODY[] {%d}\r\n%s)\r\n%s OK\r\n", uid, len(body), body, tag)
		case strings.HasPrefix(cmd, "UID STORE "):
			var uid uint32
			fmt.Sscanf(cmd, "UID STORE %d", &uid)
			f.mu.Lock()
			f.seen[uid] = true
			f.mu.Unlock()
			fmt.Fprintf(c, "%s OK\r\n", tag)
		case cmd == "LOGOUT":
			fmt.Fprintf(c, "* BYE\r\n%s OK\r\n", tag)
			return
		default:
			fmt.Fprintf(c, "%s BAD\r\n", tag)
		}
	}
}

func TestPaperMailbox(t *testing.T) {
	ts, home := docsServer(t)
	f := startFakeIMAP(t)
	hook := paperIMAP
	t.Cleanup(func() { paperIMAP = hook })
	paperIMAP = func(c *imapmini.Config) {
		c.TLS = &tls.Config{RootCAs: f.pool}
		c.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, f.addr)
		}
	}
	read := docsReadPDF
	t.Cleanup(func() { docsReadPDF = read })
	docsReadPDF = func(context.Context, []byte, int, int) (office.PDFReading, error) {
		return office.PDFReading{Pages: []office.PDFPage{{Number: 1, Text: "Telekom Deutschland GmbH\nDatum 04.11.2026\nIhre Rechnung\nRechnungsbetrag 44,95 €"}}}, nil
	}
	f.mail[11] = "From: Telekom <rechnung@telekom.test>\r\nSubject: Ihre Rechnung\r\nDate: Wed, 04 Nov 2026 10:00:00 +0100\r\nMessage-ID: <t1@telekom.test>\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nIm Anhang Ihre Rechnung.\r\n" +
		"--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"../Rechnung.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n--B--\r\n"
	f.mail[12] = "From: congstar <info@congstar.test>\r\nSubject: Deine Rechnung Oktober\r\nDate: Thu, 05 Nov 2026 10:00:00 +0100\r\nMessage-ID: <c1@congstar.test>\r\n\r\n" +
		"Hallo Alex,\r\nRechnungsdatum: 05.11.2026\r\nRechnungsbetrag: 15,00 EUR\r\nDein congstar Team\r\n"
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.test", "user": "alex", "password": "pw-geheim"}}})
	if res, body := ts.request("POST", "/api/v1/agent/paper/mail/test", nil); res.StatusCode != 200 {
		t.Fatalf("test: %d %s", res.StatusCode, body)
	}
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	if err := ts.server.paperRun(context.Background(), alex, m, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !f.seen[11] || !f.seen[12] {
		t.Fatal("not marked read")
	}
	for _, c := range f.cmds {
		if strings.Contains(c, "EXPUNGE") || strings.Contains(c, "Deleted") || strings.Contains(c, "MOVE") {
			t.Fatalf("mailbox changed: %s", c)
		}
	}
	for _, p := range []string{"Telekom/2026-11-04 Telekom Rechnung.pdf", "Telekom/2026-11-04 Telekom Rechnung.md", "congstar/2026-11-05 congstar Rechnung.md"} {
		if _, err := os.Stat(filepath.Join(home, "Dokumente", "Ablage", p)); err != nil {
			lg, _ := ts.server.paperLoad("alex")
			t.Fatalf("%s: %v %+v", p, err, lg.Entries)
		}
	}
	lg, _ := ts.server.paperLoad("alex")
	origins := map[string]int{}
	for _, e := range lg.Entries {
		origins[e.Origin]++
	}
	if origins["mail"] != 1 || origins["mailtext"] != 2 || lg.MailError != "" {
		t.Fatalf("log: %+v", lg)
	}
	// The same mail again (STORE got lost) is not taken twice.
	f.seen[12] = false
	ts.server.paperRun(context.Background(), alex, m, true, time.Now())
	lg, _ = ts.server.paperLoad("alex")
	if len(lg.Entries) != 3 || !f.seen[12] {
		t.Fatalf("twice: %d", len(lg.Entries))
	}
	// A wrong password ends up as an error without the password.
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.test", "user": "alex", "password": "falsch-geheim"}}})
	res, body := ts.request("POST", "/api/v1/agent/paper/mail/test", nil)
	if res.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "falsch-geheim") {
		t.Fatalf("wrong password: %d %s", res.StatusCode, body)
	}
}

func TestDocsNightTick(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "at": "03:00"})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true}})
	os.WriteFile(filepath.Join(home, "Dokumente", "a.txt"), []byte("Versicherungsschein"), 0o644)
	loc := ts.server.agentLocation()
	night := time.Date(2026, 11, 7, 3, 0, 0, 0, loc)
	for _, at := range []time.Time{night.Add(-time.Minute), night.Add(5 * time.Minute), night.Add(4 * time.Hour)} {
		if docsNightDue("03:00", at) {
			t.Fatalf("due at %v", at)
		}
	}
	ts.server.agentDocsTick(night)
	waitDocs(t, ts, "alex")
	var st docsState
	ts.server.docsRead("alex", docsStateFile, &st, maxDocsState)
	f := st.Folders["share/KI-Agent/Dokumente"]
	if f == nil || f.Files != 1 || !f.LastRun.Equal(night) {
		t.Fatalf("state: %+v", f)
	}
	if !docsNightDone(st, []agentFolderRef{{Root: "share/KI-Agent", Path: "Dokumente"}}, "03:00", night.Add(10*time.Minute)) {
		t.Fatal("night not done")
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/features", map[string]any{"docs": map[string]any{"enabled": true, "at": "3 Uhr"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("bad time accepted")
	}
}

// A folder or target saved before stays when its share goes away: the
// page sends all settings back, also when another group saves its part.
func TestDocsStaleFolderKeepsSaving(t *testing.T) {
	ts, home := docsServer(t)
	mnt := filepath.Dir(filepath.Dir(filepath.Dir(home)))
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	mine := map[string]any{
		"docs":  map[string]any{"on": true, "custom": true, "folders": []map[string]string{{"root": "share/Docs", "path": ""}}},
		"paper": map[string]any{"on": true, "target": map[string]string{"root": "share/Docs", "path": "Post"}},
	}
	docsMine(t, ts, ts.client, mine)
	ts.disks.UseMounts(mnt, func() map[string]bool { return map[string]bool{} })
	mine["diary"] = map[string]any{}
	docsMine(t, ts, ts.client, mine)
	// A new folder without access is still refused.
	mine["docs"] = map[string]any{"custom": true, "folders": []map[string]string{{"root": "share/Docs", "path": ""}, {"root": "share/Docs", "path": "Neu"}}}
	if res, _ := ts.request("PUT", "/api/v1/agent/features/me", mine); res.StatusCode != http.StatusBadRequest {
		t.Fatal("new folder without access accepted")
	}
}

// The memory has a budget; when it is full new documents wait, and a
// broken index is read anew instead of stopping the memory for good.
func TestDocsBudgetAndBrokenIndex(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true}})
	budget := docsBudget
	t.Cleanup(func() { docsBudget = budget })
	docsBudget = 2500
	for i := range 3 {
		os.WriteFile(filepath.Join(home, "Dokumente", fmt.Sprintf("brief%d.txt", i)), []byte("Versicherung "+strings.Repeat("x", 1500)), 0o644)
	}
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	if err := ts.server.docsRun(context.Background(), alex, m.Docs, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	_, body := ts.request("GET", "/api/v1/agent/docs", nil)
	if !strings.Contains(string(body), `"state":"full"`) {
		t.Fatalf("status: %s", body)
	}
	if hits, err := ts.server.docsSearch(alex, "Versicherung", 10, nil); err != nil || len(hits) != 1 {
		t.Fatalf("search: %v %v", hits, err)
	}
	root, _ := ts.server.docsRoot("alex")
	root.WriteFile(docsIndexFile, []byte("{kaputt"), 0o600)
	root.Close()
	docsBudget = budget
	if err := ts.server.docsRun(context.Background(), alex, m.Docs, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if hits, _ := ts.server.docsSearch(alex, "Versicherung", 10, nil); len(hits) != 3 {
		t.Fatalf("rebuilt: %v", hits)
	}
}

func fakeMailbox(t *testing.T, ts *testServer) *fakeIMAP {
	t.Helper()
	f := startFakeIMAP(t)
	hook := paperIMAP
	t.Cleanup(func() { paperIMAP = hook })
	paperIMAP = func(c *imapmini.Config) {
		c.TLS = &tls.Config{RootCAs: f.pool}
		c.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, f.addr)
		}
	}
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.test", "user": "alex", "password": "pw-geheim"}}})
	return f
}

// PLAN 53: a run that finds nothing new writes nothing, so the disk can
// go to standby; the time of the last look stays in RAM.
func TestPaperRunWritesOnlyChanges(t *testing.T) {
	ts, _ := docsServer(t)
	fakeMailbox(t, ts)
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	for range 2 {
		if err := ts.server.paperRun(context.Background(), alex, m, true, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := ts.server.docsRoot("alex")
	_, err := root.Stat(paperFile)
	root.Close()
	if err == nil {
		t.Fatal("paper.json written although nothing changed")
	}
	if _, body := ts.request("GET", "/api/v1/agent/docs", nil); !strings.Contains(string(body), `"mailAt":"`) {
		t.Fatalf("mail time not shown: %s", body)
	}
}

// A mail that is too large, one whose size the server got wrong and one
// that cannot be read never block the mails after them, and are not
// fetched again.
func TestPaperMailSkipsBadMails(t *testing.T) {
	ts, home := docsServer(t)
	f := fakeMailbox(t, ts)
	limit := imapmini.MaxMessage
	t.Cleanup(func() { imapmini.MaxMessage = limit })
	imapmini.MaxMessage = 3000
	big := "From: a@b.test\r\nSubject: gross\r\n\r\n" + strings.Repeat("x", 5000) + "\r\n"
	f.mail[5] = big
	f.mail[6] = big
	f.lie[6] = 100
	f.mail[7] = "kein Kopf\r\n\r\nText\r\n"
	f.mail[8] = "From: congstar <info@congstar.test>\r\nSubject: Deine Rechnung Oktober\r\nMessage-ID: <c8@congstar.test>\r\nDate: Thu, 05 Nov 2026 10:00:00 +0100\r\n\r\nRechnungsdatum: 05.11.2026\r\nRechnungsbetrag: 15,00 EUR\r\n"
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	if err := ts.server.paperRun(context.Background(), alex, m, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !f.seen[8] || f.seen[5] || f.seen[6] || f.seen[7] {
		t.Fatalf("seen: %v", f.seen)
	}
	if _, err := os.Stat(filepath.Join(home, "Dokumente", "Ablage", "congstar", "2026-11-05 congstar Rechnung.md")); err != nil {
		t.Fatal("good mail not taken")
	}
	lg, _ := ts.server.paperLoad("alex")
	skipped := slices.Sorted(slices.Values(lg.Skip))
	if strings.Join(skipped, ",") != "42:5,42:6,42:7" || lg.MailError != "" {
		t.Fatalf("skip: %v %q", lg.Skip, lg.MailError)
	}
	f.mu.Lock()
	f.cmds = nil
	f.mu.Unlock()
	ts.server.paperRun(context.Background(), alex, m, true, time.Now())
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "UID FETCH 5") || strings.HasPrefix(c, "UID FETCH 6") || strings.HasPrefix(c, "UID FETCH 7") {
			t.Fatalf("fetched again: %s", c)
		}
	}
}

// A panic in a background run is logged, never the end of titand.
func TestPaperRunSurvivesPanic(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true}})
	inbox := filepath.Join(home, paperInbox)
	os.MkdirAll(inbox, 0o755)
	old := time.Now().Add(-5 * time.Minute)
	os.WriteFile(filepath.Join(inbox, "a.jpg"), []byte("x"), 0o644)
	os.Chtimes(filepath.Join(inbox, "a.jpg"), old, old)
	look := docsLook
	t.Cleanup(func() { docsLook = look })
	docsLook = func(context.Context, *Server, []byte) (string, error) { panic("kaputt") }
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	if !ts.server.paperStartRun(alex, m, false, time.Now()) {
		t.Fatal("not started")
	}
	waitDocs(t, ts, "alex")
	if !ts.server.paperStartRun(alex, m, false, time.Now()) {
		t.Fatal("flag not reset after the panic")
	}
	waitDocs(t, ts, "alex")
}
