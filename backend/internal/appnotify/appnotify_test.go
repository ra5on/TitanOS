package appnotify

import (
	"strconv"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

func testHub(admins ...string) (*Hub, *time.Time) {
	h := New()
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }
	h.Admin = func(user string) bool {
		for _, a := range admins {
			if a == user {
				return true
			}
		}
		return false
	}
	return h, &now
}

func TestCategories(t *testing.T) {
	for _, c := range []struct {
		kind, tag, want string
	}{
		{webpush.KindNAS, "volume_degraded:Data", NAS},
		{webpush.KindMail, "mail:Privat", Mail},
		{webpush.KindAgent, "agent-123", Agent},
		{webpush.KindCamera, "nvr_person:Garten", NVR},
		{webpush.KindCamera, "calendar_reminder:calendar", Calendar},
	} {
		if got := Category(c.kind, webpush.Message{Tag: c.tag}); got != c.want {
			t.Errorf("%s %s: %s, want %s", c.kind, c.tag, got, c.want)
		}
	}
}

func TestAccountsAndAdmins(t *testing.T) {
	h, _ := testHub("alex")
	h.FromPush("", webpush.KindNAS, webpush.Message{Title: "Platte wird warm", Body: "52 °C", Tag: "disk_hot:sda", Severity: "warning"})
	h.FromPush("sam", webpush.KindMail, webpush.Message{Title: "Neue Mail", Tag: "mail:Privat", URL: "/mail"})
	h.FromPush("alex", webpush.KindAgent, webpush.Message{Title: "KI-Agent: fertig", Tag: "agent-1", URL: "/agent?chat=1"})

	alex, latest := h.List("alex", 0)
	if len(alex) != 2 || alex[0].Category != NAS || alex[0].Severity != "warning" || alex[1].Category != Agent || alex[1].App != webpush.AppAgent {
		t.Fatalf("alex: %+v", alex)
	}
	if latest != alex[1].ID {
		t.Fatalf("latest %s, newest %s", latest, alex[1].ID)
	}
	// sam is no administrator: only the own mail, never the NAS warning.
	sam, _ := h.List("sam", 0)
	if len(sam) != 1 || sam[0].Category != Mail || sam[0].Link != "/mail" || sam[0].Severity != "info" {
		t.Fatalf("sam: %+v", sam)
	}
	// After a marker only what is newer.
	after, reset := h.Since(alex[0].ID)
	if reset {
		t.Fatal("own marker taken as reset")
	}
	if rest, _ := h.List("alex", after); len(rest) != 1 || rest[0].Title != "KI-Agent: fertig" {
		t.Fatalf("after: %+v", rest)
	}
	h.Forget("alex")
	if rest, _ := h.List("alex", 0); len(rest) != 1 || rest[0].Category != NAS {
		t.Fatalf("forgotten keeps only the messages to all admins: %+v", rest)
	}
}

func TestMarkers(t *testing.T) {
	h, _ := testHub()
	h.Add("alex", Item{Title: "a"})
	own, _ := h.List("alex", 0)
	if seq, reset := h.Since(""); seq != 0 || reset {
		t.Fatal("empty marker")
	}
	// A marker of an earlier start of titand (another epoch), from the
	// future of this one or unreadable: everything again, with reset.
	future := h.Epoch() + "-" + strconv.FormatInt(h.Seq(own[0])+1_000_000, 10)
	for _, m := range []string{"1-5", future, "nonsense", h.Epoch() + "-x"} {
		if seq, reset := h.Since(m); seq != 0 || !reset {
			t.Errorf("%q: %d %v", m, seq, reset)
		}
	}
	if seq, reset := h.Since(own[0].ID); seq != h.Seq(own[0]) || reset {
		t.Fatalf("own: %d %v", seq, reset)
	}
	// The numbers follow the clock: a gap tells nothing about how many
	// messages others got.
	if h.Seq(own[0]) < 1_000_000 {
		t.Fatalf("seq %d is a counter, not the clock", h.Seq(own[0]))
	}
}

func TestBounds(t *testing.T) {
	h, now := testHub("alex")
	h.Add("alex", Item{Title: "old"})
	*now = now.Add(keepFor + time.Minute)
	for i := range maxPerAccount + 20 {
		h.Add("alex", Item{Title: strconv.Itoa(i)})
		h.Add("", Item{Title: "all " + strconv.Itoa(i)})
	}
	list, _ := h.List("alex", 0)
	if len(list) != maxPerAccount {
		t.Fatalf("%d items", len(list))
	}
	for _, it := range list {
		if it.Title == "old" {
			t.Fatal("older than keepFor")
		}
	}
	if list[len(list)-1].Title != "all "+strconv.Itoa(maxPerAccount+19) {
		t.Fatalf("newest last: %+v", list[len(list)-1])
	}
	if n := len(h.per["alex"]); n != maxPerAccount {
		t.Fatalf("kept %d", n)
	}
}

func TestStreams(t *testing.T) {
	h, _ := testHub("alex")
	alex, stopAlex := h.Subscribe("alex")
	sam, stopSam := h.Subscribe("sam")
	defer stopSam()
	h.Add("", Item{Title: "for admins"})
	h.Add("sam", Item{Title: "for sam"})
	if got := <-alex.C; got.Title != "for admins" {
		t.Fatalf("alex got %+v", got)
	}
	if got := <-sam.C; got.Title != "for sam" {
		t.Fatalf("sam got %+v", got)
	}
	select {
	case got := <-alex.C:
		t.Fatalf("alex got sam's %+v", got)
	default:
	}
	// A stream that does not read is told to read the list again.
	for range subBuffer + 1 {
		h.Add("alex", Item{Title: "x"})
	}
	if !alex.Lost.Load() {
		t.Fatal("overflow not marked")
	}
	stopAlex()
	for range alex.C {
		// What waited is still read; then the channel ends.
	}
	// The oldest of too many streams ends.
	subs := make([]*Sub, 0, maxStreams+1)
	for range maxStreams + 1 {
		s, stop := h.Subscribe("kim")
		defer stop()
		subs = append(subs, s)
	}
	if _, open := <-subs[0].C; open {
		t.Fatal("oldest stream not ended")
	}
	if len(h.subs["kim"]) != maxStreams {
		t.Fatalf("%d streams", len(h.subs["kim"]))
	}
}
