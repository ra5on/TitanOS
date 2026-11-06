package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/buddy"
)

// Backup-Buddy (PLAN 54-G), the API side: pairing, the partners, the
// backup job that sends to a partner and the server that takes in what a
// partner sends (package buddy).
//
// The sending side is an ordinary backup job of kind "rest" named
// buddy-<partner>: restic encrypts with the password of the job, which
// never leaves this NAS; it signs in at the partner with the token it got
// at pairing. The job shows on the Backups page like any other, with its
// restore and probe. It keeps 8 weekly and 12 monthly versions, so it never
// asks the partner to delete anything younger than the protection period.

const (
	buddyFile        = "system/agent-buddy.json"
	maxBuddyPartners = 8
	buddyJobPrefix   = "buddy-"
)

// buddyPartner is one paired NAS.
type buddyPartner struct {
	// ID is the name this NAS gave the partner (its folder here and its
	// user name when it signs in); Remote the name the partner gave this NAS.
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Remote  string `json:"remote"`
	// Hash is the SHA-256 of the token the partner signs in with here.
	Hash string `json:"hash"`
	// Token is what this NAS signs in with at the partner (secret).
	Token string `json:"token"`
	// Quota is what this NAS grants the partner; TheirQuota what the
	// partner grants this NAS (as it said at pairing).
	Quota      int64     `json:"quota"`
	TheirQuota int64     `json:"theirQuota"`
	Shares     []string  `json:"shares"`
	Paired     time.Time `json:"paired"`
}

type buddyCodeMeta struct {
	Quota  int64
	Shares []string
}

// buddyLoad reads the partners once; afterwards they are kept in RAM.
func (s *Server) buddyLoad() error {
	f := s.family54()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loaded {
		return nil
	}
	var list []buddyPartner
	if err := s.opts.Store.ReadJSON(buddyFile, &list); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f.partners, f.loaded = list, true
	return nil
}

// buddySave writes the partners (only when pairing changes them).
func (s *Server) buddySaveLocked(f *family54, next []buddyPartner) error {
	if next == nil {
		next = []buddyPartner{}
	}
	if err := s.opts.Store.WriteJSON(buddyFile, next); err != nil {
		return err
	}
	f.partners = next
	return nil
}

func (s *Server) buddyPartners() []buddyPartner {
	f := s.family54()
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.partners)
}

func (s *Server) buddySettings() agentBuddy {
	return s.agentSettings().Features.Buddy.withDefaults()
}

func (s *Server) mountedVolumes() []string {
	if s.opts.Storage == nil {
		return nil
	}
	return s.opts.Storage.Mounted()
}

// buddyServer is the receiving side, made once.
func (s *Server) buddyServer() *buddy.Server {
	f := s.family54()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.server != nil {
		return f.server
	}
	srv := buddy.NewServer()
	srv.Partner = func(id string) (buddy.Partner, bool) {
		for _, p := range s.buddyPartners() {
			if p.ID == id {
				return buddy.Partner{ID: p.ID, Hash: p.Hash, Quota: p.Quota}, true
			}
		}
		return buddy.Partner{}, false
	}
	srv.Volume = func() string { return s.buddySettings().Volume }
	srv.Mounted = s.mountedVolumes
	srv.ProtectDays = func() int { return s.buddySettings().ProtectDays }
	srv.Pair = s.buddyAnswerPair
	f.server = srv
	return srv
}

// buddyHandler refuses everything while the function is off.
func (s *Server) buddyHandler() http.Handler {
	inner := s.buddyServer().Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Off with the function and with the agent: the tick, which closes
		// the listener, only runs while the agent is on.
		if settings := s.agentSettings(); !settings.Enabled || !settings.Features.Buddy.Enabled {
			http.NotFound(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

var nonID = regexp.MustCompile(`[^a-z0-9-]+`)

// buddyNewID makes a folder name from a host name, unused so far.
func buddyNewID(name string, taken []buddyPartner) string {
	base := strings.ToLower(name)
	if host, _, err := net.SplitHostPort(base); err == nil {
		base = host
	}
	base, _, _ = strings.Cut(base, ".")
	base = strings.Trim(nonID.ReplaceAllString(base, "-"), "-")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "nas-" + base
	}
	if len(base) > 20 {
		base = strings.TrimRight(base[:20], "-")
	}
	id := base
	for i := 2; slices.ContainsFunc(taken, func(p buddyPartner) bool { return p.ID == id }); i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	return id
}

func hostName() string {
	name, _ := os.Hostname()
	if name == "" {
		name = "titanos"
	}
	return name
}

// buddyAnswerPair answers the pairing request of another NAS that entered
// the code shown here.
func (s *Server) buddyAnswerPair(req buddy.PairRequest) (buddy.PairResponse, error) {
	settings := s.buddySettings()
	if !settings.Enabled {
		return buddy.PairResponse{}, buddy.ErrNoCode
	}
	address, err := buddy.CheckAddress(req.Address)
	if err != nil {
		return buddy.PairResponse{}, err
	}
	if !buddy.ValidID(req.ID) || len(req.Token) < 32 || len(req.Token) > 256 || req.Quota < 0 || len(req.Name) > 64 {
		return buddy.PairResponse{}, errors.New("buddy: invalid pairing request")
	}
	if err := s.buddyLoad(); err != nil {
		return buddy.PairResponse{}, err
	}
	f := s.family54()
	meta, err := f.codes.Redeem(req.Code)
	if err != nil {
		return buddy.PairResponse{}, err
	}
	m, _ := meta.(buddyCodeMeta)
	token, err := buddy.NewToken()
	if err != nil {
		return buddy.PairResponse{}, err
	}
	f.mu.Lock()
	if len(f.partners) >= maxBuddyPartners {
		f.mu.Unlock()
		return buddy.PairResponse{}, errors.New("buddy: too many partners")
	}
	name := req.Name
	if name == "" {
		name = address
	}
	p := buddyPartner{ID: buddyNewID(name, f.partners), Name: name, Address: address, Remote: req.ID, Hash: buddy.HashToken(token), Token: req.Token,
		Quota: m.Quota, TheirQuota: req.Quota, Shares: m.Shares, Paired: time.Now().UTC().Truncate(time.Second)}
	err = s.buddySaveLocked(f, append(slices.Clone(f.partners), p))
	f.mu.Unlock()
	if err != nil {
		return buddy.PairResponse{}, err
	}
	_ = s.buddySyncJob(p) // logged and shown in the view
	slog.Info("buddy paired", "partner", p.ID, "address", address)
	return buddy.PairResponse{Name: hostName(), ID: p.ID, Token: token, Quota: m.Quota}, nil
}

// buddyPairClient is the client for pairing requests (tests replace it).
var buddyPairClient = buddy.Client{}

// ---------- routes ----------

type buddyJobView struct {
	LastRun   *time.Time          `json:"lastRun"`
	LastOK    *time.Time          `json:"lastOK"`
	LastError string              `json:"lastError,omitempty"`
	Bytes     uint64              `json:"bytes"`
	Running   bool                `json:"running"`
	Next      *time.Time          `json:"next"`
	Probe     *backup.ProbeResult `json:"probe,omitempty"`
}

type buddyPartnerView struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Address    string        `json:"address"`
	Quota      int64         `json:"quota"`
	Used       int64         `json:"used"`
	UsedKnown  bool          `json:"usedKnown"`
	TheirQuota int64         `json:"theirQuota"`
	Shares     []string      `json:"shares"`
	Paired     time.Time     `json:"paired"`
	Job        *buddyJobView `json:"job"`
	// JobError says why the job to the partner could not be saved.
	JobError string `json:"jobError,omitempty"`
}

func (s *Server) buddyJobs() map[string]backup.View {
	out := map[string]backup.View{}
	if s.opts.Backup == nil {
		return out
	}
	for _, v := range s.opts.Backup.CachedViews() {
		if strings.HasPrefix(v.Job.ID, buddyJobPrefix) {
			out[v.Job.ID] = v
		}
	}
	return out
}

func (s *Server) handleBuddy(w http.ResponseWriter, r *http.Request) {
	if err := s.buddyLoad(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	settings := s.buddySettings()
	f := s.family54()
	jobs := s.buddyJobs()
	partners := []buddyPartnerView{}
	for _, p := range s.buddyPartners() {
		v := buddyPartnerView{ID: p.ID, Name: p.Name, Address: p.Address, Quota: p.Quota, TheirQuota: p.TheirQuota, Shares: p.Shares, Paired: p.Paired}
		if v.Shares == nil {
			v.Shares = []string{}
		}
		v.Used, v.UsedKnown = s.buddyServer().Usage(p.ID)
		if job, ok := jobs[buddyJobPrefix+p.ID]; ok {
			v.Job = &buddyJobView{LastRun: job.State.LastRun, LastOK: job.State.LastOK, LastError: job.State.LastError, Bytes: job.State.Bytes, Running: job.State.Running, Next: job.Next, Probe: job.State.Probe}
		}
		f.mu.Lock()
		v.JobError = f.jobErr[p.ID]
		f.mu.Unlock()
		partners = append(partners, v)
	}
	shares := []string{}
	if s.opts.Backup != nil && s.opts.Backup.Sources != nil {
		for _, src := range s.opts.Backup.Sources() {
			shares = append(shares, src.Name)
		}
	}
	var code any
	if c, expires, ok := f.codes.Active(); ok {
		code = map[string]any{"code": c, "expires": expires}
	}
	f.mu.Lock()
	listening := slices.Clone(f.addresses)
	f.mu.Unlock()
	if listening == nil {
		listening = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings, "name": hostName(), "partners": partners, "code": code,
		"volumes": s.mountedVolumes(), "shares": shares, "listening": listening, "problem": s.buddyProblem(settings),
	})
}

// buddyProblem says why partners cannot reach this NAS, if they cannot.
func (s *Server) buddyProblem(settings agentBuddy) string {
	switch {
	case !settings.Enabled:
		return "off"
	case settings.Volume == "" || !slices.Contains(s.mountedVolumes(), settings.Volume):
		return "volume"
	case settings.Address == "":
		return "address"
	case s.opts.Tailscale == nil:
		return "tailscale"
	}
	f := s.family54()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.addresses) == 0 {
		return "tailscale"
	}
	return ""
}

func (s *Server) buddyCheckShares(shares []string) ([]string, error) {
	if len(shares) > 32 {
		return nil, errors.New("too many shares")
	}
	known := []string{}
	if s.opts.Backup != nil && s.opts.Backup.Sources != nil {
		for _, src := range s.opts.Backup.Sources() {
			known = append(known, src.Name)
		}
	}
	out := []string{}
	for _, sh := range shares {
		if !slices.Contains(known, sh) {
			return nil, fmt.Errorf("unknown share %q", sh)
		}
		if !slices.Contains(out, sh) {
			out = append(out, sh)
		}
	}
	return out, nil
}

func (s *Server) handleBuddyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quota  int64    `json:"quota"`
		Shares []string `json:"shares"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.buddySettings().Enabled {
		writeError(w, http.StatusConflict, "buddy_off", "the Backup-Buddy is off")
		return
	}
	shares, err := s.buddyCheckShares(req.Shares)
	if err != nil || req.Quota < 1<<30 || req.Quota > 1<<50 {
		writeError(w, http.StatusBadRequest, "invalid_buddy", "a quota of at least 1 GiB and known shares")
		return
	}
	code, expires, err := s.family54().codes.Issue(buddyCodeMeta{Quota: req.Quota, Shares: shares})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "buddy_code", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expires": expires})
}

func (s *Server) handleBuddyCodeCancel(w http.ResponseWriter, r *http.Request) {
	s.family54().codes.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

// handleBuddyPair enters the code of another NAS: this NAS sends its
// pairing request there.
func (s *Server) handleBuddyPair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string   `json:"address"`
		Code    string   `json:"code"`
		Quota   int64    `json:"quota"`
		Shares  []string `json:"shares"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	settings := s.buddySettings()
	if !settings.Enabled {
		writeError(w, http.StatusConflict, "buddy_off", "the Backup-Buddy is off")
		return
	}
	if settings.Address == "" {
		writeError(w, http.StatusConflict, "buddy_address", "set the address of this NAS first, so the partner can reach it")
		return
	}
	address, err := buddy.CheckAddress(req.Address)
	shares, serr := s.buddyCheckShares(req.Shares)
	if err != nil || serr != nil || len(buddy.Normalize(req.Code)) != 9 || req.Quota < 1<<30 || req.Quota > 1<<50 {
		writeError(w, http.StatusBadRequest, "invalid_buddy", "address, code (9 characters), quota of at least 1 GiB and known shares")
		return
	}
	if err := s.buddyLoad(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	f := s.family54()
	if len(s.buddyPartners()) >= maxBuddyPartners {
		writeError(w, http.StatusConflict, "buddy_full", "too many partners")
		return
	}
	token, err := buddy.NewToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "buddy_token", err.Error())
		return
	}
	id := buddyNewID(address, s.buddyPartners())
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := buddyPairClient.Pair(ctx, address, buddy.PairRequest{Code: req.Code, Name: hostName(), Address: settings.Address, ID: id, Token: token, Quota: req.Quota})
	if err != nil {
		writeError(w, http.StatusBadGateway, "buddy_pair_failed", err.Error())
		return
	}
	p := buddyPartner{ID: id, Name: res.Name, Address: address, Remote: res.ID, Hash: buddy.HashToken(token), Token: res.Token,
		Quota: req.Quota, TheirQuota: res.Quota, Shares: shares, Paired: time.Now().UTC().Truncate(time.Second)}
	if p.Name == "" {
		p.Name = address
	}
	f.mu.Lock()
	err = s.buddySaveLocked(f, append(slices.Clone(f.partners), p))
	f.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	out := map[string]any{"id": p.ID, "name": p.Name}
	if err := s.buddySyncJob(p); err != nil {
		out["jobError"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBuddyPartnerSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quota  int64    `json:"quota"`
		Shares []string `json:"shares"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	shares, err := s.buddyCheckShares(req.Shares)
	if err != nil || req.Quota < 1<<30 || req.Quota > 1<<50 {
		writeError(w, http.StatusBadRequest, "invalid_buddy", "a quota of at least 1 GiB and known shares")
		return
	}
	if err := s.buddyLoad(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	f := s.family54()
	f.mu.Lock()
	at := slices.IndexFunc(f.partners, func(p buddyPartner) bool { return p.ID == r.PathValue("id") })
	if at < 0 {
		f.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "no such partner")
		return
	}
	next := slices.Clone(f.partners)
	next[at].Quota, next[at].Shares = req.Quota, shares
	err = s.buddySaveLocked(f, next)
	p := next[at]
	f.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	if err := s.buddySyncJob(p); err != nil {
		writeError(w, http.StatusBadRequest, "buddy_job", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBuddyPartnerDelete ends a partnership: its job here goes; with
// ?data=1 also what it stored here. What this NAS stored at the partner
// stays there until the partner removes it.
func (s *Server) handleBuddyPartnerDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.buddyLoad(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	id := r.PathValue("id")
	f := s.family54()
	f.mu.Lock()
	at := slices.IndexFunc(f.partners, func(p buddyPartner) bool { return p.ID == id })
	if at < 0 {
		f.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "no such partner")
		return
	}
	err := s.buddySaveLocked(f, slices.Delete(slices.Clone(f.partners), at, at+1))
	f.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	if s.opts.Backup != nil {
		if err := s.opts.Backup.Delete(buddyJobPrefix + id); err != nil && !errors.Is(err, backup.ErrNotFound) {
			slog.Warn("buddy job not removed", "partner", id, "err", err)
		}
	}
	if r.URL.Query().Get("data") == "1" {
		if err := s.buddyServer().RemoveData(id); err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusServiceUnavailable, "buddy_data", err.Error())
			return
		}
	}
	s.buddyServer().Forget(id)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- the sending job ----------

func (s *Server) buddyJob(p buddyPartner, settings agentBuddy) backup.Job {
	return backup.Job{
		ID:      buddyJobPrefix + p.ID,
		Sources: slices.Clone(p.Shares),
		Destination: backup.Destination{Type: "rest", URL: buddy.BaseURL(p.Address) + "/" + p.Remote,
			Username: p.Remote, Password: p.Token},
		Enabled: true,
		Every:   settings.Every, Weekday: settings.Weekday, MonthDay: 1, Time: settings.Time,
		// Never younger than the protection period of the partner.
		Keep: backup.Keep{Daily: 31, Weekly: 8, Monthly: 12},
	}
}

// buddySyncJob makes the job of a partner match: there while shares are
// chosen, gone without. It writes /config only when something differs.
func (s *Server) buddySyncJob(p buddyPartner) error {
	if s.opts.Backup == nil || s.opts.Storage == nil {
		return nil
	}
	id := buddyJobPrefix + p.ID
	have, exists := s.buddyJobs()[id]
	want := s.buddyJob(p, s.buddySettings())
	var err error
	switch {
	case len(p.Shares) == 0 && exists:
		err = s.opts.Backup.Delete(id)
	case len(p.Shares) == 0:
	case exists && !buddyJobDiffers(have.Job, want):
	case exists:
		err = s.opts.Backup.Update(id, want, s.opts.Storage.Names())
	default:
		err = s.opts.Backup.Create(want, s.opts.Storage.Names())
	}
	f := s.family54()
	f.mu.Lock()
	if err != nil {
		f.jobErr[p.ID] = err.Error()
	} else {
		delete(f.jobErr, p.ID)
	}
	f.mu.Unlock()
	if err != nil {
		slog.Warn("buddy job not saved", "partner", p.ID, "err", err)
	}
	return err
}

// buddyJobDiffers compares what the buddy sets of a job (the stored one
// comes without its secrets).
func buddyJobDiffers(have, want backup.Job) bool {
	return !slices.Equal(have.Sources, want.Sources) || have.Every != want.Every || have.Weekday != want.Weekday || have.Time != want.Time ||
		have.Destination.Type != want.Destination.Type || have.Destination.URL != want.Destination.URL || have.Destination.Username != want.Destination.Username ||
		have.Keep.Daily != want.Keep.Daily || have.Keep.Weekly != want.Keep.Weekly || have.Keep.Monthly != want.Keep.Monthly || !have.Enabled
}

// buddyTick keeps the server listening, the jobs on the schedule of the
// settings and starts the monthly probe restore right after a backup that
// worked (the disks are awake then).
func (s *Server) buddyTick(now time.Time) {
	settings := s.buddySettings()
	f := s.family54()
	if !settings.Enabled {
		f.listener.Reconcile(nil, nil)
		f.mu.Lock()
		f.addresses = nil
		f.mu.Unlock()
		return
	}
	if err := s.buddyLoad(); err != nil {
		return
	}
	// The network every five minutes: tailscale status is a command.
	f.mu.Lock()
	due := now.Unix()-f.lastNet >= 300
	if due {
		f.lastNet = now.Unix()
	}
	f.mu.Unlock()
	if due {
		var want []string
		devMode := s.opts.BackupTarget == nil || s.opts.BackupTarget.Disabled
		if s.opts.Tailscale != nil && !devMode && slices.Contains(s.mountedVolumes(), settings.Volume) {
			for _, ip := range buddy.TailscaleOnly(s.opts.Tailscale.Status().IPs) {
				want = append(want, net.JoinHostPort(ip, strconv.Itoa(settings.Port)))
			}
		}
		f.listener.Reconcile(want, s.buddyHandler())
		f.mu.Lock()
		f.addresses = f.listener.Addresses()
		f.mu.Unlock()
	}
	// A changed schedule goes to the jobs.
	key := fmt.Sprint(settings.Every, settings.Weekday, settings.Time)
	f.mu.Lock()
	// The first tick after a start compares too, but buddySyncJob writes
	// only what differs, so a start does not write to the stick.
	changed := f.synced != key
	f.synced = key
	f.mu.Unlock()
	if changed {
		for _, p := range s.buddyPartners() {
			if len(p.Shares) > 0 {
				_ = s.buddySyncJob(p) // logged and shown in the view
			}
		}
	}
	if settings.Probe != "monthly" || s.opts.Backup == nil {
		return
	}
	for id, job := range s.buddyJobs() {
		st := job.State
		if st.Running || st.LastOK == nil || now.Sub(*st.LastOK) > time.Hour {
			continue
		}
		if st.Probe != nil && now.Sub(st.Probe.At) < 30*24*time.Hour {
			continue
		}
		f.mu.Lock()
		tried := now.Unix()-f.probeTry[id] < 6*3600
		if !tried {
			f.probeTry[id] = now.Unix()
		}
		f.mu.Unlock()
		if tried {
			continue
		}
		if err := s.opts.Backup.Probe(id, backup.ProbeRequest{MaxBytes: 256 << 20, MaxFiles: settings.ProbeFiles}); err != nil {
			slog.Warn("buddy probe not started", "job", id, "err", err)
		}
	}
}
