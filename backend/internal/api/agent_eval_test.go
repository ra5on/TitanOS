package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// TestAgentEval is the measuring stand of the agent (PLAN 55 point 9): a
// fixed set of typical tasks against a real Ollama server, each checked by
// what it left behind (answer or file), not by how it got there. It runs
// only with TITANOS_OLLAMA (and optionally TITANOS_OLLAMA_MODEL,
// TITANOS_EVAL_RUNS, TITANOS_EVAL_ONLY) and works on test data only.
//
//	TITANOS_OLLAMA=http://192.168.10.5:11434 TITANOS_OLLAMA_MODEL=gemma4:12b \
//	  go test ./internal/api -run TestAgentEval -v -timeout 60m
func TestAgentEval(t *testing.T) {
	address := os.Getenv("TITANOS_OLLAMA")
	if address == "" {
		t.Skip("TITANOS_OLLAMA not set")
	}
	model := os.Getenv("TITANOS_OLLAMA_MODEL")
	if model == "" {
		model = "gpt-oss:20b"
	}
	runs := 1
	if n, err := strconv.Atoi(os.Getenv("TITANOS_EVAL_RUNS")); err == nil && n > 0 {
		runs = n
	}
	only := os.Getenv("TITANOS_EVAL_ONLY")
	client, err := agent.NewOllama(address)
	if err != nil {
		t.Fatal(err)
	}
	total, passed := 0, 0
	var report []string
	for _, task := range agentEvalTasks() {
		if only != "" && !strings.Contains(only, task.name) {
			continue
		}
		ok := 0
		var took time.Duration
		var steps, maxPrompt int
		for i := 0; i < runs; i++ {
			res := runAgentEval(t, client, model, task)
			took += res.took
			steps += res.steps
			if res.maxPrompt > maxPrompt {
				maxPrompt = res.maxPrompt
			}
			if res.err == "" {
				ok++
			} else {
				t.Logf("%s run %d failed: %s", task.name, i+1, res.err)
			}
		}
		total += runs
		passed += ok
		report = append(report, fmt.Sprintf("%-12s %d/%d  %5.1fs  %4.1f steps  max prompt %d tokens", task.name, ok, runs, took.Seconds()/float64(runs), float64(steps)/float64(runs), maxPrompt))
	}
	t.Logf("model %s\n%s\nscore %d/%d", model, strings.Join(report, "\n"), passed, total)
}

type agentEvalTask struct {
	name    string
	ask     string
	context int // num_ctx; 0 keeps Ollama's setting
	// history is put in front of the question (long chats).
	history func() []agent.Entry
	setup   func(dir string)
	check   func(dir, answer string, chat *agent.Chat) string
}

type agentEvalResult struct {
	err              string
	took             time.Duration
	steps, maxPrompt int
}

func agentEvalTasks() []agentEvalTask {
	write := func(dir, rel, text string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(text), 0o644)
	}
	has := func(answer string, want ...string) string {
		low := strings.ToLower(answer)
		for _, w := range want {
			if !strings.Contains(low, strings.ToLower(w)) {
				return fmt.Sprintf("answer lacks %q: %.300s", w, answer)
			}
		}
		return ""
	}
	fileHas := func(path string, re string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return "file missing: " + filepath.Base(path)
		}
		if !regexp.MustCompile(re).Match(data) {
			return fmt.Sprintf("%s does not match %s: %.200s", filepath.Base(path), re, data)
		}
		return ""
	}
	return []agentEvalTask{
		{name: "shares", ask: "Welche Freigaben gibt es auf dem NAS?",
			check: func(_, a string, _ *agent.Chat) string { return has(a, "Docs") }},
		{name: "find", ask: "Wie hoch war die Rechnung der Stadtwerke im März? Sie liegt irgendwo in Docs.",
			setup: func(d string) {
				write(d, "Rechnungen/2026/03/stadtwerke.txt", "Stadtwerke Musterstadt\nRechnung März 2026\nBetrag: 84,20 EUR\n")
				write(d, "Rechnungen/2026/02/stadtwerke.txt", "Stadtwerke Musterstadt\nRechnung Februar 2026\nBetrag: 79,10 EUR\n")
				write(d, "Rechnungen/2026/03/telefon.txt", "Telefon März: 29,99 EUR\n")
			},
			check: func(_, a string, _ *agent.Chat) string {
				if !strings.Contains(a, "84,20") && !strings.Contains(a, "84.20") && !strings.Contains(a, "84,2 ") {
					return has(a, "84,20")
				}
				return ""
			}},
		{name: "create", ask: "Leg in Docs eine Datei einkauf.md an mit einer Liste: Milch, Brot, Eier.",
			check: func(d, _ string, _ *agent.Chat) string {
				return fileHas(filepath.Join(d, "einkauf.md"), `(?is)milch.*brot.*eier`)
			}},
		{name: "count", ask: "Wie viele .txt-Dateien liegen in Docs/Notizen? Schreib nur die Zahl in eine neue Datei Docs/anzahl.txt.",
			setup: func(d string) {
				for _, n := range []string{"a.txt", "b.txt", "c.txt", "bild.jpg", "liste.md"} {
					write(d, "Notizen/"+n, "x")
				}
			},
			check: func(d, _ string, _ *agent.Chat) string { return fileHas(filepath.Join(d, "anzahl.txt"), `^\s*3\s*$`) }},
		{name: "search", ask: "In welcher Datei in Docs steht das Wort Sonnenblume? Nenn den Dateinamen.",
			setup: func(d string) {
				write(d, "Garten/plan.txt", "Beet 1: Tomaten\nBeet 2: Sonnenblume und Kürbis\n")
				write(d, "Garten/werkzeug.txt", "Spaten, Harke\n")
				write(d, "Urlaub/packliste.txt", "Sonnencreme\n")
			},
			check: func(_, a string, _ *agent.Chat) string { return has(a, "plan.txt") }},
		{name: "rename", ask: "Benenne in Docs/Fotos alle Dateien, die mit IMG_ anfangen, um: statt IMG_ soll Urlaub_ davor stehen.",
			setup: func(d string) {
				for _, n := range []string{"IMG_001.jpg", "IMG_002.jpg", "IMG_003.jpg", "andere.jpg"} {
					write(d, "Fotos/"+n, "x")
				}
			},
			check: func(d, _ string, _ *agent.Chat) string {
				for _, n := range []string{"Urlaub_001.jpg", "Urlaub_002.jpg", "Urlaub_003.jpg", "andere.jpg"} {
					if _, err := os.Stat(filepath.Join(d, "Fotos", n)); err != nil {
						return "missing Fotos/" + n
					}
				}
				return ""
			}},
		{name: "sort", ask: "Räum Docs/Eingang auf: PDFs in den Unterordner PDF, Bilder (.jpg) in Bilder, Textdateien (.txt) in Texte. Die Ordner gibt es noch nicht.",
			setup: func(d string) {
				for _, n := range []string{"a.pdf", "b.pdf", "c.jpg", "d.jpg", "e.txt", "f.txt", "g.pdf"} {
					write(d, "Eingang/"+n, "x")
				}
			},
			check: func(d, _ string, _ *agent.Chat) string {
				for _, n := range []string{"PDF/a.pdf", "PDF/b.pdf", "PDF/g.pdf", "Bilder/c.jpg", "Bilder/d.jpg", "Texte/e.txt", "Texte/f.txt"} {
					if _, err := os.Stat(filepath.Join(d, "Eingang", n)); err != nil {
						return "missing Eingang/" + n
					}
				}
				return ""
			}},
		{name: "sum", ask: "Zähl die Beträge aller Rechnungen in Docs/Rechnungen zusammen und schreib nur die Summe in Euro (z. B. 12,34) in eine neue Datei Docs/summe.txt.",
			setup: func(d string) {
				for i, v := range []string{"12,50", "40,00", "7,25", "100,10", "3,15", "55,00"} {
					write(d, fmt.Sprintf("Rechnungen/r%d.txt", i+1), fmt.Sprintf("Rechnung Nr. %d\nBetrag: %s EUR\n", 100+i, v))
				}
			},
			check: func(d, _ string, _ *agent.Chat) string { return fileHas(filepath.Join(d, "summe.txt"), `218[,.]0?0?`) }},
		{name: "bigdir", context: 16384, ask: "Welche Datei in Docs/Archiv ist die größte? Nenn nur ihren Namen.",
			setup: func(d string) {
				for i := 0; i < 400; i++ {
					size := 100 + (i*37)%900
					if i == 287 {
						size = 50000
					}
					write(d, fmt.Sprintf("Archiv/scan_%04d_dokument_eingang.pdf", i), strings.Repeat("x", size))
				}
			},
			check: func(_, a string, _ *agent.Chat) string { return has(a, "scan_0287") }},
		{name: "longchat", context: 8192, ask: "Neue Frage: Welche Freigaben gibt es auf dem NAS? Und wie heißt mein Hund (steht ganz am Anfang)?",
			history: func() []agent.Entry {
				filler := strings.Repeat("Protokollzeile: Dienst läuft, keine Auffälligkeiten, Temperatur normal. ", 45)
				list := []agent.Entry{{ID: agent.NewID(), Role: "user", Content: "Merk dir für diesen Chat: Mein Hund heißt Bruno. Zeig mir dann die Systemprotokolle."},
					{ID: agent.NewID(), Role: "assistant", Content: "Gut, Bruno. Ich sehe mir die Protokolle an."}}
				for i := 0; i < 12; i++ {
					list = append(list,
						agent.Entry{ID: agent.NewID(), Role: "assistant", Calls: []agent.ToolCall{evalCall("system_logs")}},
						agent.Entry{ID: agent.NewID(), Role: "tool", Tool: "system_logs", OK: true, Result: filler},
						agent.Entry{ID: agent.NewID(), Role: "user", Content: fmt.Sprintf("Und Teil %d?", i+2)},
						agent.Entry{ID: agent.NewID(), Role: "assistant", Content: "Auch hier ist alles unauffällig."})
				}
				return list
			},
			check: func(_, a string, _ *agent.Chat) string {
				if e := has(a, "Docs"); e != "" {
					return e
				}
				return has(a, "Bruno")
			}},
	}
}

func evalCall(name string) agent.ToolCall {
	var c agent.ToolCall
	c.Function.Name = name
	c.Function.Arguments = []byte("{}")
	return c
}

func runAgentEval(t *testing.T, client *agent.Ollama, model string, task agentEvalTask) agentEvalResult {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	store := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(store) }
	if task.setup != nil {
		task.setup(dir)
	}
	user := mustUser(t, ts, "alex")
	settings := ts.server.agentSettings()
	box := ts.server.newAgentToolbox(user, settings)
	chat := &agent.Chat{ID: agent.NewID(), Origin: "web"}
	if task.history != nil {
		chat.Entries = task.history()
	}
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), Role: "user", Content: task.ask})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	r := &agent.Runner{Ollama: client, Model: model, Tools: box, MaxSteps: 15, Context: task.context,
		System: ts.server.agentPrompt(ctx, user, settings, chat, box)}
	agentEvalTune(r)
	start := time.Now()
	runErr := r.Run(ctx, chat)
	// Steps that wait for the person are approved, as the person would.
	for i := 0; i < 6 && runErr == nil && chat.Pending != nil; i++ {
		runErr = r.Decide(ctx, chat, chat.Pending.ID, true)
	}
	res := agentEvalResult{took: time.Since(start)}
	answer := ""
	for _, e := range chat.Entries {
		switch e.Role {
		case "tool":
			res.steps++
		case "assistant":
			if e.Stats != nil && e.Stats.PromptTokens > res.maxPrompt {
				res.maxPrompt = e.Stats.PromptTokens
			}
			if strings.TrimSpace(e.Content) != "" {
				answer = e.Content
			}
		}
	}
	if runErr != nil {
		res.err = "run: " + runErr.Error()
		return res
	}
	res.err = task.check(dir, answer, chat)
	if res.err != "" && testing.Verbose() {
		for _, e := range chat.Entries[len(chat.Entries)-min(len(chat.Entries), 12):] {
			t.Logf("  %s %s %.160s %.160s", e.Role, e.Tool, e.Content, e.Result)
		}
	}
	return res
}
