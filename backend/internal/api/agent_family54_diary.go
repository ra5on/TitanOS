package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Familien-Tagebuch (PLAN 54-G): the tool diary collects what happened in
// a range of days, only from what the account itself may see: its own
// chats and reminders of the agent, Home Assistant (only for owners and
// administrators, like the other ha tools, and only released entities),
// photos of a folder of the account (EXIF dates; a sleeping disk is not
// woken in the background), Immich over its API with the account's key and
// Frigate events. The result is foreign content (untrusted) and names its
// sources, which the chat shows under the answer.

const (
	maxDiaryDays     = 31
	maxDiaryHomeDays = 7
	maxDiaryItems    = 400
	maxDiaryPhotos   = 20000
	maxDiaryAnswer   = 4 << 20
)

// diaryHTTP talks to Immich and Frigate (tests replace it).
var diaryHTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type diaryItem struct {
	Day    string `json:"-"`
	Time   string `json:"time,omitempty"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

type diaryDay struct {
	Date  string      `json:"date"`
	Items []diaryItem `json:"items"`
}

func (b *agentToolbox) diaryTool() *agentTool {
	return &agentTool{name: "diary", area: agent.AreaAgent, kind: agent.KindRead, untrusted: true,
		description: "Family diary: what happened on some days, e.g. \"Was haben wir letztes Wochenende gemacht?\" or \"Wer war heute an der Tür?\" (focus door, only with Frigate). Collects the person's own chats and reminders, Home Assistant, photos (folder, Immich) and Frigate for the range. Work out the dates from the current time. Name the sources in the answer.",
		params: schema(map[string]any{
			"from":  str("First day, YYYY-MM-DD."),
			"to":    str("Last day, YYYY-MM-DD (at most 31 days after from)."),
			"focus": enum("all (default) or door: who was at the door (Frigate only).", "all", "door"),
		}, "from", "to"),
		own: b.diary}
}

func (b *agentToolbox) diary(ctx context.Context, chat *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct{ From, To, Focus string }
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	loc := b.s.agentLocation()
	from, err1 := time.ParseInLocation("2006-01-02", req.From, loc)
	to, err2 := time.ParseInLocation("2006-01-02", req.To, loc)
	if err1 != nil || err2 != nil || to.Before(from) || to.Sub(from) > maxDiaryDays*24*time.Hour {
		return agent.Outcome{}, toolcatalog.Errorf("from and to as YYYY-MM-DD, to not before from, at most %d days", maxDiaryDays)
	}
	end := to.AddDate(0, 0, 1)
	if now := time.Now(); end.After(now) {
		end = now
	}
	mine, err := b.s.agentFeaturesOf(b.user.Name)
	if err != nil {
		return agent.Outcome{}, err
	}
	d := mine.Diary
	c := &diaryCollect{loc: loc, from: from, end: end}
	background := chat == nil || chat.Origin == "event" || chat.Origin == "reflect" || isMorningReport(chat)
	door := req.Focus == "door"
	if door {
		if d.FrigateURL == "" || !d.on("frigate") {
			c.note("Wer an der Tür war, weiß nur Frigate; Frigate ist für dieses Konto nicht eingerichtet (Funktionen › Familien-Tagebuch).")
		} else {
			b.diaryFrigate(ctx, c, d, true)
		}
		return c.outcome()
	}
	if d.on("chats") {
		b.diaryChats(c, chat)
	}
	if d.on("reminders") {
		b.diaryReminders(c)
	}
	if d.on("home") {
		b.diaryHome(ctx, c)
	}
	if d.on("photos") && d.Photos != nil {
		b.diaryPhotos(ctx, c, *d.Photos, background)
	}
	if d.on("immich") && d.ImmichURL != "" && d.ImmichKey != "" {
		b.diaryImmich(ctx, c, d)
	}
	if d.on("frigate") && d.FrigateURL != "" {
		b.diaryFrigate(ctx, c, d, false)
	}
	return c.outcome()
}

// diaryCollect gathers the items and the sources that gave something.
type diaryCollect struct {
	loc       *time.Location
	from, end time.Time
	items     []diaryItem
	sources   []agent.Source
	notes     []string
}

func (c *diaryCollect) in(t time.Time) bool { return !t.Before(c.from) && t.Before(c.end) }

func (c *diaryCollect) add(at time.Time, source, text string, withTime bool) {
	if len(c.items) >= maxDiaryItems {
		return
	}
	at = at.In(c.loc)
	item := diaryItem{Day: at.Format("2006-01-02"), Source: source, Text: text}
	if withTime {
		item.Time = at.Format("15:04")
	}
	c.items = append(c.items, item)
}

func (c *diaryCollect) addDay(day, source, text string) {
	if len(c.items) < maxDiaryItems {
		c.items = append(c.items, diaryItem{Day: day, Source: source, Text: text})
	}
}

// source names a source under the answer ("Fotos (48)").
func (c *diaryCollect) source(title, id string) {
	c.sources = append(c.sources, agent.Source{Title: title, URL: "titanos:diary/" + id})
}

func (c *diaryCollect) note(text string) { c.notes = append(c.notes, text) }

func (c *diaryCollect) outcome() (agent.Outcome, error) {
	byDay := map[string][]diaryItem{}
	for _, it := range c.items {
		byDay[it.Day] = append(byDay[it.Day], it)
	}
	days := []diaryDay{}
	for day := c.from; day.Before(c.end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		items := byDay[key]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Time < items[j].Time })
		if len(items) > 0 {
			days = append(days, diaryDay{Date: key + " " + day.Weekday().String(), Items: items})
		}
	}
	names := []string{}
	for _, s := range c.sources {
		names = append(names, s.Title)
	}
	out := map[string]any{"from": c.from.Format("2006-01-02"), "to": c.end.Add(-time.Second).Format("2006-01-02"), "days": days, "sources": names}
	if len(c.notes) > 0 {
		out["notes"] = c.notes
	}
	if len(days) == 0 {
		out["empty"] = "Nothing was found in these sources for the range; say so, do not invent anything."
	}
	data, err := json.Marshal(out)
	if err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: string(data), Sources: c.sources}, nil
}

// ---------- chats and reminders ----------

func (b *agentToolbox) diaryChats(c *diaryCollect, current *agent.Chat) {
	list, err := b.s.agentStore().Chats(b.user.Name)
	if err != nil {
		c.note("Chats: nicht lesbar.")
		return
	}
	n := 0
	for _, info := range list {
		if current != nil && info.ID == current.ID || info.Origin == "event" || info.Origin == "reflect" {
			continue
		}
		at := info.Created
		if !c.in(at) {
			continue
		}
		c.add(at, "chats", "Chat: "+info.Title, true)
		n++
	}
	if n > 0 {
		c.source(fmt.Sprintf("Chats (%d)", n), "chats")
	}
}

func (b *agentToolbox) diaryReminders(c *diaryCollect) {
	list, err := b.s.agentStore().Schedules(b.user.Name)
	if err != nil {
		c.note("Erinnerungen: nicht lesbar.")
		return
	}
	n := 0
	for _, sc := range list {
		if sc.Kind != "reminder" {
			continue
		}
		switch {
		case !sc.LastRun.IsZero() && c.in(sc.LastRun):
			c.add(sc.LastRun, "reminders", "Erinnerung: "+sc.Name, true)
		case sc.Every == "once" && sc.Date != "":
			at, err := time.ParseInLocation("2006-01-02 15:04", sc.Date+" "+cmpOr(sc.At, "00:00"), c.loc)
			if err != nil || !c.in(at) {
				continue
			}
			c.add(at, "reminders", "Erinnerung: "+sc.Name, sc.At != "")
		default:
			continue
		}
		n++
	}
	if n > 0 {
		c.source(fmt.Sprintf("Erinnerungen (%d)", n), "reminders")
	}
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ---------- Home Assistant ----------

// diaryHomeDomains are the entities that tell what a household did.
var diaryHomeDomains = []string{"person", "alarm_control_panel", "lock", "binary_sensor", "cover", "device_tracker"}

const maxDiaryEntities = 12

func (b *agentToolbox) diaryHome(ctx context.Context, c *diaryCollect) {
	hub := b.s.haHub()
	if hub == nil || len(hub.Active()) == 0 {
		return
	}
	if !b.user.Role.CanAdminister() {
		// Like the other ha tools: only owners and administrators.
		return
	}
	ctx = b.haContext(ctx, &agentTool{area: toolcatalog.AreaHome})
	start := c.from
	if c.end.Sub(start) > maxDiaryHomeDays*24*time.Hour {
		start = c.end.Add(-maxDiaryHomeDays * 24 * time.Hour)
		c.note("Home Assistant: nur die letzten 7 Tage des Zeitraums.")
	}
	n := 0
	for _, ha := range hub.Active() {
		states, err := ha.States(ctx, true)
		if err != nil {
			c.note("Home Assistant: nicht erreichbar.")
			continue
		}
		var picked []homeassistant.State
		for _, st := range states {
			domain, _, _ := strings.Cut(st.EntityID, ".")
			if !slices.Contains(diaryHomeDomains, domain) {
				continue
			}
			if domain == "binary_sensor" {
				class, _ := st.Attributes["device_class"].(string)
				if !slices.Contains([]string{"door", "garage_door", "opening", "lock", "occupancy", "presence"}, class) {
					continue
				}
			}
			picked = append(picked, st)
			if len(picked) >= maxDiaryEntities {
				break
			}
		}
		for _, st := range picked {
			for day := start; day.Before(c.end); day = day.Add(24 * time.Hour) {
				until := day.Add(24 * time.Hour)
				if until.After(c.end) {
					until = c.end
				}
				h, err := ha.History(ctx, st.EntityID, day, until, 0)
				if err != nil {
					break
				}
				prev := ""
				for _, s := range h.States {
					if s.State == prev || s.State == "unavailable" || s.State == "unknown" {
						continue
					}
					prev = s.State
					if c.in(s.LastChanged) {
						c.add(s.LastChanged, "home", st.Name()+": "+s.State, true)
						n++
					}
				}
			}
		}
	}
	if n > 0 {
		c.source("Home Assistant", "home")
	}
}

// ---------- photos of a folder ----------

// diaryFolderAllowed: the folder is in a root this account may open.
func (s *Server) diaryFolderAllowed(user string, f agentFolderRef) error {
	if s.opts.Users == nil {
		return errors.New("diary: no accounts")
	}
	u, ok := s.opts.Users.Get(user)
	if !ok {
		return errors.New("diary: unknown account")
	}
	if _, err := s.catalogRoot(u, f.Root); err != nil {
		return errors.New("diary: this account may not open the photo folder")
	}
	clean := path.Clean("/" + f.Path)
	if f.Path != "" && strings.TrimPrefix(clean, "/") != strings.Trim(f.Path, "/") || strings.Contains(f.Path, "\x00") {
		return errors.New("diary: photo folder path")
	}
	return nil
}

var photoExt = []string{".jpg", ".jpeg", ".heic", ".png", ".mp4", ".mov"}

func (b *agentToolbox) diaryPhotos(ctx context.Context, c *diaryCollect, f agentFolderRef, background bool) {
	// The rights of now, not of when the folder was chosen.
	root, err := b.s.catalogRoot(b.user, f.Root)
	if err != nil {
		c.note("Fotos: der Ordner ist für dieses Konto nicht mehr freigegeben.")
		return
	}
	if b.s.agentAsleep(ctx, b.user, f.Root) {
		// A reading tool cannot ask; only "wake" lets it wake the disk.
		in, err := b.s.agentInitiativeOf(b.user.Name)
		if background || err != nil || in.Wake != "wake" {
			c.note("Fotos: die Platte schläft und wird jetzt nicht geweckt.")
			return
		}
	}
	dir, err := os.OpenRoot(root.Dir)
	if err != nil {
		c.note("Fotos: Ordner nicht lesbar.")
		return
	}
	defer dir.Close()
	base := strings.Trim(path.Clean("/"+f.Path), "/")
	if base == "" {
		base = "."
	}
	perDay := map[string]int{}
	seen := 0
	_ = fs.WalkDir(dir.FS(), base, func(p string, entry fs.DirEntry, err error) error { // unreadable entries are skipped
		if err != nil || ctx.Err() != nil {
			return nil
		}
		if entry.IsDir() {
			if p != base && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			// Folders locked for the agent stay closed, also below the base.
			rel := p
			if rel == "." {
				rel = ""
			}
			if b.realFolderMode(f.Root, rel) == agent.ModeDeny {
				if p == base {
					c.note("Fotos: der Ordner ist für den Agenten gesperrt.")
				}
				return fs.SkipDir
			}
			return nil
		}
		if seen++; seen > maxDiaryPhotos {
			return fs.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !slices.Contains(photoExt, ext) {
			return nil
		}
		info, err := entry.Info()
		// A photo of the range was written after it was taken.
		if err != nil || info.ModTime().Before(c.from) {
			return nil
		}
		taken := info.ModTime()
		if ext == ".jpg" || ext == ".jpeg" {
			if file, err := dir.Open(p); err == nil {
				if photo := thumbs.ReadPhoto(file); !photo.Taken.IsZero() {
					// EXIF times are the camera's local time.
					t := photo.Taken
					taken = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, c.loc)
				}
				file.Close()
			}
		}
		if c.in(taken) {
			perDay[taken.In(c.loc).Format("2006-01-02")]++
		}
		return nil
	})
	total := 0
	days := make([]string, 0, len(perDay))
	for day := range perDay {
		days = append(days, day)
	}
	slices.Sort(days)
	for _, day := range days {
		c.addDay(day, "photos", fmt.Sprintf("%d Fotos im Ordner %s", perDay[day], path.Base("/"+f.Path)))
		total += perDay[day]
	}
	if total > 0 {
		c.source(fmt.Sprintf("Fotos (%d)", total), "photos")
	}
}

// ---------- Immich ----------

func diaryRequest(ctx context.Context, method, raw string, body any, header map[string]string, out any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := diaryHTTP.Do(req)
	if err != nil {
		return errors.New("not reachable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("answered %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxDiaryAnswer)).Decode(out)
}

func (b *agentToolbox) diaryImmich(ctx context.Context, c *diaryCollect, d agentDiaryMine) {
	type asset struct {
		LocalDateTime time.Time `json:"localDateTime"`
		FileCreatedAt time.Time `json:"fileCreatedAt"`
		Type          string    `json:"type"`
		ExifInfo      *struct {
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"exifInfo"`
		People []struct {
			Name string `json:"name"`
		} `json:"people"`
	}
	type day struct {
		count  int
		cities map[string]int
		people map[string]int
	}
	days := map[string]*day{}
	total := 0
	for page := 1; page <= 4; page++ {
		var res struct {
			Assets struct {
				Items    []asset `json:"items"`
				NextPage *string `json:"nextPage"`
			} `json:"assets"`
		}
		body := map[string]any{"takenAfter": c.from.UTC().Format(time.RFC3339), "takenBefore": c.end.UTC().Format(time.RFC3339), "size": 250, "page": page, "withExif": true, "withPeople": true}
		if err := diaryRequest(ctx, http.MethodPost, d.ImmichURL+"/api/search/metadata", body, map[string]string{"x-api-key": d.ImmichKey}, &res); err != nil {
			c.note("Immich: " + err.Error() + ".")
			break
		}
		for _, a := range res.Assets.Items {
			// localDateTime is the wall clock where the photo was taken,
			// written as UTC.
			t := a.LocalDateTime
			at := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, c.loc)
			if t.IsZero() {
				at = a.FileCreatedAt.In(c.loc)
			}
			key := at.Format("2006-01-02")
			if !c.in(at) {
				continue
			}
			dd := days[key]
			if dd == nil {
				dd = &day{cities: map[string]int{}, people: map[string]int{}}
				days[key] = dd
			}
			dd.count++
			total++
			if a.ExifInfo != nil && a.ExifInfo.City != "" {
				dd.cities[a.ExifInfo.City]++
			}
			for _, p := range a.People {
				if p.Name != "" {
					dd.people[p.Name]++
				}
			}
		}
		if res.Assets.NextPage == nil || *res.Assets.NextPage == "" {
			break
		}
	}
	top := func(m map[string]int) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || m[keys[i]] == m[keys[j]] && keys[i] < keys[j] })
		out := []string{}
		for _, k := range keys[:min(3, len(keys))] {
			out = append(out, k+" ("+strconv.Itoa(m[k])+")")
		}
		return out
	}
	keys := make([]string, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		dd := days[k]
		text := fmt.Sprintf("%d Fotos in Immich", dd.count)
		if places := top(dd.cities); len(places) > 0 {
			text += ", Orte: " + strings.Join(places, ", ")
		}
		if people := top(dd.people); len(people) > 0 {
			text += ", Personen: " + strings.Join(people, ", ")
		}
		c.addDay(k, "immich", text)
	}
	if total > 0 {
		c.source(fmt.Sprintf("Immich (%d)", total), "immich")
	}
}

// ---------- Frigate ----------

func (b *agentToolbox) diaryFrigate(ctx context.Context, c *diaryCollect, d agentDiaryMine, door bool) {
	q := url.Values{"after": {strconv.FormatInt(c.from.Unix(), 10)}, "before": {strconv.FormatInt(c.end.Unix(), 10)}, "limit": {"200"}}
	if door {
		q.Set("labels", "person")
		q.Set("label", "person")
	}
	var events []struct {
		Camera    string  `json:"camera"`
		Label     string  `json:"label"`
		SubLabel  any     `json:"sub_label"`
		StartTime float64 `json:"start_time"`
		Zones     []string
	}
	if err := diaryRequest(ctx, http.MethodGet, d.FrigateURL+"/api/events?"+q.Encode(), nil, nil, &events); err != nil {
		c.note("Frigate: " + err.Error() + ".")
		return
	}
	n := 0
	counts := map[string]map[string]int{}
	for _, e := range events {
		at := time.Unix(int64(e.StartTime), 0)
		if !c.in(at) {
			continue
		}
		n++
		who := e.Label
		if name, ok := e.SubLabel.(string); ok && name != "" {
			who = name
		} else if list, ok := e.SubLabel.([]any); ok && len(list) > 0 {
			if name, ok := list[0].(string); ok && name != "" {
				who = name
			}
		}
		if door || e.Label == "person" {
			c.add(at, "frigate", fmt.Sprintf("%s: %s", e.Camera, who), true)
			continue
		}
		key := at.In(c.loc).Format("2006-01-02")
		if counts[key] == nil {
			counts[key] = map[string]int{}
		}
		counts[key][e.Camera+": "+who]++
	}
	for day, m := range counts {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			c.addDay(day, "frigate", fmt.Sprintf("%s (%d-mal)", k, m[k]))
		}
	}
	if n > 0 {
		c.source(fmt.Sprintf("Frigate (%d)", n), "frigate")
	}
}
