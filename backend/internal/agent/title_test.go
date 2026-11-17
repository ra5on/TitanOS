package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// 1.09 (export of 17 Nov 2026 headed "Ist zu Hause alles zu? Prüfe
// Fenster, Türen, Schlösser und …"): a title of before 1.07 that was cut
// with " …" is shown as the whole first question – in the list, the
// export and messages – until the model has written a short one. The
// stored title stays, so the page can still ask for that once.
func TestCutTitleOfBefore107IsShownWhole(t *testing.T) {
	question := "Ist zu Hause alles zu? Prüfe Fenster, Türen, Schlösser und die Alarmanlage in Home Assistant."
	cut := "Ist zu Hause alles zu? Prüfe Fenster, Türen, Schlösser und …"
	at := time.Date(2026, 11, 17, 18, 5, 0, 0, time.UTC)
	chat := &Chat{ID: NewID(), Title: cut, Created: at, Updated: at, Entries: []Entry{
		{ID: NewID(), Role: "user", Content: "[Anhang: grundriss.pdf]\n\n" + question},
		{ID: NewID(), Role: "assistant", Content: "Alles ist geschlossen."},
	}}
	if !chat.LegacyTitle() || chat.ShownTitle() != question {
		t.Fatalf("shown %q", chat.ShownTitle())
	}
	if info := chat.Info(); info.Title != question {
		t.Fatalf("list line %q", info.Title)
	}
	// Named by the person: kept as it is.
	own := *chat
	own.TitleOwn = true
	if own.ShownTitle() != cut {
		t.Fatalf("own title changed: %q", own.ShownTitle())
	}
	// No question to take it from: at least without "…".
	empty := Chat{Title: cut}
	if got := empty.ShownTitle(); strings.Contains(got, "…") || got == "" {
		t.Fatalf("without question: %q", got)
	}
	// A title of the model is not touched.
	made := Chat{Title: "Haus zu? Fenster und Schlösser", TitleMade: true}
	if made.ShownTitle() != made.Title {
		t.Fatal("title of the model changed")
	}

	// The list as 1.06 wrote it: fixed when it is read, once, on disk too.
	dir := t.TempDir()
	s := &Store{Open: func() (*os.Root, error) { return os.OpenRoot(dir) }}
	if err := s.write("alex", "chats/"+chat.ID+".json", chat); err != nil {
		t.Fatal(err)
	}
	if err := s.write("alex", "chats.json", []ChatInfo{{ID: chat.ID, Title: cut, Created: at, Updated: at}}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Chats("alex")
	if err != nil || len(list) != 1 || list[0].Title != question {
		t.Fatalf("list %+v %v", list, err)
	}
	var stored []ChatInfo
	if err := s.read("alex", "chats.json", &stored, maxIndexFile); err != nil || stored[0].Title != question {
		t.Fatalf("stored list %+v %v", stored, err)
	}
	read, err := s.Chat("alex", chat.ID)
	if err != nil || read.Title != cut {
		t.Fatalf("the chat keeps its stored title: %v %v", read, err)
	}
}
