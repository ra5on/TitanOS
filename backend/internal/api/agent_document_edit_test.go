package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/office"
)

// PLAN 75-12: the agent changes a cell of an existing workbook, only after
// the person approves; the old version stays as .prev.
func TestAgentDocumentEditChangesACell(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	dir := ts.withShare()
	book, err := office.XLSX([]office.Sheet{{Name: "Kosten", Rows: [][]any{{"Posten", "Betrag"}, {"Miete", 900}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kosten.xlsx"), book, 0o644); err != nil {
		t.Fatal(err)
	}
	fake.replies = []string{
		toolCall("document_edit", map[string]any{"root": "share/Docs", "path": "kosten.xlsx",
			"cells": []map[string]any{{"sheet": "Kosten", "cell": "B2", "value": "950", "type": "number"}}}),
		`{"role":"assistant","content":"Geändert."}`,
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Miete auf 950"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	chat := waitChat(t, ts, ts.client, started.ID)
	if chat.State != agent.StateWaiting || chat.Pending == nil || chat.Pending.Tool != "document_edit" {
		t.Fatalf("not waiting: %s %+v", chat.State, chat.Pending)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "kosten.xlsx")); string(data) != string(book) {
		t.Fatal("changed before approval")
	}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/decide", map[string]any{"id": chat.Pending.ID, "approve": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("approve: %d %s", res.StatusCode, body)
	}
	waitChat(t, ts, ts.client, chat.ID)
	data, err := os.ReadFile(filepath.Join(dir, "kosten.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := office.Text("kosten.xlsx", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "950") || strings.Contains(text, "900") {
		t.Fatalf("cell not changed: %s", text)
	}
	if prev, _ := os.ReadFile(filepath.Join(dir, "kosten.xlsx.prev")); string(prev) != string(book) {
		t.Fatal("old version not kept as .prev")
	}
}

// A document_edit that cannot work is refused before any card is shown.
func TestDocumentEditRefusesWrongInput(t *testing.T) {
	book, _ := office.XLSX([]office.Sheet{{Name: "A", Rows: [][]any{{"x"}}}})
	cases := map[string]editRequest{
		"texts for xlsx": {Path: "a.xlsx", Texts: []office.TextEdit{{Find: "x", Replace: "y", Count: 1}}},
		"pdf":            {Path: "a.pdf", Cells: []office.CellEdit{{Sheet: "A", Cell: "A1", Value: "1"}}},
		"missing cell":   {Path: "a.xlsx", Cells: []office.CellEdit{{Sheet: "A", Cell: "Z99", Value: "1"}}},
	}
	for name, req := range cases {
		if _, err := req.apply(book); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
