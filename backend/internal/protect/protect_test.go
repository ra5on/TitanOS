package protect

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var t0 = time.Date(2026, 11, 6, 10, 42, 0, 0, time.UTC)

// A stream of renames to .locked from one client starts an incident at the
// fifth; the other files of the share join it; it stops after Quiet.
func TestDetectorKnownExtension(t *testing.T) {
	d := NewDetector(DefaultThresholds())
	var inc *Incident
	for i := range 5 {
		from := fmt.Sprintf("Post/Brief%d.pdf", i)
		k := ClassifyRename(from, from+".locked")
		if k != KindKnownRename {
			t.Fatalf("kind %v", k)
		}
		inc = d.Observe("Documents", "pid:7", k, Affected{Was: from, Now: from + ".locked"}, t0.Add(time.Duration(i)*time.Second))
		if i < 4 && inc != nil {
			t.Fatalf("started at %d", i)
		}
	}
	if inc == nil || inc.Reason != "known-extension" || len(inc.Files) != 5 || inc.Files[0].Was != "Post/Brief0.pdf" {
		t.Fatalf("incident: %+v", inc)
	}
	// Another client on the same share adds to the incident.
	if d.Observe("Documents", "pid:8", KindKnownRename, Affected{Was: "a.xlsx", Now: "a.xlsx.locked"}, t0.Add(10*time.Second)) != nil {
		t.Fatal("second incident")
	}
	if got, _ := d.Snapshot("Documents"); len(got.Files) != 6 {
		t.Fatalf("files: %d", len(got.Files))
	}
	if len(d.Tick(t0.Add(time.Minute))) != 0 {
		t.Fatal("stopped too early")
	}
	stopped := d.Tick(t0.Add(10*time.Second + 2*time.Minute))
	if len(stopped) != 1 || stopped[0].Count != 6 {
		t.Fatalf("stopped: %+v", stopped)
	}
	if d.Active("Documents") {
		t.Fatal("still active")
	}
}

// Normal work does not alarm: saving Office files over temporary names,
// renames between normal extensions, slow renames, other shares.
func TestDetectorNormalWork(t *testing.T) {
	d := NewDetector(DefaultThresholds())
	for i := range 200 {
		at := t0.Add(time.Duration(i) * 100 * time.Millisecond)
		for _, r := range [][2]string{
			{fmt.Sprintf("~WRL%04d.tmp", i), fmt.Sprintf("Bericht%d.docx", i)},
			{fmt.Sprintf("Bild%d.jpeg", i), fmt.Sprintf("Bild%d.jpg", i)},
			{fmt.Sprintf("x%d.docx", i), fmt.Sprintf("x%d.docx.sb-1234-abc", i)},
			{fmt.Sprintf("film%d.part", i), fmt.Sprintf("film%d.mkv", i)},
		} {
			if inc := d.Observe("Documents", "pid:1", ClassifyRename(r[0], r[1]), Affected{Was: r[0], Now: r[1]}, at); inc != nil {
				t.Fatalf("alarm on %v: %+v", r, inc)
			}
		}
	}
	// 29 unknown renames in a minute stay below the threshold of 30.
	for i := range 29 {
		from := fmt.Sprintf("d%d.pdf", i)
		if d.Observe("Documents", "pid:2", ClassifyRename(from, from+".qwerty"), Affected{Was: from, Now: from + ".qwerty"}, t0.Add(time.Duration(i)*time.Second)) != nil {
			t.Fatal("alarm below threshold")
		}
	}
	// The same, slowly over an hour, never alarms.
	for i := range 100 {
		from := fmt.Sprintf("s%d.pdf", i)
		if d.Observe("Fotos", "pid:3", ClassifyRename(from, from+".qwerty"), Affected{Was: from, Now: from + ".qwerty"}, t0.Add(time.Duration(i)*time.Minute)) != nil {
			t.Fatal("alarm on slow renames")
		}
	}
}

func TestDetectorUnknownRewritesNotesMute(t *testing.T) {
	d := NewDetector(DefaultThresholds())
	var inc *Incident
	for i := range 30 {
		from := fmt.Sprintf("f%d.docx", i)
		inc = d.Observe("A", "c", ClassifyRename(from, from+".x7k2q"), Affected{Was: from, Now: from + ".x7k2q"}, t0.Add(time.Duration(i)*time.Second))
	}
	if inc == nil || inc.Reason != "unknown-extension" {
		t.Fatalf("unknown: %+v", inc)
	}
	for i := range 30 {
		inc = d.Observe("B", "c", KindRewrite, Affected{Was: fmt.Sprintf("r%d.pdf", i), Now: fmt.Sprintf("r%d.pdf", i)}, t0.Add(time.Duration(i)*time.Second))
	}
	if inc == nil || inc.Reason != "rewrite" {
		t.Fatalf("rewrite: %+v", inc)
	}
	for i, name := range []string{"a/HOW_TO_DECRYPT.txt", "b/HOW_TO_DECRYPT.txt", "c/HOW_TO_DECRYPT.txt"} {
		if !IsNote(filepath.Base(name)) {
			t.Fatal(name)
		}
		inc = d.Observe("C", "c", KindNote, Affected{Was: name, Now: name}, t0.Add(time.Duration(i)*time.Second))
	}
	if inc == nil || inc.Reason != "notes" {
		t.Fatalf("notes: %+v", inc)
	}
	// After a false alarm the client is muted on that share.
	d.Forget("A")
	d.Mute("A", "c", t0.Add(time.Hour))
	for i := range 50 {
		from := fmt.Sprintf("g%d.docx", i)
		if d.Observe("A", "c", KindKnownRename, Affected{Was: from, Now: from + ".locked"}, t0.Add(time.Minute+time.Duration(i)*time.Second)) != nil {
			t.Fatal("muted client alarmed")
		}
	}
	if IsNote("Bericht.txt") || IsNote("decrypt.pdf") {
		t.Fatal("not notes")
	}
}

func TestLooksEncrypted(t *testing.T) {
	random := make([]byte, SampleSize)
	rand.Read(random)
	pdf := append([]byte("%PDF-1.7\n"), random...)
	text := bytes.Repeat([]byte("Liebe Grüße aus dem Urlaub. "), 400)
	for _, c := range []struct {
		name string
		data []byte
		want bool
	}{
		{"a.pdf", random, true},
		{"a.pdf", pdf, false},
		{"a.txt", random, true},
		{"a.txt", text, false},
		{"a.jpg", append([]byte{0xff, 0xd8, 0xff, 0xe0}, random...), false},
		{"a.mp4", append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p'}, random...), false},
		{"a.mp4", random, true},
		{"a.bin", random, false}, // promises nothing
		{"a.pdf", random[:100], false},
	} {
		if got := LooksEncrypted(c.name, c.data); got != c.want {
			t.Errorf("%s (%d bytes): %v", c.name, len(c.data), got)
		}
	}
	if !Judgeable("x.docx") || Judgeable("x.bin") {
		t.Fatal("judgeable")
	}
	if !Hidden(".titanos/agent/x") || !Hidden("a/.recycle/b") || Hidden("a/b.txt") {
		t.Fatal("hidden")
	}
}

// fanotify events as the kernel writes them (no root needed to parse).
func TestParseEvents(t *testing.T) {
	le := binary.NativeEndian
	info := func(typ uint8, handle []byte, name string) []byte {
		rec := make([]byte, 20)
		rec[0] = typ
		le.PutUint32(rec[4:], 11)
		le.PutUint32(rec[8:], 22)
		le.PutUint32(rec[12:], uint32(len(handle)))
		le.PutUint32(rec[16:], 1)
		rec = append(rec, handle...)
		rec = append(rec, name...)
		rec = append(rec, 0)
		for len(rec)%4 != 0 {
			rec = append(rec, 0)
		}
		le.PutUint16(rec[2:], uint16(len(rec)))
		return rec
	}
	event := func(mask uint64, pid int32, infos ...[]byte) []byte {
		meta := make([]byte, metaSize)
		body := bytes.Join(infos, nil)
		le.PutUint32(meta[0:], uint32(metaSize+len(body)))
		meta[4] = unix.FANOTIFY_METADATA_VERSION
		le.PutUint16(meta[6:], uint16(metaSize))
		le.PutUint64(meta[8:], mask)
		le.PutUint32(meta[16:], uint32(0xffffffff))
		le.PutUint32(meta[20:], uint32(pid))
		return append(meta, body...)
	}
	buf := append(event(unix.FAN_RENAME, 4242,
		info(unix.FAN_EVENT_INFO_TYPE_OLD_DFID_NAME, []byte{1, 2, 3, 4, 5, 6, 7, 8}, "Brief.pdf"),
		info(unix.FAN_EVENT_INFO_TYPE_NEW_DFID_NAME, []byte{1, 2, 3, 4, 5, 6, 7, 8}, "Brief.pdf.locked")),
		event(unix.FAN_CLOSE_WRITE, 7, info(unix.FAN_EVENT_INFO_TYPE_DFID_NAME, []byte{9, 9, 9, 9}, "a.txt"))...)
	buf = append(buf, 1, 2, 3) // a torn tail
	got := ParseEvents(buf)
	if len(got) != 2 {
		t.Fatalf("events: %d", len(got))
	}
	if got[0].PID != 4242 || len(got[0].Infos) != 2 || got[0].Infos[0].Name != "Brief.pdf" || got[0].Infos[1].Name != "Brief.pdf.locked" || got[0].Infos[0].Fsid != [2]int32{11, 22} || len(got[0].Infos[0].Handle) != 8 {
		t.Fatalf("rename: %+v", got[0])
	}
	if got[1].Mask != unix.FAN_CLOSE_WRITE || got[1].Infos[0].Name != "a.txt" {
		t.Fatalf("write: %+v", got[1])
	}
	// Without a known file system nothing resolves.
	if _, ok := got[0].resolve(map[[2]int32]int{}); ok {
		t.Fatal("resolved without mount")
	}
}

func TestParseSMBStatus(t *testing.T) {
	data := []byte(`{"sessions":{"3405617150":{"session_id":"3405617150","server_id":{"pid":"2287","task_id":"0"},"username":"lena","groupname":"users","remote_machine":"192.168.1.57","hostname":"ipv4:192.168.1.57:51234"},"9":{"server_id":{"pid":"99"},"username":"max","remote_machine":"PC-KUECHE","hostname":"ipv6:[fe80::1]:445"}}}`)
	m, err := ParseSMBStatus(data)
	if err != nil {
		t.Fatal(err)
	}
	if c := m[2287]; c.User != "lena" || c.IP != "192.168.1.57" {
		t.Fatalf("lena: %+v", c)
	}
	if c := m[99]; c.IP != "fe80::1" || c.Machine != "PC-KUECHE" || c.Label() != "PC-KUECHE (fe80::1)" {
		t.Fatalf("max: %+v %s", c, c.Label())
	}
	r := &fakeRunner{}
	if err := Disconnect(t.Context(), r, "Documents", "192.168.1.57"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls, ";") != "smbcontrol smbd close-share Documents;smbcontrol smbd kill-client-ip 192.168.1.57" {
		t.Fatalf("calls: %v", r.calls)
	}
	r.calls = nil
	Disconnect(t.Context(), r, "", "1.2.3.4; rm -rf /")
	if len(r.calls) != 0 {
		t.Fatalf("bad ip used: %v", r.calls)
	}
}

type fakeRunner struct{ calls []string }

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

// Restore from a fake snapshot: the .locked file goes to the recycle bin,
// the original comes back; a file the snapshot lacks stays.
func TestRestore(t *testing.T) {
	share, snap := t.TempDir(), t.TempDir()
	write := func(dir, rel, text string) {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755)
		os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o640)
	}
	write(snap, "Post/Telekom.pdf", "original pdf")
	write(snap, "Steuer/Belege.xlsx", "original xlsx")
	write(share, "Post/Telekom.pdf.locked", "ENCRYPTED")
	write(share, "Steuer/Belege.xlsx", "ENCRYPTED in place")
	write(share, "neu.txt.locked", "ENCRYPTED new")
	res := Restore(share, snap, []Affected{
		{Was: "Post/Telekom.pdf", Now: "Post/Telekom.pdf.locked"},
		{Was: "Steuer/Belege.xlsx", Now: "Steuer/Belege.xlsx"},
		{Was: "neu.txt", Now: "neu.txt.locked"},
		{Was: "../etc/passwd", Now: "x"},
	}, "20261106-1042")
	if res.Restored != 2 || res.Recycled != 2 || res.Missing != 1 || len(res.Failed) != 1 {
		t.Fatalf("result: %+v", res)
	}
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(share, rel)); return string(b) }
	if read("Post/Telekom.pdf") != "original pdf" || read("Steuer/Belege.xlsx") != "original xlsx" {
		t.Fatal("not restored")
	}
	if _, err := os.Stat(filepath.Join(share, "Post/Telekom.pdf.locked")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("locked file stayed")
	}
	if read(RecycleFolder+"/20261106-1042/Post/Telekom.pdf.locked") != "ENCRYPTED" || read(RecycleFolder+"/20261106-1042/Steuer/Belege.xlsx") != "ENCRYPTED in place" {
		t.Fatal("attacked copies not in the recycle bin")
	}
	if read("neu.txt.locked") != "ENCRYPTED new" {
		t.Fatal("file without snapshot copy touched")
	}
}

type memStore struct{ data []byte }

var errNone = errors.New("none")

func (m *memStore) Load(v any) error {
	if m.data == nil {
		return errNone
	}
	return json.Unmarshal(m.data, v)
}
func (m *memStore) Save(v any) error {
	b, err := json.Marshal(v)
	m.data = b
	return err
}

// The waiting time survives a restart (a new instance on the same store),
// a decline ends it, a grant ends after 30 days, a code grants at once.
func TestEmergency(t *testing.T) {
	store := &memStore{}
	nf := func(err error) bool { return errors.Is(err, errNone) }
	e := NewEmergency(store, nf)
	lena := &Person{Account: "lena", WaitDays: 3, Shares: []string{"Documents"}}
	if _, err := e.Ask(nil, "fremd", "x", "", t0); !errors.Is(err, ErrNotRegistered) {
		t.Fatal("unregistered asked")
	}
	req, err := e.Ask(lena, "lena", "Papa ist im Krankenhaus.", "", t0)
	if err != nil || !req.Due.Equal(t0.Add(72*time.Hour)) {
		t.Fatalf("ask: %+v %v", req, err)
	}
	if _, err := e.Ask(lena, "lena", "again", "", t0); !errors.Is(err, ErrOpenRequest) {
		t.Fatal("second open request")
	}
	// Restart.
	e = NewEmergency(store, nf)
	if g, _, _ := e.Due(t0.Add(71 * time.Hour)); len(g) != 0 {
		t.Fatal("granted early")
	}
	g, _, _ := e.Due(t0.Add(72 * time.Hour))
	if len(g) != 1 || g[0].ID != req.ID {
		t.Fatalf("due: %+v", g)
	}
	granted, err := e.Grant(req.ID, "", []string{"Documents"}, t0.Add(72*time.Hour))
	if err != nil || granted.State != StateGranted || !granted.Ends.Equal(t0.Add(72*time.Hour+GrantFor)) {
		t.Fatalf("grant: %+v %v", granted, err)
	}
	if _, ok := e.Active("lena", t0.Add(80*time.Hour)); !ok {
		t.Fatal("not active")
	}
	e = NewEmergency(store, nf)
	if _, end, _ := e.Due(t0.Add(72*time.Hour + GrantFor)); len(end) != 1 || end[0].Added[0] != "Documents" {
		t.Fatalf("end: %+v", end)
	}
	if r, _ := e.End(req.ID, "", t0.Add(72*time.Hour+GrantFor)); r.State != StateEnded {
		t.Fatal("not ended")
	}
	if _, ok := e.Active("lena", t0.Add(72*time.Hour+GrantFor)); ok {
		t.Fatal("still active")
	}
	// Decline.
	req2, _ := e.Ask(lena, "lena", "again", "", t0.Add(800*time.Hour))
	if r, err := e.Decline(req2.ID, "alex", t0.Add(801*time.Hour)); err != nil || r.State != StateDeclined {
		t.Fatalf("decline: %+v %v", r, err)
	}
	if g, _, _ := e.Due(t0.Add(2000 * time.Hour)); len(g) != 0 {
		t.Fatal("declined request granted")
	}
	if _, err := e.Grant(req2.ID, "alex", nil, t0.Add(802*time.Hour)); !errors.Is(err, ErrDecided) {
		t.Fatal("declined request granted by hand")
	}
	// One-time code.
	code, err := e.NewCode("lena")
	if err != nil || !e.HasCode("lena") || strings.Contains(string(store.data), code) {
		t.Fatalf("code kept in clear: %v", err)
	}
	if _, err := e.Ask(lena, "lena", "x", "WRONG-CODE0", t0.Add(900*time.Hour)); !errors.Is(err, ErrBadCode) {
		t.Fatal("wrong code")
	}
	req3, err := e.Ask(lena, "lena", "x", strings.ToLower(code), t0.Add(900*time.Hour))
	if err != nil || !req3.Code || !req3.Due.Equal(t0.Add(900*time.Hour)) || e.HasCode("lena") {
		t.Fatalf("code ask: %+v %v", req3, err)
	}
}

func TestHandbook(t *testing.T) {
	f := Facts{
		Host: "nas-haupt",
		Volumes: []VolumeFact{
			{Name: "Data", Level: "raid1", Filesystem: "ext4", Size: 8e12, Disks: []DiskFact{{Model: "WD Red 8 TB", Serial: "WD-CA1B2C3", Size: 8e12}, {Model: "WD Red 8 TB", Serial: "WD-CA4D5E6", Size: 8e12}}},
			{Name: "Scratch", Level: "raid0", Filesystem: "xfs"},
		},
		Shares: []NamedFact{{Name: "Documents", Detail: "Data"}},
		Remote: []NamedFact{{Name: "Tailscale", Detail: "nas-haupt.tailnet"}},
	}
	md := Markdown(f, t0, true)
	for _, want := range []string{"# Notfallhandbuch – nas-haupt", "WD-CA4D5E6", "RAID 1", "… eine Platte ausfällt", "Scratch", "keine Datensicherung", "Tailscale"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(Markdown(f, t0, false), "What to do when") {
		t.Fatal("english")
	}
	h := f.Hash()
	f.Volumes[0].Disks[1].Serial = "NEW"
	if f.Hash() == h {
		t.Fatal("hash ignores a changed disk")
	}
	if s := Sections(f, true); len(s) != 4 || !strings.Contains(strings.Join(s[0].Steps, " "), "Data (RAID 1, 1)") {
		t.Fatalf("sections: %+v", s)
	}
}

// Waiting and granted requests are never pruned, however many decided
// ones come after them.
func TestPruneKeepsOpen(t *testing.T) {
	store := &memStore{}
	e := NewEmergency(store, func(err error) bool { return errors.Is(err, errNone) })
	p := &Person{Account: "lena", Shares: []string{"Fotos"}}
	first, _ := e.Ask(p, "lena", "first", "", t0)
	e.Grant(first.ID, "alex", []string{"Fotos"}, t0)
	other := &Person{Account: "max", Shares: []string{"Fotos"}}
	for i := range 150 {
		r, err := e.Ask(other, "max", "x", "", t0.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		e.Decline(r.ID, "alex", t0.Add(time.Duration(i)*time.Minute))
	}
	st, _ := e.State()
	if len(st.Requests) > 101 {
		t.Fatalf("not pruned: %d", len(st.Requests))
	}
	if r, ok := e.Active("lena", t0.Add(time.Hour)); !ok || r.ID != first.ID {
		t.Fatal("granted request pruned")
	}
	if _, end, _ := e.Due(t0.Add(GrantFor)); len(end) != 1 {
		t.Fatal("grant does not end")
	}
}

// Wrong one-time codes: from the fifth on, codes are refused for a while,
// also with the right one; the lock survives a restart.
func TestCodeAttemptLimit(t *testing.T) {
	store := &memStore{}
	nf := func(err error) bool { return errors.Is(err, errNone) }
	e := NewEmergency(store, nf)
	p := &Person{Account: "lena", Shares: []string{"Fotos"}}
	code, _ := e.NewCode("lena")
	for i := range MaxCodeTries {
		if _, err := e.Ask(p, "lena", "x", "WRONG-WRONG", t0.Add(time.Duration(i)*time.Second)); !errors.Is(err, ErrBadCode) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	e = NewEmergency(store, nf)
	if _, err := e.Ask(p, "lena", "x", code, t0.Add(time.Minute)); !errors.Is(err, ErrCodeLocked) {
		t.Fatalf("right code during the lock: %v", err)
	}
	if r, err := e.Ask(p, "lena", "x", code, t0.Add(20*time.Minute)); err != nil || !r.Code {
		t.Fatalf("after the lock: %v", err)
	}
	if codeLock(MaxCodeTries+20) != 24*time.Hour || codeLock(MaxCodeTries-1) != 0 {
		t.Fatal("lock times")
	}
}
