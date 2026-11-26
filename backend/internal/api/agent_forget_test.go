package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/protect"
)

// A-42 (owner 16 Nov 2026, "Mitlöschen"): deleting an account deletes the
// agent's data of it – chats, memory, skills, schedules, its own projects,
// notes, its folder in KI-Agent – and nothing that names it stays behind.
// A shared project goes to a member (here the owner of the NAS among the
// members), with the members' chats in it. A new account of the same name
// inherits nothing.
func TestArchivedAgentDeletionHelper(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	for _, name := range []string{"maria", "ben"} {
		if _, err := ts.users.Create(name, testPassword, auth.RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"maria", "ben"}
	settings["reactAs"] = "maria"
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/features", map[string]any{"emergency": map[string]any{"enabled": true, "persons": []map[string]any{{"account": "maria", "waitDays": 3, "shares": []string{"Docs"}}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("emergency: %d %s", res.StatusCode, body)
	}
	if _, err := ts.server.protect().emergency.Ask(&protect.Person{Account: "maria", WaitDays: 3, Shares: []string{"Docs"}}, "maria", "Notfall", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	store := ts.server.agentStore()
	now := time.Now()
	shared := agent.Project{ID: agent.NewID(), Name: "Urlaub", Created: now, Members: []string{"ben", "alex"}}
	alone := agent.Project{ID: agent.NewID(), Name: "Privat", Created: now}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.SaveProjects("maria", []agent.Project{shared, alone}))
	chat := func(user, project string) *agent.Chat {
		c := &agent.Chat{ID: agent.NewID(), Title: "Chat", Project: project, State: agent.StateIdle, Entries: []agent.Entry{{ID: agent.NewID(), Role: "user", Content: "x"}}}
		must(store.SaveChat(user, c))
		return c
	}
	chat("maria", shared.ID)
	chat("maria", "")
	benChat := chat("ben", "maria:"+shared.ID)
	alexChat := chat("alex", "maria:"+shared.ID)
	must(store.SaveMemories("maria", []agent.Memory{{ID: agent.NewID(), Text: "Mein HA ist die VM", At: now}}))
	must(store.SaveSkills("maria", []agent.Skill{{ID: agent.NewID(), Name: "Backup", When: "w", Steps: "s", Updated: now}}))
	must(store.SaveSchedules("maria", []agent.Schedule{{ID: agent.NewID(), Name: "Blumen", Task: "gießen", Kind: "reminder", Every: "daily", At: "08:00", Enabled: true, Created: now}}))
	// maria is a member of a project of ben.
	must(store.SaveProjects("ben", []agent.Project{{ID: agent.NewID(), Name: "Garten", Created: now, Members: []string{"maria"}}}))
	ts.server.agentSyncShare()
	folder := filepath.Join(mnt, "tank", agentShareName, "maria")
	must(os.MkdirAll(filepath.Join(folder, "Notizen", "Haus"), 0o700))
	must(os.WriteFile(filepath.Join(folder, "Notizen", "Haus", "Heizung.md"), []byte("Wartung im Mai"), 0o600))
	if slices.Index(store.ScheduleAccounts(), "maria") < 0 {
		t.Fatal("schedule not in RAM")
	}

	// The question before: what goes with the account.
	data := ts.server.agentDataOf("maria")
	want := agentAccountData{Available: true, Chats: 2, Projects: 2, HandedOver: 1, Memories: 1, Skills: 1, Schedules: 1, Notes: 1, Folder: true}
	if data != want {
		t.Fatalf("archive counts: %+v", data)
	}

	if res, body := ts.request("DELETE", "/api/v1/users/maria", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	// Only exercise the historical helpers explicitly in this archival test.
	ts.server.agentForgetSettings("maria")
	ts.server.agentForgetAccount("maria")
	// The settings name it no more as soon as the answer is there.
	if s := ts.server.agentSettings(); slices.Contains(s.Users, "maria") || s.ReactAs == "maria" || s.Features.Emergency.person("maria") != nil {
		t.Fatalf("settings still name the account: %v %q %+v", s.Users, s.ReactAs, s.Features.Emergency.Persons)
	}
	ts.server.jobs.Wait()

	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder in KI-Agent: %v", err)
	}
	if slices.Contains(store.Accounts(), "maria") || slices.Contains(store.ScheduleAccounts(), "maria") {
		t.Fatalf("store still has the account: %v %v", store.Accounts(), store.ScheduleAccounts())
	}
	if _, ok := ts.server.emergencyLatest("maria"); ok {
		t.Fatal("emergency request kept")
	}
	// The shared project went to alex (owner of the NAS among the
	// members), the chats of the members with it; ben keeps no member
	// named maria.
	alexProjects, _ := store.Projects("alex")
	if len(alexProjects) != 1 || alexProjects[0].ID != shared.ID || !slices.Equal(alexProjects[0].Members, []string{"ben"}) {
		t.Fatalf("handed over: %+v", alexProjects)
	}
	if c, _ := store.Chat("ben", benChat.ID); c.Project != "alex:"+shared.ID {
		t.Fatalf("ben's chat: %q", c.Project)
	}
	if c, _ := store.Chat("alex", alexChat.ID); c.Project != shared.ID {
		t.Fatalf("alex's chat: %q", c.Project)
	}
	if list, _ := store.Projects("ben"); len(list) != 1 || len(list[0].Members) != 0 {
		t.Fatalf("ben's project: %+v", list)
	}

	// A new account of the same name starts empty.
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "maria", "password": testPassword, "role": "admin"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	ts.server.agentForgetLeftovers("maria")
	data = ts.server.agentDataOf("maria")
	if data != (agentAccountData{Available: true}) {
		t.Fatalf("new account inherits: %s", body)
	}
	if got := ts.server.agentSharedProjects("maria"); len(got) != 0 {
		t.Fatalf("new account sees projects: %+v", got)
	}
	if ts.server.agentSettings().Features.Emergency.person("maria") != nil {
		t.Fatal("new account registered for the emergency access")
	}
	// It may use the agent again once allowed, and its files are kept.
	must(store.SaveMemories("maria", []agent.Memory{{ID: agent.NewID(), Text: "neu", At: now}}))
	if list, _ := store.Memories("maria"); len(list) != 1 {
		t.Fatalf("new account cannot keep files: %+v", list)
	}
}

// A run that ends after the deletion does not bring the folder back:
// the store refuses to make it again until the account is made anew.
func TestForgottenAccountIsNotMadeAgain(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	c := &agent.Chat{ID: agent.NewID(), Title: "Chat", State: agent.StateIdle}
	if err := store.SaveChat("gast", c); err != nil {
		t.Fatal(err)
	}
	if err := store.Forget("gast"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChat("gast", c); err == nil {
		t.Fatal("a late save made the folder again")
	}
	if slices.Contains(store.Accounts(), "gast") {
		t.Fatal("folder still there")
	}
	store.Unforget("gast")
	if err := store.SaveChat("gast", c); err != nil {
		t.Fatal(err)
	}
}

// Only administrators who may manage the account read the counts.
func TestAgentDataNeedsTheRightToManage(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.users.Create("ben", testPassword, auth.RoleAdmin)
	ts.users.Create("gast", testPassword, auth.RoleUser)
	ben := ts.newClient()
	ts.do(ben, "POST", "/api/v1/login", credentials{Name: "ben", Password: testPassword})
	if res, _ := ts.do(ben, "GET", "/api/v1/users/alex/agent-data", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin reads an owner: %d", res.StatusCode)
	}
	if res, _ := ts.do(ben, "GET", "/api/v1/users/gast/agent-data", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("admin reads a user: %d", res.StatusCode)
	}
	gast := ts.newClient()
	ts.do(gast, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(gast, "GET", "/api/v1/users/gast/agent-data", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user reads counts: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/users/nosuch/agent-data", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown account: %d", res.StatusCode)
	}
}

// An account another node of the cluster deleted takes its agent data
// along here too; one taken over anew from the cluster inherits nothing.
func TestClusterAccountChangePreservesArchivedChats(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.users.Create("maria", testPassword, auth.RoleUser)
	store := ts.server.agentStore()
	c := &agent.Chat{ID: agent.NewID(), Title: "Chat", State: agent.StateIdle}
	if err := store.SaveChat("maria", c); err != nil {
		t.Fatal(err)
	}
	before, _ := ts.users.Get("maria")
	if _, _, err := ts.users.Forget("maria"); err != nil {
		t.Fatal(err)
	}
	ts.server.clusterArea().clusterApplied(before, true, auth.User{}, true)
	ts.server.jobs.Wait()
	if list, err := store.Chats("maria"); err != nil || len(list) != 1 {
		t.Fatal("archived chat lost on deletion")
	}
	if _, err := ts.users.Create("maria", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	after, _ := ts.users.Get("maria")
	ts.server.clusterArea().clusterApplied(auth.User{}, false, after, false)
	if list, err := store.Chats("maria"); err != nil || len(list) != 1 {
		t.Fatal("archived chat lost on recreation")
	}
	if personalKey(before) == personalKey(after) {
		t.Fatal("replacement shares a personal Agent identity")
	}

}

// 1.11 (TECHNIK 5.7): the delete dialog counts the events and contacts of
// T-Kalender and Kontakte (groups not counted), also without the agent's
// volume; without a store of calendars the answer has no such keys.
func TestAgentDataCountsCalendarAndContacts(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	ts.guest("bea")
	for i, uid := range []string{"a", "b", "c"} {
		if _, err := pim.Put(pimstore.Calendar, "bea", pimstore.DefaultCalendar, uid+".ics", pimEvent(uid), pimstore.PutOptions{}); err != nil {
			t.Fatal(i, err)
		}
	}
	group := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:g\r\nFN:Familie\r\nX-ADDRESSBOOKSERVER-KIND:group\r\nEND:VCARD\r\n")
	for name, card := range map[string][]byte{"x.vcf": pimCard("x"), "g.vcf": group} {
		if _, err := pim.Put(pimstore.Contacts, "bea", pimstore.DefaultBook, name, card, pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var data agentAccountData
	res, body := ts.request("GET", "/api/v1/users/bea/agent-data", nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &data) != nil {
		t.Fatalf("agent-data: %d %s", res.StatusCode, body)
	}
	if data.Available || data.CalendarUnknown || data.CalendarEvents == nil || *data.CalendarEvents != 3 || data.Contacts == nil || *data.Contacts != 1 {
		t.Fatalf("counts: %s", body)
	}
	// An account without anything: 0, counted.
	_, body = ts.request("GET", "/api/v1/users/alex/agent-data", nil)
	if !strings.Contains(string(body), `"calendarEvents":0`) || !strings.Contains(string(body), `"contacts":0`) {
		t.Fatalf("owner: %s", body)
	}
	// A server without a store of calendars says nothing about them.
	plain := newTestServer(t)
	plain.setup()
	_, body = plain.request("GET", "/api/v1/users/alex/agent-data", nil)
	if strings.Contains(string(body), "calendar") || strings.Contains(string(body), `"contacts"`) {
		t.Fatalf("without a store: %s", body)
	}
}
