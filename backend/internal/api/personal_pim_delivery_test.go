package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Mail, calendars, permissions, workflow broker and external-effect receipts
// are real production code. Only the inference response and mailservers are fixtures.
func TestPersonalDeliveryMailCalendarDraftAndApprovedSend(t *testing.T) {
	ts, provider, m := newMailServer(t)
	s := ts.server
	pim, _, _ := pimTestStore(t)
	s.opts.PIM = pim
	s.opts.PersonalDataDir = t.TempDir()
	s.initPersonalTools()
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	provider.Deliver("INBOX", fixtureDelivery)
	src, err := m.ReadQuiet(t.Context(), "alex", box, "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	cal := "lieferung"
	if _, err = pim.CreateCollection(store.Calendar, "alex", cal, store.Meta{Name: "Privat"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pim.Put(store.Calendar, "alex", cal, "busy.ics", pimEvent("Privater Titel"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tools := personalMailTools{m, func(_ context.Context, _ auth.User, cloud bool, task, prompt string, data any) (string, error) {
		calls++
		ref := string(jsonPIM(data))
		if cloud || !strings.Contains(ref, "30. November") || strings.Contains(ref, "Privater Titel") {
			t.Fatal("unexpected data scope")
		}
		if strings.Contains(task, "appointment") {
			return `{"title":"Liefertermin","start":"2026-11-30T10:00","end":"2026-11-30T11:00"}`, nil
		}
		if !strings.Contains(ref, "prior_step_result") || !strings.Contains(ref, cal) && !strings.Contains(ref, "Privat") {
			t.Fatal("calendar context not sent to reply", ref)
		}
		return "Am 30. November um 10 Uhr passt es.", nil
	}}
	for _, tool := range tools.tools() {
		s.personal.tools[tool.ID] = tool
	}
	rights := map[string]string{"mail.event": "auto", "calendar.free": "auto", "mail.reply": "auto", "mail.send": "auto"}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": rights})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": rights})
	sources := []pa.Source{{Kind: "mail", ID: "m", Ref: map[string]string{"account": box, "folder": "INBOX", "uid": "1", "messageId": src.MessageID, "date": src.Date.Format(time.RFC3339Nano)}}, {Kind: "calendar.range", ID: cal, Ref: map[string]string{"collection": cal, "start": "2026-11-30T09:00:00Z", "end": "2026-11-30T12:00:00Z"}}}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/skills/delivery/run", map[string]any{"sources": sources, "requestId": "liefertermin", "prompt": "Liefertermin prüfen"})
	read := func() pa.Job { return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil) }
	for i := 0; i < 3; i++ {
		workOne(t, s)
		j = read()
		if j.CompletedSteps != i+1 || j.Result == nil || len(provider.Sent()) != 0 {
			t.Fatalf("step %d %+v", i, j)
		}
		if i == 2 {
			var result struct {
				Draft struct{ Fields map[string]any }
			}
			if json.Unmarshal(j.Result.Data, &result) != nil || result.Draft.Fields["a"] != box {
				t.Fatal("no native draft")
			}
		}
	}
	s.initPersonalTools() // Browser closed and IP service restarted after the saved draft.
	workOne(t, s)
	j = read()
	if j.Phase != "approval" || j.Preview == nil || !strings.Contains(j.Preview.Text, "vendor@example.test") || !strings.Contains(j.Preview.Text, "10 Uhr passt") || calls != 2 {
		t.Fatalf("%+v", j)
	}
	personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	workOne(t, s)
	j = read()
	if j.Status != "done" || len(j.Receipts) != 4 || len(provider.Sent()) != 1 {
		t.Fatal(j)
	}
}
