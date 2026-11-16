package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/docindex"
)

// Review 1.02: the name of a reminder was cut at 79 characters plus "…",
// but a schedule takes 80 bytes: a long or umlaut-rich file name was always
// refused, and in "Gleich als Erinnerung" the deadline was never planned.
func TestDocsRemindLongName(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	now := time.Now().In(ts.server.agentLocation())
	date := now.AddDate(0, 1, 0)
	for _, p := range []string{
		"Dokumente/Verträge/2026-11-03 Mobilfunkvertrag Kündigungsbestätigung Telekom Deutschland GmbH.pdf",
		"Dokumente/Kündigung Größe Übergabe.pdf",
	} {
		item := deadlineItem("share/Docs", p, 1, docindex.Deadline{Kind: docindex.KindCancel, Date: date, Snippet: "Kündigung bis"}, "", "")
		if err := ts.server.docsRemind("alex", item, []int{14}, now); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	list, err := ts.server.agentStore().Schedules("alex")
	if err != nil || len(list) != 2 {
		t.Fatalf("schedules: %v %d", err, len(list))
	}
	for _, sc := range list {
		if len(sc.Name) > 80 || strings.Contains(sc.Name, "…") || !strings.HasPrefix(sc.Name, "Frist: ") {
			t.Fatalf("name %q", sc.Name)
		}
	}
}
