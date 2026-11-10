package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Gruppe „Schutz“ (PLAN 54-S): Ransomware-Wächter (agent_protect_guard.go),
// Notfallhandbuch (agent_protect_handbook.go), Notfallzugang
// (agent_protect_emergency.go). All three are NAS-wide, set by owners and
// administrators, and off by default. Their state lives in .titanos/agent
// on the agent's volume (WriteShared), never on the stick and never only in
// RAM; the stick is written only when a share's access changes (a lock, a
// grant), never in a loop.

// Guard modes (54-S.1): only tell; lock the share and tell (ab Werk);
// additionally put the files back by itself when the attack stopped.
const (
	guardReport = "report"
	guardLock   = "lock"
	guardAuto   = "auto"
)

type agentGuard struct {
	Enabled bool   `json:"enabled,omitempty"`
	Mode    string `json:"mode,omitempty"`
	// Thresholds per share and device within WindowSec (0: value ab Werk).
	Known     int `json:"known,omitempty"`
	Renames   int `json:"renames,omitempty"`
	Rewrites  int `json:"rewrites,omitempty"`
	Notes     int `json:"notes,omitempty"`
	WindowSec int `json:"windowSec,omitempty"`
	QuietSec  int `json:"quietSec,omitempty"`
	// Exclude are shares that are not watched (e.g. a backup target).
	Exclude []string `json:"exclude,omitempty"`
	// NoPreSnapshots switches off the hourly snapshot taken while a Btrfs
	// volume is written to anyway (the point to go back to).
	NoPreSnapshots bool `json:"noPreSnapshots,omitempty"`
}

// thresholds are the detector's values, ab Werk where not set.
func (g agentGuard) thresholds() protect.Thresholds {
	t := protect.DefaultThresholds()
	if g.Known > 0 {
		t.Known = g.Known
	}
	if g.Renames > 0 {
		t.Renames = g.Renames
	}
	if g.Rewrites > 0 {
		t.Rewrites = g.Rewrites
	}
	if g.Notes > 0 {
		t.Notes = g.Notes
	}
	if g.WindowSec > 0 {
		t.Window = time.Duration(g.WindowSec) * time.Second
	}
	if g.QuietSec > 0 {
		t.Quiet = time.Duration(g.QuietSec) * time.Second
	}
	return t
}

func (g agentGuard) mode() string {
	if g.Mode == "" {
		return guardLock
	}
	return g.Mode
}

// view shows the values in effect, so the page shows the defaults.
func (g agentGuard) view() agentGuard {
	t := g.thresholds()
	g.Mode = g.mode()
	g.Known, g.Renames, g.Rewrites, g.Notes = t.Known, t.Renames, t.Rewrites, t.Notes
	g.WindowSec, g.QuietSec = int(t.Window/time.Second), int(t.Quiet/time.Second)
	if g.Exclude == nil {
		g.Exclude = []string{}
	}
	return g
}

type agentHandbook struct {
	Enabled bool `json:"enabled,omitempty"`
	// Probes adds the results of the backup restore probes (off ab Werk).
	Probes bool `json:"probes,omitempty"`
	// WithContacts adds Contacts (off ab Werk).
	WithContacts bool              `json:"withContacts,omitempty"`
	Contacts     []protect.Contact `json:"contacts,omitempty"`
}

func (h agentHandbook) view() agentHandbook {
	if h.Contacts == nil {
		h.Contacts = []protect.Contact{}
	}
	return h
}

type agentEmergency struct {
	Enabled bool             `json:"enabled,omitempty"`
	Persons []protect.Person `json:"persons,omitempty"`
	// Code lets an owner print a one-time code per person that grants at
	// once (off ab Werk).
	Code bool `json:"code,omitempty"`
}

func (e agentEmergency) view() agentEmergency {
	if e.Persons == nil {
		e.Persons = []protect.Person{}
	}
	return e
}

func (e agentEmergency) person(account string) *protect.Person {
	for i := range e.Persons {
		if e.Persons[i].Account == account {
			p := e.Persons[i]
			return &p
		}
	}
	return nil
}

// checkProtect validates the settings of the group. It runs while the
// agent settings are locked: it must not call agentSettings.
func (s *Server) checkProtect(f *agentFeatures, old agentFeatures) error {
	env := s.agentEnv()
	g := &f.Guard
	switch g.Mode {
	case "", guardReport, guardLock, guardAuto:
	default:
		return fmt.Errorf("unknown guard mode %q", g.Mode)
	}
	for _, n := range []*int{&g.Known, &g.Renames, &g.Rewrites, &g.Notes} {
		if *n < 0 || *n > 100000 {
			return errors.New("guard thresholds must be between 1 and 100000")
		}
	}
	if g.WindowSec < 0 || g.WindowSec > 3600 || g.QuietSec < 0 || g.QuietSec > 3600 {
		return errors.New("guard times must be at most an hour")
	}
	// Values equal to those ab Werk are not stored, so a later default
	// change reaches everyone who did not choose.
	def := agentGuard{}.view()
	if g.Mode == guardLock {
		g.Mode = ""
	}
	for _, p := range [][2]*int{{&g.Known, &def.Known}, {&g.Renames, &def.Renames}, {&g.Rewrites, &def.Rewrites}, {&g.Notes, &def.Notes}, {&g.WindowSec, &def.WindowSec}, {&g.QuietSec, &def.QuietSec}} {
		if *p[0] == *p[1] {
			*p[0] = 0
		}
	}
	names := map[string]bool{}
	if env.shares != nil {
		for _, sh := range env.shares.List() {
			names[sh.Name] = true
		}
	}
	g.Exclude = uniqueSorted(g.Exclude)
	for _, n := range g.Exclude {
		if !names[n] {
			return fmt.Errorf("unknown share %q", n)
		}
	}
	if len(g.Exclude) == 0 {
		g.Exclude = nil
	}

	h := &f.Handbook
	if len(h.Contacts) > 10 {
		return errors.New("at most 10 contacts")
	}
	for i := range h.Contacts {
		c := &h.Contacts[i]
		c.Name, c.Phone, c.Note = strings.TrimSpace(c.Name), strings.TrimSpace(c.Phone), strings.TrimSpace(c.Note)
		if c.Name == "" || len(c.Name) > 80 || len(c.Phone) > 40 || len(c.Note) > 200 || strings.ContainsAny(c.Name+c.Phone+c.Note, "\x00\r\n") {
			return errors.New("invalid contact")
		}
	}
	if len(h.Contacts) == 0 {
		h.Contacts = nil
	}

	e := &f.Emergency
	if len(e.Persons) > 20 {
		return errors.New("at most 20 persons")
	}
	seen := map[string]bool{}
	for i := range e.Persons {
		p := &e.Persons[i]
		user, ok := env.users.Get(p.Account)
		if !ok {
			return fmt.Errorf("unknown account %q", p.Account)
		}
		if user.Disabled {
			return fmt.Errorf("account %q is locked", p.Account)
		}
		if seen[p.Account] {
			return fmt.Errorf("account %q twice", p.Account)
		}
		seen[p.Account] = true
		if p.WaitDays == 0 {
			p.WaitDays = 3
		}
		if p.WaitDays < 1 || p.WaitDays > 60 {
			return errors.New("the waiting time is 1 to 60 days")
		}
		p.Shares = uniqueSorted(p.Shares)
		if len(p.Shares) == 0 {
			return fmt.Errorf("no share for %q", p.Account)
		}
		for _, n := range p.Shares {
			if !names[n] {
				return fmt.Errorf("unknown share %q", n)
			}
		}
	}
	if len(e.Persons) == 0 {
		e.Persons = nil
	}
	// Switched off, a person removed or their shares changed: open
	// requests and grants follow once the settings are saved.
	if !slices.EqualFunc(old.Emergency.Persons, e.Persons, samePerson) || old.Emergency.Enabled != e.Enabled || old.Guard.Enabled != g.Enabled {
		protectGo("reconcile", func() {
			time.Sleep(50 * time.Millisecond)
			s.protectReconcile(time.Now())
		})
	}
	return nil
}

func samePerson(a, b protect.Person) bool {
	return a.Account == b.Account && a.WaitDays == b.WaitDays && slices.Equal(a.Shares, b.Shares)
}

func uniqueSorted(in []string) []string {
	out := slices.Clone(in)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	out = slices.DeleteFunc(out, func(s string) bool { return s == "" })
	slices.Sort(out)
	return slices.Compact(out)
}

// ---------- state ----------

// protectState is the state of the group in RAM, written through to the
// volume on every change. It hangs off the server by pointer (the Server
// type belongs to all groups).
type protectState struct {
	mu sync.Mutex
	// accessMu orders every change of a share's access (lock, reopen,
	// grant, revoke).
	accessMu sync.Mutex

	runner  run.Runner
	source  protect.Source
	sampler protect.Sampler
	now     func() time.Time

	// guard
	detector    *protect.Detector
	watchCancel context.CancelFunc
	watchKey    string
	watchErr    string
	// watchFailed and watchBackoff retry a watcher that stopped with an
	// error (1 min, doubling up to 30 min).
	watchFailed  time.Time
	watchBackoff time.Duration
	watchSince   time.Time
	overflows    int
	alerts       []guardAlert
	loaded       bool
	lastPre      map[string]time.Time // volume → last pre-snapshot
	sessions     map[int]protect.Client
	sessionsAt   time.Time

	// emergency
	emergency *protect.Emergency

	// handbook
	handbookTried time.Time
}

var protectStates sync.Map // *Server → *protectState

// protectGo runs background work of the group; a panic is logged and
// ends only that work, never titand.
func protectGo(what string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("protection work crashed", "work", what, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		f()
	}()
}

func (s *Server) protect() *protectState {
	if v, ok := protectStates.Load(s); ok {
		return v.(*protectState)
	}
	ps := &protectState{runner: run.Exec{}, source: protect.Fanotify{}, now: time.Now, lastPre: map[string]time.Time{}}
	ps.emergency = protect.NewEmergency(sharedStore{s, emergencyFile}, func(err error) bool { return errors.Is(err, agent.ErrNotFound) })
	v, _ := protectStates.LoadOrStore(s, ps)
	return v.(*protectState)
}

// sharedStore keeps one file in .titanos/agent of the agent's volume.
type sharedStore struct {
	s    *Server
	name string
}

func (st sharedStore) Load(v any) error { return st.s.agentStore().ReadShared(st.name, v) }
func (st sharedStore) Save(v any) error { return st.s.agentStore().WriteShared(st.name, v) }

const (
	guardFile     = "protect-guard.json"
	emergencyFile = "protect-emergency.json"
	handbookFile  = "protect-handbook.json"
)

// ---------- routes, tick, tools ----------

func (s *Server) registerAgentProtect(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/agent/protect/me", s.requireUser(s.handleProtectMe))
	// Ransomware-Wächter
	mux.HandleFunc("GET /api/v1/agent/protect/guard", s.requireAdmin(s.handleGuard))
	mux.HandleFunc("POST /api/v1/agent/protect/guard/{id}/restore", s.requireAdmin(s.handleGuardRestore))
	mux.HandleFunc("POST /api/v1/agent/protect/guard/{id}/reopen", s.requireAdmin(s.handleGuardReopen))
	mux.HandleFunc("POST /api/v1/agent/protect/guard/{id}/dismiss", s.requireAdmin(s.handleGuardDismiss))
	// Notfallhandbuch
	mux.HandleFunc("GET /api/v1/agent/protect/handbook", s.requireUser(s.handleHandbook))
	mux.HandleFunc("GET /api/v1/agent/protect/handbook/download", s.requireUser(s.handleHandbookDownload))
	mux.HandleFunc("POST /api/v1/agent/protect/handbook/refresh", s.requireAdmin(s.handleHandbookRefresh))
	// Notfallzugang
	mux.HandleFunc("GET /api/v1/agent/protect/emergency", s.requireAdmin(s.handleEmergency))
	mux.HandleFunc("POST /api/v1/agent/protect/emergency/request", s.requireUser(s.handleEmergencyRequest))
	mux.HandleFunc("POST /api/v1/agent/protect/emergency/{id}/{action}", s.requireUser(s.handleEmergencyAction))
	mux.HandleFunc("POST /api/v1/agent/protect/emergency/code/{account}", s.requireAdmin(s.handleEmergencyCode))
}

// handleProtectMe tells any account what of the group is there for it:
// administrators the open alerts and requests (for a hint in the agent),
// a registered person their emergency access.
func (s *Server) handleProtectMe(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	f := s.agentSettings().Features
	out := map[string]any{"guard": f.Guard.Enabled && user.Role.CanAdminister(), "handbook": f.Handbook.Enabled && user.Role.CanAdminister(), "emergency": false}
	now := s.protect().now()
	if user.Role.CanAdminister() {
		out["alerts"] = s.guardOpenCount()
		if f.Emergency.Enabled {
			out["requests"] = s.emergencyWaitingCount()
		}
	}
	if f.Emergency.Enabled {
		if p := f.Emergency.person(user.Name); p != nil {
			out["emergency"] = true
			out["person"] = p
			out["codes"] = f.Emergency.Code
			if req, ok := s.emergencyLatest(user.Name); ok {
				out["request"] = req
			}
			if _, ok := s.protect().emergency.Active(user.Name, now); ok && f.Handbook.Enabled {
				out["handbook"] = true
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// agentProtectTick runs once a minute from the agent's tick.
func (s *Server) agentProtectTick(now agentNow) {
	f := s.agentSettings().Features
	s.guardTick(f.Guard, now)
	s.emergencyTick(f.Emergency, now)
	s.handbookTick(f.Handbook, now)
}

// protectReconcile brings requests, grants and the watcher in line with
// the settings right after they changed.
func (s *Server) protectReconcile(now time.Time) {
	f := s.agentSettings().Features
	s.emergencyReconcile(f.Emergency, now)
	if !f.Guard.Enabled {
		s.guardStop()
	}
}

func (b *agentToolbox) protectTools() []*agentTool {
	f := b.s.agentSettings().Features
	var tools []*agentTool
	if f.Guard.Enabled && b.user.Role.CanAdminister() {
		tools = append(tools, &agentTool{
			name: "ransomware_guard_status", area: agent.AreaAgent, kind: agent.KindRead,
			description: "The state of the ransomware guard: whether it watches, its mode and the recent alerts (share, device, account, number of files, snapshot). Restoring or reopening is done by the person in the alert view, not by you.",
			params:      schema(map[string]any{}),
			own: func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
				res, err := toolcatalog.JSON(b.s.guardStatusFor(5))
				return agent.Outcome{Text: res.Text}, err
			},
		})
	}
	if f.Handbook.Enabled && b.s.handbookAllowed(b.user) {
		tools = append(tools, &agentTool{
			name: "emergency_handbook", area: agent.AreaAgent, kind: agent.KindRead,
			description: "The emergency handbook of this NAS as Markdown: volumes with RAID level and disks (model, serial number), shares, apps, VMs, network, remote access and what to do when a disk fails, the NAS does not start, the stick is broken or data must come back. It holds no passwords.",
			params:      schema(map[string]any{}),
			own: func(ctx context.Context, _ *agent.Chat, _ json.RawMessage) (agent.Outcome, error) {
				hb, err := b.s.handbookCurrent(ctx)
				if err != nil {
					return agent.Outcome{}, err
				}
				if !b.user.Role.CanAdminister() {
					b.s.emergencyRecord(b.user.Name, "emergency handbook read", b.user.Name+" (agent)")
				}
				return agent.Outcome{Text: protect.Markdown(hb.Facts, hb.Made.In(b.s.agentLocation()), true)}, nil
			},
		})
	}
	if f.Emergency.Enabled && f.Emergency.person(b.user.Name) != nil {
		tools = append(tools, &agentTool{
			name: "emergency_access_request", area: agent.AreaAgent, kind: agent.KindCreate,
			description: "Ask for the emergency access of this NAS (only for registered persons): the owners are told and may decline; without an answer read access to the chosen shares follows after the waiting time. Use it only when the person asks for it and give their reason.",
			params:      schema(map[string]any{"reason": str("Why the access is needed, in the person's words.")}, "reason"),
			own: func(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
				var req struct{ Reason string }
				if err := json.Unmarshal(args, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
					return agent.Outcome{}, toolcatalog.Errorf("a reason is needed")
				}
				out, err := b.s.emergencyAsk(b.user, req.Reason, "", "agent")
				if err != nil {
					return agent.Outcome{}, toolcatalog.Errorf("%v", err)
				}
				return agent.Outcome{Text: fmt.Sprintf("Requested. Without an answer the access is granted on %s.", out.Due.In(b.s.agentLocation()).Format("02.01.2006 15:04"))}, nil
			},
		})
	}
	return tools
}

// handbookAllowed: owners and administrators, and a person with a granted
// emergency access.
func (s *Server) handbookAllowed(user auth.User) bool {
	if user.Role.CanAdminister() {
		return true
	}
	if !s.agentSettings().Features.Emergency.Enabled {
		return false
	}
	_, ok := s.protect().emergency.Active(user.Name, s.protect().now())
	return ok
}
