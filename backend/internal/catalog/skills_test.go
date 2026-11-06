package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const goodSkill = `name: Rechnungen nach Steuerjahr
description: Sortiert Belege in Steuer/<Jahr>
author: ra5on
when: Wenn Rechnungen eines Jahres gesucht werden
tools: [files_search, note_write]
steps: |
  1. Mit files_search nach Rechnungen suchen.
  2. Eine Notiz schreiben (note_write).
`

// Skills come with the apps: from the skills/ folder of the official
// archive and of an own source, with the name of their source; malformed
// skills are skipped.
func TestSkillsFromSources(t *testing.T) {
	srv, _ := officialServer(t, map[string]string{
		"TitanOS-Apps-abc/beispiel/compose.yaml":           withMeta,
		"TitanOS-Apps-abc/skills/steuer/skill.yaml":        goodSkill,
		"TitanOS-Apps-abc/skills/kaputt/skill.yaml":        "name: [nicht: ein text",
		"TitanOS-Apps-abc/skills/ohne-schritte/skill.yaml": "name: Leer\nwhen: nie\n",
		"TitanOS-Apps-abc/skills/boeses-tool/skill.yaml":   "name: X\nwhen: y\nsteps: z\ntools: [\"rm -rf\"]\n",
		"TitanOS-Apps-abc/skills/Gross_Name/skill.yaml":    goodSkill,
	})
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball(map[string]string{"meine-main/skills/strom/skill.yaml": "name: Strombericht\nwhen: Montags\nsteps: 1. ha_statistics lesen\ntools: [ha_statistics]\n"}))
	}))
	t.Cleanup(own.Close)
	m := NewManager(config.Open(t.TempDir()))
	m.SetClient(srv.Client())
	m.SetOfficialURL(srv.URL + "/ra5on/TitanOS-Apps")
	m.Load()
	m.Refresh(context.Background())
	// A source with skills only is a source, not an empty one.
	if err := m.Add(context.Background(), "Meine Skills", own.URL+"/skills.tar.gz"); err != nil {
		t.Fatalf("add a source with skills only: %v", err)
	}
	got := m.Skills()
	if len(got) != 2 {
		t.Fatalf("skills: %+v", got)
	}
	s := got[0]
	if s.ID != "steuer" || s.Source != BuiltinID || s.SourceName != "TitanOS-Apps" || s.Name != "Rechnungen nach Steuerjahr" || len(s.Tools) != 2 || s.Author != "ra5on" || s.Steps == "" {
		t.Fatalf("official skill: %+v", s)
	}
	if got[1].SourceName != "Meine Skills" || got[1].ID != "strom" {
		t.Fatalf("own source: %+v", got[1])
	}
	// The apps are unchanged.
	if _, apps := m.Status(); len(apps) != 1 {
		t.Fatalf("apps: %d", len(apps))
	}
}
