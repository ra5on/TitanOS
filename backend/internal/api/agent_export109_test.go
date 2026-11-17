package api

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// The export of 17 Nov 2026 again (1.09): the steps in words as in the
// chat, marks drawn, no "?" in the PDF.
func TestAgentExportStepsInWords(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetAll})
	at := time.Date(2026, 11, 17, 18, 5, 0, 0, time.UTC)
	chat := &agent.Chat{ID: agent.NewID(), Title: "Automationen in Home Assistant", TitleMade: true, Created: at, Updated: at, State: agent.StateIdle, Entries: []agent.Entry{
		{ID: "u", Role: "user", Content: "wieviele automationen habe ich"},
		{ID: "s1", Role: "tool", Tool: "ha_entities", Args: json.RawMessage(`{"domain":"automation"}`), OK: true, Result: `{"total":0}`},
		{ID: "s2", Role: "tool", Tool: "files_search", Args: json.RawMessage(`{"root":"share/Backup","query":"automation"}`), Result: "Error: no such share"},
		{ID: "s3", Role: "tool", Tool: "ha_capabilities", Args: json.RawMessage(`{"entity":"automation.x"}`), Result: "Not run: the step limit of this task is reached."},
		{ID: "a", Role: "assistant", Content: "Du hast 0\u202fAutomation\u2011Entitäten in Home\u202fAssistant. Siehe „Entwickler\u2011Tools\u202f›\u202fEntitäten“."},
	}}
	if err := ts.server.agentStore().SaveChat("alex", chat); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("GET", "/api/v1/agent/chats/"+chat.ID+"/export", nil)
	md := string(body)
	for _, want := range []string{"**Du:** wieviele", "- ✓ Home Assistant gelesen · automation\n", "- ✗ Dateien gesucht · Backup · automation\n", "- – `ha_capabilities`\n", "**KI-Agent:**"} {
		if res.StatusCode != 200 || !strings.Contains(md, want) {
			t.Fatalf("export lacks %q:\n%s", want, md)
		}
	}
	if _, body := ts.request("GET", "/api/v1/agent/chats/"+chat.ID+"/export?lang=en", nil); !strings.Contains(string(body), "- ✓ Home Assistant read · automation") || !strings.Contains(string(body), "**You:**") {
		t.Fatalf("english export:\n%s", body)
	}
	res, body = ts.request("GET", "/api/v1/agent/chats/"+chat.ID+"/export?format=pdf", nil)
	if res.StatusCode != 200 {
		t.Fatalf("pdf: %d", res.StatusCode)
	}
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed")
	}
	file := filepath.Join(t.TempDir(), "chat.pdf")
	os.WriteFile(file, body, 0o600)
	out, err := exec.Command(tool, "-enc", "UTF-8", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(out)), " ")
	for _, want := range []string{"✓ Home Assistant gelesen", "✗ Dateien gesucht", "0 Automation-Entitäten in Home Assistant", "„Entwickler-Tools › Entitäten“"} {
		if !strings.Contains(text, want) {
			t.Errorf("pdf lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "?") {
		t.Errorf("a ? in the pdf:\n%s", text)
	}
}

// The words of the export are those of the page: the same tools have a
// word (STEP_WORDS in lib/agent.ts) and the same texts (agent.step.*).
func TestExportStepWordsMatchThePage(t *testing.T) {
	web := filepath.Join("..", "..", "..", "web", "src", "lib")
	src, err := os.ReadFile(filepath.Join(web, "agent.ts"))
	if err != nil {
		t.Skip("web sources not here")
	}
	block := regexp.MustCompile(`(?s)const STEP_WORDS: Record<string, string> = \{(.*?)\};`).FindSubmatch(src)
	if block == nil {
		t.Fatal("STEP_WORDS not found")
	}
	page := map[string]string{}
	for _, m := range regexp.MustCompile(`(\w+): '(\w+)'`).FindAllSubmatch(block[1], -1) {
		page[string(m[1])] = string(m[2])
	}
	if len(page) != len(exportStepWords) {
		t.Errorf("page has %d tools with words, export %d", len(page), len(exportStepWords))
	}
	for tool, word := range page {
		if exportStepWords[tool] != word {
			t.Errorf("%s: page %q, export %q", tool, word, exportStepWords[tool])
		}
	}
	for _, lang := range []string{"de", "en"} {
		data, err := os.ReadFile(filepath.Join(web, "i18n", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var texts map[string]string
		if err := json.Unmarshal(data, &texts); err != nil {
			t.Fatal(err)
		}
		for _, word := range page {
			if got := exportTexts[lang][word]; got != texts["agent.step."+word] {
				t.Errorf("%s %s: export %q, page %q", lang, word, got, texts["agent.step."+word])
			}
		}
	}
}
