package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// PLAN 53: the agent as more than a chat bot. Every account sets for itself
// how much the agent does on its own (initiative.json in its private agent
// folder): thinking about the day's chats at night, watching the NAS,
// forecasts, events, learning skills, caring for its memory, personality,
// whether its own tasks may wake a sleeping disk and where it searches.
// What it finds waits in "Vorschläge" (inbox.json) until the person says yes.
// Work in the background never wakes a sleeping disk.

type agentFolderRef struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

type agentInitiative struct {
	Level       string           `json:"level"`
	Reflect     bool             `json:"reflect"`
	ReflectAt   string           `json:"reflectAt"`
	Confirm     bool             `json:"confirm"`
	MorningNote bool             `json:"morningNote"`
	Watch       bool             `json:"watch"`
	Forecast    bool             `json:"forecast"`
	Events      []string         `json:"events"`
	Wake        string           `json:"wake"`
	Search      []agentFolderRef `json:"search"`
	QuietFrom   string           `json:"quietFrom"`
	QuietTo     string           `json:"quietTo"`
	GPUMinutes  int              `json:"gpuMinutes"`
	Weekly      bool             `json:"weekly"`
	MemoryAuto  bool             `json:"memoryAuto"`
	Conflicts   bool             `json:"conflicts"`
	FadeMonths  int              `json:"fadeMonths"`
	LearnSkills bool             `json:"learnSkills"`
	Length      string           `json:"length"`
	Tone        string           `json:"tone"`
	Name        string           `json:"name"`
	Icon        string           `json:"icon"`
	// State kept by titand.
	LastReflect time.Time      `json:"lastReflect,omitzero"`
	Learned     []string       `json:"learned,omitempty"`
	Flows       map[string]int `json:"flows,omitempty"`
}

func defaultInitiative() agentInitiative {
	return agentInitiative{Level: "suggest", Reflect: true, ReflectAt: "03:30", Confirm: true, MorningNote: true,
		Watch: true, Forecast: true, Events: []string{"usb", "update"}, Wake: "wake", Search: []agentFolderRef{},
		QuietFrom: "22:00", QuietTo: "07:00", GPUMinutes: 30, Weekly: true, MemoryAuto: true, Conflicts: true,
		FadeMonths: 6, LearnSkills: true, Length: "normal", Tone: "casual"}
}

var clockRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (in *agentInitiative) clean() error {
	pick := func(v *string, ok []string, def string) {
		if !slices.Contains(ok, *v) {
			*v = def
		}
	}
	d := defaultInitiative()
	pick(&in.Level, []string{"quiet", "hints", "suggest", "act"}, d.Level)
	pick(&in.Wake, []string{"wake", "ask", "never"}, d.Wake)
	pick(&in.Length, []string{"short", "normal", "long"}, d.Length)
	pick(&in.Tone, []string{"casual", "plain"}, d.Tone)
	for _, c := range []*string{&in.ReflectAt, &in.QuietFrom, &in.QuietTo} {
		if !clockRe.MatchString(*c) {
			return errors.New("times as HH:MM")
		}
	}
	in.Events = slices.DeleteFunc(slices.Clone(in.Events), func(e string) bool {
		return !slices.Contains([]string{"usb", "update"}, e)
	})
	if in.Search == nil {
		in.Search = []agentFolderRef{}
	}
	if len(in.Search) > 32 {
		return errors.New("at most 32 search places")
	}
	in.GPUMinutes = min(max(in.GPUMinutes, 5), 120)
	in.FadeMonths = min(max(in.FadeMonths, 0), 24)
	if len([]rune(in.Name)) > 24 || len([]rune(in.Icon)) > 4 {
		return errors.New("name or icon too long")
	}
	return nil
}

// agentInitState keeps each account's initiative in RAM once read, so the
// minute tick and notifications do not open files on the agent's volume,
// and the suggestions found while that volume sleeps.
type agentInitState struct {
	mu      sync.Mutex
	cache   map[string]agentInitiative
	inboxMu sync.Mutex
	pending map[string][]agentInboxItem
}

const maxAgentPrivate = 512 << 10

// agentInitiativeOf reads the account's settings: defaults if there are
// none yet, an error if the file cannot be read (then nothing writes it).
// Callers hold no lock.
func (s *Server) agentInitiativeOf(user string) (agentInitiative, error) {
	s.agentInit.mu.Lock()
	defer s.agentInit.mu.Unlock()
	return s.agentInitiativeLocked(user)
}

func (s *Server) agentInitiativeLocked(user string) (agentInitiative, error) {
	if in, ok := s.agentInit.cache[user]; ok {
		return in, nil
	}
	in := defaultInitiative()
	if err := s.agentStore().ReadPrivateLimit(user, "initiative.json", &in, maxAgentPrivate); err != nil {
		if !errors.Is(err, agent.ErrNotFound) {
			return defaultInitiative(), err
		}
		in = defaultInitiative()
	}
	if in.clean() != nil {
		return defaultInitiative(), errors.New("initiative.json is not valid")
	}
	if s.agentInit.cache == nil {
		s.agentInit.cache = map[string]agentInitiative{}
	}
	s.agentInit.cache[user] = in
	return in, nil
}

// agentInitiativeBG is for work in the background: only from RAM, or from
// the volume while it is awake anyway.
func (s *Server) agentInitiativeBG(user string) (agentInitiative, bool) {
	s.agentInit.mu.Lock()
	in, ok := s.agentInit.cache[user]
	s.agentInit.mu.Unlock()
	if ok {
		return in, true
	}
	if s.agentVolumeAsleep() {
		return agentInitiative{}, false
	}
	in, err := s.agentInitiativeOf(user)
	return in, err == nil
}

// saveAgentInitiativeLocked writes and caches; agentInitMu is held.
func (s *Server) saveAgentInitiativeLocked(user string, in agentInitiative) error {
	if len(in.Learned) > 100 {
		in.Learned = in.Learned[len(in.Learned)-100:]
	}
	if err := s.agentStore().WritePrivate(user, "initiative.json", in); err != nil {
		return err
	}
	if s.agentInit.cache == nil {
		s.agentInit.cache = map[string]agentInitiative{}
	}
	s.agentInit.cache[user] = in
	return nil
}

func (s *Server) handleAgentInitiative(w http.ResponseWriter, r *http.Request) {
	in, err := s.agentInitiativeOf(currentUser(r).Name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unreadable", "the settings of the agent cannot be read; nothing was changed")
		return
	}
	// The weekly review is on by default: it appears with the first look.
	s.agentWeeklySchedule(currentUser(r).Name, in.Weekly)
	in.Learned, in.Flows = nil, nil
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleAgentInitiativeSet(w http.ResponseWriter, r *http.Request) {
	var in agentInitiative
	if !readJSON(w, r, &in) {
		return
	}
	if err := in.clean(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_initiative", err.Error())
		return
	}
	user := currentUser(r)
	s.agentInit.mu.Lock()
	old, err := s.agentInitiativeLocked(user.Name)
	if err == nil {
		in.LastReflect, in.Learned, in.Flows = old.LastReflect, old.Learned, old.Flows
		err = s.saveAgentInitiativeLocked(user.Name, in)
	}
	s.agentInit.mu.Unlock()
	if err != nil {
		agentStoreError(w, err)
		return
	}
	s.agentWeeklySchedule(user.Name, in.Weekly)
	in.Learned, in.Flows = nil, nil
	writeJSON(w, http.StatusOK, in)
}

// agentWeeklySchedule keeps the weekly review of an account as an ordinary
// planned task (Sunday 18:00), so it can be moved or changed like any other.
func (s *Server) agentWeeklySchedule(user string, on bool) {
	store := s.agentStore()
	list, err := store.Schedules(user)
	if err != nil {
		return
	}
	const id = "weeklyreview0001"
	i := slices.IndexFunc(list, func(sc agent.Schedule) bool { return sc.ID == id })
	switch {
	case on && i < 0:
		sc := agent.Schedule{ID: id, Name: "Wochenrückblick", Every: "weekly", Weekday: 0, At: "18:00", Enabled: true, Created: time.Now().UTC(),
			Task: "Wochenrückblick: Was lief auf dem NAS diese Woche (Backups, Meldungen, Updates, Speicher), was hast du als Agent getan, und was steht nächste Woche an? Kurz und mit Tabelle; ändere nichts."}
		if checkSchedule(&sc) != nil {
			return
		}
		list = append(list, sc)
	case !on && i >= 0:
		list = slices.Delete(list, i, i+1)
	default:
		return
	}
	_ = store.SaveSchedules(user, list)
}

// agentStyle turns personality and memory settings into instructions for
// the prompt of the account.
func agentStyle(in agentInitiative) string {
	var b strings.Builder
	switch in.Length {
	case "short":
		b.WriteString("Answer short: a few sentences or a small table. ")
	case "long":
		b.WriteString("Answer thoroughly with explanations and context. ")
	}
	if in.Tone == "plain" {
		b.WriteString("Use a plain, factual tone without small talk; in German use \"Sie\" only if the person does. ")
	} else {
		b.WriteString("Use a friendly, relaxed tone. ")
	}
	if in.Name != "" {
		fmt.Fprintf(&b, "Your name is %s. ", in.Name)
	}
	if in.MemoryAuto {
		b.WriteString("Remember lasting facts on your own with memory_save (corrections, where things live, preferences) and always when the person says \"merk dir\" or \"remember\". ")
	} else {
		b.WriteString("Use memory_save only when the person explicitly asks you to remember something. ")
	}
	return strings.TrimSpace(b.String())
}

// ---------- Vorschläge ----------

type agentInboxItem struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Title   string            `json:"title"`
	Body    string            `json:"body,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
	Created time.Time         `json:"created"`
}

const maxInbox = 100

// agentInboxRead reads the list (agentInboxMu held) and adds what waited.
func (s *Server) agentInboxRead(user string) ([]agentInboxItem, error) {
	list := []agentInboxItem{}
	if err := s.agentStore().ReadPrivateLimit(user, "inbox.json", &list, maxAgentPrivate); err != nil && !errors.Is(err, agent.ErrNotFound) {
		return nil, err
	}
	if pending := s.agentInit.pending[user]; len(pending) > 0 {
		for _, item := range pending {
			list = mergeInbox(list, item)
		}
		if err := s.agentStore().WritePrivate(user, "inbox.json", list); err != nil {
			return nil, err
		}
		delete(s.agentInit.pending, user)
	}
	return list, nil
}

func mergeInbox(list []agentInboxItem, item agentInboxItem) []agentInboxItem {
	if slices.ContainsFunc(list, func(x agentInboxItem) bool { return x.Kind == item.Kind && x.Title == item.Title }) {
		return list
	}
	list = append(list, item)
	if len(list) > maxInbox {
		list = list[len(list)-maxInbox:]
	}
	return list
}

func (s *Server) agentInbox(user string) []agentInboxItem {
	s.agentInit.inboxMu.Lock()
	defer s.agentInit.inboxMu.Unlock()
	list, err := s.agentInboxRead(user)
	if err != nil {
		return []agentInboxItem{}
	}
	return list
}

// addAgentInbox keeps one item per kind and title; the oldest go first.
// While the agent's volume sleeps the item waits in RAM.
func (s *Server) addAgentInbox(user string, item agentInboxItem) {
	item.ID, item.Created = agent.NewID(), time.Now().UTC()
	s.agentInit.inboxMu.Lock()
	defer s.agentInit.inboxMu.Unlock()
	if s.agentVolumeAsleep() {
		if s.agentInit.pending == nil {
			s.agentInit.pending = map[string][]agentInboxItem{}
		}
		s.agentInit.pending[user] = mergeInbox(s.agentInit.pending[user], item)
		return
	}
	list, err := s.agentInboxRead(user)
	if err != nil {
		return
	}
	_ = s.agentStore().WritePrivate(user, "inbox.json", mergeInbox(list, item))
}

func (s *Server) handleAgentInbox(w http.ResponseWriter, r *http.Request) {
	s.agentInit.inboxMu.Lock()
	list, err := s.agentInboxRead(currentUser(r).Name)
	s.agentInit.inboxMu.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unreadable", "the suggestions cannot be read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// handleAgentInboxDecide takes a suggestion ({"accept": true}) or drops it.
func (s *Server) handleAgentInboxDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accept bool   `json:"accept"`
		Text   string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r).Name
	id := r.PathValue("id")
	s.agentInit.inboxMu.Lock()
	list, err := s.agentInboxRead(user)
	if err != nil {
		s.agentInit.inboxMu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "agent_unreadable", "the suggestions cannot be read")
		return
	}
	i := slices.IndexFunc(list, func(x agentInboxItem) bool { return x.ID == id })
	if i < 0 {
		s.agentInit.inboxMu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "no such suggestion")
		return
	}
	item := list[i]
	list = slices.Delete(list, i, i+1)
	err = s.agentStore().WritePrivate(user, "inbox.json", list)
	s.agentInit.inboxMu.Unlock()
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if req.Accept {
		if req.Text != "" && len(req.Text) <= 600 {
			item.Title = req.Text
		}
		if err := s.applyAgentInbox(user, item); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_suggestion", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) applyAgentInbox(user string, item agentInboxItem) error {
	store := s.agentStore()
	switch item.Kind {
	case "memory":
		list, err := store.Memories(user)
		if err != nil {
			return err
		}
		if old := item.Data["replace"]; old != "" {
			list = slices.DeleteFunc(list, func(m agent.Memory) bool { return m.ID == old })
		}
		if !slices.ContainsFunc(list, func(m agent.Memory) bool { return strings.EqualFold(m.Text, item.Title) }) {
			list = append(list, agent.Memory{ID: agent.NewID(), Text: item.Title, At: time.Now().UTC(), Chat: item.Data["chat"], Untrusted: item.Data["untrusted"] == "1"})
		}
		if len(list) > maxMemories {
			list = list[len(list)-maxMemories:]
		}
		return store.SaveMemories(user, list)
	case "fade", "conflict":
		list, err := store.Memories(user)
		if err != nil {
			return err
		}
		return store.SaveMemories(user, slices.DeleteFunc(list, func(m agent.Memory) bool { return m.ID == item.Data["memory"] }))
	case "skill":
		list, err := store.Skills(user)
		if err != nil {
			return err
		}
		if len(list) >= maxSkills {
			return errors.New("too many skills")
		}
		list = append(list, agent.Skill{ID: agent.NewID(), Name: item.Title, When: item.Data["when"], Steps: item.Data["steps"], Chat: item.Data["chat"], Updated: time.Now().UTC()})
		return store.SaveSkills(user, list)
	case "deadline":
		// PLAN 54-D: a deadline becomes reminders (agent_docs_deadline.go).
		return s.docsAcceptDeadline(user, item)
	}
	// Hints, forecasts and events: noted, nothing to change.
	return nil
}

func (s *Server) handleAgentMemoryClear(w http.ResponseWriter, r *http.Request) {
	if err := s.agentStore().SaveMemories(currentUser(r).Name, []agent.Memory{}); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": []agent.Memory{}})
}

// ---------- memory care ----------

var memoryWord = regexp.MustCompile(`[\p{L}\p{N}]{4,}`)

// memoryConflict finds an older memory about the same thing that says
// something else: most of its longer words are shared, but not all.
func memoryConflict(list []agent.Memory, text string) *agent.Memory {
	words := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, w := range memoryWord.FindAllString(strings.ToLower(s), -1) {
			out[w] = true
		}
		return out
	}
	nw := words(text)
	if len(nw) < 3 {
		return nil
	}
	for i := range list {
		ow := words(list[i].Text)
		shared := 0
		for w := range nw {
			if ow[w] {
				shared++
			}
		}
		if shared*2 >= min(len(nw), len(ow)) && shared < max(len(nw), len(ow)) && !strings.EqualFold(list[i].Text, text) {
			return &list[i]
		}
	}
	return nil
}

// ---------- tools: sleeping disks and search places ----------

// agentAsleep tells whether a disk of the volume behind a root is asleep,
// from what titand already knows (lsblk and its own standby list; no disk
// is touched).
func (s *Server) agentAsleep(ctx context.Context, user auth.User, rootID string) bool {
	env := s.agentEnv()
	if env.storage == nil || rootID == "" {
		return false
	}
	root, err := s.catalogRoot(user, rootID)
	if err != nil || root.Volume == "" {
		return false
	}
	st, err := env.storage.DiskSleepStatus(ctx)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(st.DiskStatus, func(d storage.DiskSleepDisk) bool { return d.Volume == root.Volume && d.State == "asleep" })
}

var agentFileTools = []string{"files_search", "files_list", "file_read", "document_read", "files_read_text", "backup_files"}

// initiativeCheck applies the account's choices to a file tool: whether it
// may wake a sleeping disk (background chats never do) and where it searches.
func (b *agentToolbox) initiativeCheck(ctx context.Context, chat *agent.Chat, name string, args json.RawMessage, mode *string) error {
	if !slices.Contains(agentFileTools, name) {
		return nil
	}
	var a struct{ Root string }
	_ = json.Unmarshal(args, &a)
	in, err := b.s.agentInitiativeOf(b.user.Name)
	if err != nil {
		in = defaultInitiative()
	}
	if name == "files_search" && len(in.Search) > 0 && a.Root != "" &&
		!slices.ContainsFunc(in.Search, func(f agentFolderRef) bool { return f.Root == a.Root }) && !namedInChat(chat, a.Root) {
		return toolcatalog.Errorf("%s is not one of the person's search places (Einstellungen › Eigeninitiative); search it only when the person names it", a.Root)
	}
	if !b.s.agentAsleep(ctx, b.user, a.Root) {
		return nil
	}
	background := chat == nil || chat.Origin == "event" || chat.Origin == "reflect" || isMorningReport(chat)
	wake := in.Wake
	// A planned task may choose for itself (PLAN 72); empty keeps the account's choice.
	if w := b.s.scheduleWake(b.user.Name, chat); w != "" {
		wake = w
	}
	switch {
	case background || wake == "never":
		return toolcatalog.Errorf("the disk of %s is asleep and must not be woken now; tell the person and try later", a.Root)
	case wake == "ask":
		*mode = agent.Stricter(*mode, agent.ModeAsk)
	}
	return nil
}

// scheduleWake is the wake choice of the planned task that started a chat,
// "" for other chats and tasks that keep the account's choice.
func (s *Server) scheduleWake(user string, chat *agent.Chat) string {
	if chat == nil || chat.Origin != "schedule" || chat.Schedule == "" {
		return ""
	}
	list, err := s.agentStore().Schedules(user)
	if err != nil {
		return ""
	}
	for _, sc := range list {
		if sc.ID == chat.Schedule {
			return sc.Wake
		}
	}
	return ""
}

// namedInChat: the last message of the person names the root ("Backup").
func namedInChat(chat *agent.Chat, root string) bool {
	if chat == nil {
		return false
	}
	name := strings.ToLower(root[strings.LastIndex(root, "/")+1:])
	for i := len(chat.Entries) - 1; i >= 0; i-- {
		if chat.Entries[i].Role == "user" {
			return strings.Contains(strings.ToLower(chat.Entries[i].Content), name)
		}
	}
	return false
}

// ---------- watch and events ----------

// agentInitiativeObserve hands a NAS message to every account that watches:
// a hint in "Vorschläge", or with "Handelt selbst" a chat of its own.
func (s *Server) agentInitiativeObserve(m notify.Message) {
	env := s.agentEnv()
	settings := s.agentSettings()
	if !settings.Enabled || env.users == nil {
		return
	}
	event := agentEventKind(m.Kind)
	warning := m.Severity == notify.Warning || m.Severity == notify.Error
	if !warning && event == "" {
		return
	}
	for _, u := range env.users.List() {
		if u.Disabled || (u.Role != auth.RoleOwner && u.Role != auth.RoleAdmin) || !s.agentAllowed(u) {
			continue
		}
		in, ok := s.agentInitiativeBG(u.Name)
		if !ok || in.Level == "quiet" || (warning && !in.Watch) || (!warning && !slices.Contains(in.Events, event)) {
			continue
		}
		if in.Level == "act" && !settings.React && !inQuiet(in, time.Now().In(s.agentLocation())) {
			s.agentObserveAs(u, m)
			continue
		}
		s.addAgentInbox(u.Name, agentInboxItem{Kind: "alert", Title: m.Title, Body: m.Text, Data: map[string]string{"kind": m.Kind, "subject": m.Subject}})
	}
}

func agentEventKind(kind string) string {
	switch {
	case kind == "usb_plugged":
		return "usb"
	case kind == "update_available" || kind == "docker_auto_updated":
		return "update"
	}
	return ""
}

// ---------- forecast ----------

type volumeSample struct {
	Day  string `json:"day"`
	Used uint64 `json:"used"`
	Size uint64 `json:"size"`
}

var agentForecast struct {
	sync.Mutex
	samples map[string][]volumeSample
	day     string
	loaded  bool
}

// agentForecastTick takes one sample of each volume a day (statfs from
// kernel memory, no disk is woken) and warns when one fills up within 60 days.
func (s *Server) agentForecastTick(now time.Time) {
	env := s.agentEnv()
	if env.storage == nil {
		return
	}
	day := now.Format("2006-01-02")
	agentForecast.Lock()
	if agentForecast.day == day || now.Hour() < 4 {
		agentForecast.Unlock()
		return
	}
	agentForecast.day = day
	agentForecast.Unlock()
	s.loadForecast()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := env.storage.Status(ctx)
	if err != nil {
		return
	}
	agentForecast.Lock()
	warn := map[string]int{}
	for _, v := range st.Volumes {
		if !v.Mounted || v.SizeBytes == 0 {
			continue
		}
		list := append(agentForecast.samples[v.Name], volumeSample{Day: day, Used: v.UsedBytes, Size: v.SizeBytes})
		if len(list) > 60 {
			list = list[len(list)-60:]
		}
		agentForecast.samples[v.Name] = list
		if days := daysUntilFull(list); days > 0 && days <= 60 {
			warn[v.Name] = days
		}
	}
	agentForecast.Unlock()
	s.saveForecast()
	if len(warn) == 0 || env.users == nil {
		return
	}
	for _, u := range env.users.List() {
		if u.Disabled || (u.Role != auth.RoleOwner && u.Role != auth.RoleAdmin) || !s.agentAllowed(u) {
			continue
		}
		if in, ok := s.agentInitiativeBG(u.Name); !ok || !in.Forecast || in.Level == "quiet" {
			continue
		}
		for name, days := range warn {
			s.addAgentInbox(u.Name, agentInboxItem{Kind: "forecast", Title: fmt.Sprintf("%s ist in etwa %d Tagen voll", name, days),
				Body: "Seit den letzten Tagen wächst der belegte Platz gleichmäßig. Frag den Agenten, was so viel Platz braucht.", Data: map[string]string{"volume": name}})
		}
	}
}

// daysUntilFull: linear trend over at least 7 daily samples.
func daysUntilFull(list []volumeSample) int {
	if len(list) < 7 {
		return 0
	}
	first, last := list[0], list[len(list)-1]
	if last.Used <= first.Used {
		return 0
	}
	perDay := float64(last.Used-first.Used) / float64(len(list)-1)
	if last.Size <= last.Used {
		return 1
	}
	return int(float64(last.Size-last.Used)/perDay) + 1
}

// ---------- learning skills ----------

// agentLearn looks at a finished chat: the same flow of at least three
// different tools twice becomes a suggested skill.
func (s *Server) agentLearn(user string, chat *agent.Chat) {
	tools := []string{}
	for _, e := range chat.Entries {
		if e.Role == "tool" && e.OK && !slices.Contains(tools, e.Tool) {
			tools = append(tools, e.Tool)
		}
	}
	if len(tools) < 3 || chat.Origin == "reflect" {
		return
	}
	s.agentInit.mu.Lock()
	in, err := s.agentInitiativeLocked(user)
	if err != nil || !in.LearnSkills || in.Level == "quiet" {
		s.agentInit.mu.Unlock()
		return
	}
	sig := slices.Clone(tools)
	sort.Strings(sig)
	key := strings.Join(sig, ",")
	if slices.Contains(in.Learned, key) {
		s.agentInit.mu.Unlock()
		return
	}
	if in.Flows == nil {
		in.Flows = map[string]int{}
	}
	in.Flows[key]++
	propose := in.Flows[key] >= 2
	if propose {
		in.Learned = append(in.Learned, key)
		delete(in.Flows, key)
	}
	if len(in.Flows) > 50 {
		in.Flows = map[string]int{key: in.Flows[key]}
	}
	_ = s.saveAgentInitiativeLocked(user, in)
	s.agentInit.mu.Unlock()
	if !propose {
		return
	}
	when := ""
	for _, e := range chat.Entries {
		if e.Role == "user" {
			when = []string{e.Content}[0]
			break
		}
	}
	if r := []rune(when); len(r) > 160 {
		when = string(r[:160])
	}
	steps := []string{}
	for i, t := range tools {
		steps = append(steps, fmt.Sprintf("%d. %s", i+1, t))
	}
	s.addAgentInbox(user, agentInboxItem{Kind: "skill", Title: chat.Title, Body: "Diesen Ablauf hast du zweimal gleich gemacht: " + strings.Join(tools, " → "),
		Data: map[string]string{"when": when, "steps": strings.Join(steps, "\n"), "chat": chat.ID}})
}

// inQuiet: now lies in the account's quiet hours (may span midnight).
func inQuiet(in agentInitiative, now time.Time) bool {
	hm := now.Format("15:04")
	if in.QuietFrom == in.QuietTo {
		return false
	}
	if in.QuietFrom < in.QuietTo {
		return hm >= in.QuietFrom && hm < in.QuietTo
	}
	return hm >= in.QuietFrom || hm < in.QuietTo
}

// isMorningReport: the morning report counts as work in the background.
func isMorningReport(chat *agent.Chat) bool {
	return chat != nil && chat.Origin == "schedule" && len(chat.Entries) > 0 && strings.HasPrefix(chat.Entries[0].Content, "Morgenbericht:")
}

// The samples of the forecast survive updates in the owner's agent folder,
// read and written only while the agent's volume is awake.
func (s *Server) loadForecast() {
	agentForecast.Lock()
	defer agentForecast.Unlock()
	if agentForecast.samples == nil {
		agentForecast.samples = map[string][]volumeSample{}
	}
	owner := s.firstOwner()
	if agentForecast.loaded || owner == "" || s.agentVolumeAsleep() {
		return
	}
	saved := map[string][]volumeSample{}
	if err := s.agentStore().ReadPrivateLimit(owner, "forecast.json", &saved, maxAgentPrivate); err != nil && !errors.Is(err, agent.ErrNotFound) {
		return
	}
	agentForecast.loaded = true
	// Samples of today taken before reading stay; older ones come back.
	for name, list := range saved {
		cur := agentForecast.samples[name]
		agentForecast.samples[name] = append(list, cur...)
	}
}

func (s *Server) saveForecast() {
	owner := s.firstOwner()
	if owner == "" || s.agentVolumeAsleep() {
		return
	}
	agentForecast.Lock()
	data := map[string][]volumeSample{}
	for k, v := range agentForecast.samples {
		data[k] = slices.Clone(v)
	}
	agentForecast.Unlock()
	if len(data) > 0 {
		_ = s.agentStore().WritePrivate(owner, "forecast.json", data)
	}
}
