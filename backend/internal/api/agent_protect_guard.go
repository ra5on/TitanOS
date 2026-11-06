package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// Ransomware-Wächter (PLAN 54-S.1). fanotify reports every write, rename
// and new file on the mounted data volumes (no polling, no reading of idle
// disks). Events are mapped to shares by path and counted per share and
// writing process by protect.Detector. When a threshold is crossed:
//
//	report: tell owners and administrators, show the alert;
//	lock (ab Werk): also make the share read-only (old access kept here),
//	   close the share's SMB connections and disconnect the device, and
//	   hold the snapshot from before the attack (Btrfs);
//	auto: also put the files back from that snapshot once it stopped.
//
// Without Btrfs the alert offers the restore of the backup instead.

// guardAlert is one alarm, kept in protect-guard.json.
type guardAlert struct {
	ID     string             `json:"id"`
	Share  string             `json:"share"`
	Volume string             `json:"volume"`
	Client protect.Client     `json:"client"`
	Reason string             `json:"reason"`
	Mode   string             `json:"mode"`
	First  time.Time          `json:"first"`
	Last   time.Time          `json:"last"`
	Count  int                `json:"count"`
	More   int                `json:"more,omitempty"`
	Files  []protect.Affected `json:"files"`
	// State: active (still going), stopped, restored, reopened, dismissed.
	State string `json:"state"`
	// Locked says the guard made the share read-only; Saved is the access
	// before (only the entries it changed), SavedNFS the NFS mode.
	Locked   bool              `json:"locked"`
	Saved    map[string]string `json:"saved,omitempty"`
	SavedNFS string            `json:"savedNfs,omitempty"`
	Btrfs    bool              `json:"btrfs"`
	// Snapshot is the one to go back to (from before the attack); Held the
	// read-only copy the guard keeps of it, when it was not its own.
	Snapshot   string                 `json:"snapshot,omitempty"`
	SnapshotAt time.Time              `json:"snapshotAt,omitzero"`
	Held       string                 `json:"held,omitempty"`
	Restored   *protect.RestoreResult `json:"restored,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Closed     time.Time              `json:"closed,omitzero"`
	By         string                 `json:"by,omitempty"`
}

func (a guardAlert) open() bool { return a.State == "active" || a.State == "stopped" }

const (
	maxGuardAlerts   = 20
	maxStoredFiles   = 3000
	guardPrePrefix   = "guard-pre-"
	guardHoldPrefix  = "guard-hold-"
	guardPreEvery    = time.Hour
	guardPreKeep     = 48 * time.Hour
	guardHoldKeep    = 14 * 24 * time.Hour
	guardFalseMute   = time.Hour
	guardSessionsTTL = 10 * time.Second
)

// ---------- state on the volume ----------

func (s *Server) guardLoadLocked(ps *protectState) {
	if ps.loaded {
		return
	}
	var list []guardAlert
	if err := s.agentStore().ReadShared(guardFile, &list); err != nil && !errors.Is(err, agent.ErrNotFound) {
		slog.Warn("ransomware guard state not readable", "err", err)
		return
	}
	ps.alerts, ps.loaded = list, true
}

func (s *Server) guardSaveLocked(ps *protectState) {
	if len(ps.alerts) > maxGuardAlerts {
		ps.alerts = ps.alerts[len(ps.alerts)-maxGuardAlerts:]
	}
	if err := s.agentStore().WriteShared(guardFile, ps.alerts); err != nil {
		slog.Error("ransomware guard state not saved", "err", err)
	}
}

func (s *Server) guardOpenCount() int {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s.guardLoadLocked(ps)
	n := 0
	for _, a := range ps.alerts {
		if a.open() {
			n++
		}
	}
	return n
}

// ---------- watching ----------

// guardMounts are the mount points of the mounted volumes.
func (s *Server) guardMounts() []string {
	if s.opts.Storage == nil {
		return nil
	}
	var out []string
	for _, v := range s.opts.Storage.Mounted() {
		out = append(out, s.opts.Storage.MountPoint(v))
	}
	slices.Sort(out)
	return out
}

func (s *Server) guardTick(g agentGuard, now time.Time) {
	ps := s.protect()
	if !g.Enabled {
		s.guardStop()
		return
	}
	ps.mu.Lock()
	if ps.detector == nil {
		ps.detector = protect.NewDetector(g.thresholds())
	} else {
		ps.detector.T = g.thresholds()
	}
	mounts := s.guardMounts()
	k := strings.Join(mounts, "\n")
	if ps.watchCancel == nil || ps.watchKey != k {
		if ps.watchCancel != nil {
			ps.watchCancel()
		}
		ctx, cancel := context.WithCancel(s.agentBase())
		ps.watchCancel, ps.watchKey, ps.watchErr, ps.watchSince = cancel, k, "", now
		go s.guardWatch(ctx, mounts)
	}
	det := ps.detector
	ps.mu.Unlock()

	// Incidents that are still going get their files; stopped ones end.
	s.guardRefresh(det)
	for _, inc := range det.Tick(now) {
		s.guardStopped(inc, g)
	}
}

func (s *Server) guardStop() {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.watchCancel != nil {
		ps.watchCancel()
		ps.watchCancel, ps.watchKey = nil, ""
	}
}

func (s *Server) guardWatch(ctx context.Context, mounts []string) {
	ps := s.protect()
	events := make(chan protect.Event, 4096)
	done := make(chan error, 1)
	src := ps.source
	if f, ok := src.(protect.Fanotify); ok && f.OnOverflow == nil {
		f.OnOverflow = func() {
			ps.mu.Lock()
			ps.overflows++
			ps.mu.Unlock()
		}
		src = f
	}
	go func() { done <- src.Watch(ctx, mounts, events) }()
	for {
		select {
		case ev := <-events:
			s.guardEvent(ev)
		case err := <-done:
			if err != nil && ctx.Err() == nil {
				slog.Error("ransomware guard is not watching", "err", err)
				ps.mu.Lock()
				ps.watchErr = err.Error()
				ps.mu.Unlock()
			}
			// Drain what is left, then stop.
			for {
				select {
				case ev := <-events:
					s.guardEvent(ev)
				default:
					return
				}
			}
		}
	}
}

// guardShareOf maps an absolute path to a watched share and the path in it.
func (s *Server) guardShareOf(p string, g agentGuard) (shares.Share, string, bool) {
	if s.opts.Shares == nil {
		return shares.Share{}, "", false
	}
	for _, sh := range s.opts.Shares.List() {
		dir := s.opts.Shares.Path(sh)
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		if slices.Contains(g.Exclude, sh.Name) || protect.Hidden(filepath.ToSlash(rel)) {
			return shares.Share{}, "", false
		}
		return sh, filepath.ToSlash(rel), true
	}
	return shares.Share{}, "", false
}

// guardEvent classifies one file event and feeds the detector.
func (s *Server) guardEvent(ev protect.Event) {
	g := s.agentSettings().Features.Guard
	ps := s.protect()
	sh, rel, ok := s.guardShareOf(ev.Path, g)
	if !ok {
		return
	}
	s.guardPreSnapshot(sh.Volume, g, ev.At)
	file := protect.Affected{Was: rel, Now: rel}
	kind := protect.KindNone
	switch ev.Op {
	case protect.OpRename:
		if _, from, ok := s.guardShareOf(ev.From, g); ok {
			file.Was = from
		}
		kind = protect.ClassifyRename(ev.From, ev.Path)
	case protect.OpCreate:
		if protect.IsNote(filepath.Base(ev.Path)) {
			kind = protect.KindNote
		}
	case protect.OpWrite:
		if protect.IsNote(filepath.Base(ev.Path)) {
			kind = protect.KindNote
		} else if protect.Judgeable(ev.Path) {
			if sample, ok := ps.sampler.Read(ev.Path, ev.At); ok && protect.LooksEncrypted(ev.Path, sample) {
				kind = protect.KindRewrite
			}
		}
	}
	if kind == protect.KindNone {
		return
	}
	ps.mu.Lock()
	det := ps.detector
	ps.mu.Unlock()
	if det == nil {
		return
	}
	if inc := det.Observe(sh.Name, "pid:"+strconv.Itoa(ev.PID), kind, file, ev.At); inc != nil {
		s.guardStart(*inc, ev.PID, g)
	}
}

// ---------- snapshots ----------

var snapStamp = regexp.MustCompile(`(\d{8})[-T](\d{6})`)

// snapshotTime reads the time from a snapshot's name: guard- names are UTC,
// the others (agent-, scheduled) local time.
func (s *Server) snapshotTime(name string) (time.Time, bool) {
	m := snapStamp.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	loc := s.agentLocation()
	if strings.HasPrefix(name, "guard-") {
		loc = time.UTC
	}
	at, err := time.ParseInLocation("20060102150405", m[1]+m[2], loc)
	return at, err == nil
}

func (s *Server) isBtrfs(volume string) bool {
	return s.opts.Storage != nil && s.opts.Storage.FilesystemOf(volume) == "btrfs"
}

// guardPreSnapshot keeps a point to go back to: while a Btrfs volume is
// written to anyway (its disks are awake), at most one read-only snapshot
// an hour, kept two days.
func (s *Server) guardPreSnapshot(volume string, g agentGuard, at time.Time) {
	if g.NoPreSnapshots || !s.isBtrfs(volume) {
		return
	}
	ps := s.protect()
	ps.mu.Lock()
	if at.Sub(ps.lastPre[volume]) < guardPreEvery {
		ps.mu.Unlock()
		return
	}
	ps.lastPre[volume] = at
	ps.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), time.Minute)
		defer cancel()
		name := guardPrePrefix + at.UTC().Format("20060102-150405")
		if err := s.opts.Storage.SnapshotAction(ctx, volume, name, "create", ""); err != nil {
			slog.Warn("ransomware guard snapshot not taken", "volume", volume, "err", err)
			return
		}
		s.guardPrune(ctx, volume, at)
	}()
}

// guardPrune removes the guard's snapshots of a volume that are old and
// not needed by an open alert. It runs right after a new one was taken,
// when the volume is awake.
func (s *Server) guardPrune(ctx context.Context, volume string, now time.Time) {
	list, err := s.opts.Storage.Snapshots(volume)
	if err != nil {
		return
	}
	ps := s.protect()
	ps.mu.Lock()
	s.guardLoadLocked(ps)
	needed := map[string]bool{}
	for _, a := range ps.alerts {
		if a.Volume == volume && (a.open() || now.Sub(a.Closed) < guardHoldKeep) {
			needed[a.Snapshot], needed[a.Held] = true, true
		}
	}
	ps.mu.Unlock()
	for _, snap := range list {
		keep := guardPreKeep
		switch {
		case strings.HasPrefix(snap.Name, guardPrePrefix):
		case strings.HasPrefix(snap.Name, guardHoldPrefix):
			keep = guardHoldKeep
		default:
			continue
		}
		at, ok := s.snapshotTime(snap.Name)
		if !ok || needed[snap.Name] || now.Sub(at) < keep {
			continue
		}
		if err := s.opts.Storage.SnapshotAction(ctx, volume, snap.Name, "delete", snap.Name); err != nil {
			slog.Warn("old ransomware guard snapshot not removed", "volume", volume, "snapshot", snap.Name, "err", err)
		}
	}
}

// guardFindSnapshot picks the newest snapshot of the volume from before
// the attack.
func (s *Server) guardFindSnapshot(volume string, before time.Time) (string, time.Time) {
	list, err := s.opts.Storage.Snapshots(volume)
	if err != nil {
		return "", time.Time{}
	}
	best, bestAt := "", time.Time{}
	for _, snap := range list {
		if strings.HasPrefix(snap.Name, guardHoldPrefix) {
			continue
		}
		at, ok := s.snapshotTime(snap.Name)
		if !ok || !at.Before(before) {
			continue
		}
		if at.After(bestAt) {
			best, bestAt = snap.Name, at
		}
	}
	return best, bestAt
}

func (s *Server) snapshotPath(volume, name string) string {
	return filepath.Join(s.opts.Storage.MountPoint(volume), ".titanos", "snapshots", name)
}

// guardHold keeps a read-only copy of a snapshot that is not the guard's
// own, so the retention of agent or scheduled snapshots cannot take it.
func (s *Server) guardHold(ctx context.Context, volume, snap string, at time.Time) string {
	if strings.HasPrefix(snap, guardPrePrefix) {
		return ""
	}
	name := guardHoldPrefix + at.UTC().Format("20060102-150405")
	dir := filepath.Join(s.opts.Storage.MountPoint(volume), ".titanos", "snapshots")
	if _, err := s.protect().runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", filepath.Join(dir, snap), filepath.Join(dir, name)); err != nil {
		slog.Warn("snapshot for the ransomware alert not held", "volume", volume, "snapshot", snap, "err", err)
		return ""
	}
	return name
}

// ---------- alarm ----------

func (s *Server) guardClient(ctx context.Context, pid int) protect.Client {
	ps := s.protect()
	ps.mu.Lock()
	fresh := ps.now().Sub(ps.sessionsAt) < guardSessionsTTL
	sessions := ps.sessions
	ps.mu.Unlock()
	if !fresh {
		if m, err := protect.SMBSessions(ctx, ps.runner); err == nil {
			sessions = m
			ps.mu.Lock()
			ps.sessions, ps.sessionsAt = m, ps.now()
			ps.mu.Unlock()
		}
	}
	if c, ok := sessions[pid]; ok {
		return c
	}
	return protect.Client{PID: pid, Process: protect.ProcessName(pid)}
}

func newAlertID(at time.Time) string {
	return at.UTC().Format("20060102-150405") + "-" + agent.NewID()[:6]
}

// guardStart acts on a new incident.
func (s *Server) guardStart(inc protect.Incident, pid int, g agentGuard) {
	ctx, cancel := context.WithTimeout(s.agentBase(), 2*time.Minute)
	defer cancel()
	ps := s.protect()
	sh, ok := s.shareByName(inc.Share)
	if !ok {
		return
	}
	a := guardAlert{ID: newAlertID(inc.First), Share: sh.Name, Volume: sh.Volume, Reason: inc.Reason, Mode: g.mode(), First: inc.First.UTC(), Last: inc.Last.UTC(), Count: inc.Count, Files: inc.Files, State: "active", Btrfs: s.isBtrfs(sh.Volume)}
	a.Client = s.guardClient(ctx, pid)
	if a.Btrfs {
		a.Snapshot, a.SnapshotAt = s.guardFindSnapshot(sh.Volume, inc.First)
	}
	if a.Mode != guardReport {
		if err := s.guardLockShare(ctx, &a); err != nil {
			a.Error = err.Error()
		}
		if a.Snapshot != "" {
			a.Held = s.guardHold(ctx, sh.Volume, a.Snapshot, inc.First)
		}
	}
	ps.mu.Lock()
	s.guardLoadLocked(ps)
	ps.alerts = append(ps.alerts, a)
	s.guardSaveLocked(ps)
	ps.mu.Unlock()
	slog.Error("ransomware guard alarm", "share", a.Share, "reason", a.Reason, "client", a.Client.Label(), "account", a.Client.User, "files", a.Count, "locked", a.Locked)
	s.opts.Activity.Record(activity.Entry{At: time.Now().UTC(), User: a.Client.User, Via: activity.ViaSystem, Action: "ransomware guard alarm", Target: a.Share, OK: true})
	s.guardTell(a)
}

// guardTell sends the alarm to owners and administrators.
func (s *Server) guardTell(a guardAlert) {
	if a.Locked && s.opts.Notify != nil {
		s.opts.Notify.Notify(notify.Error, "share_guard_locked", a.Share)
	}
	if s.opts.WebPush == nil {
		return
	}
	who := a.Client.Label()
	if a.Client.User != "" {
		who += ", Konto „" + a.Client.User + "“"
	}
	body := fmt.Sprintf("%d Dateien verdächtig verändert, von %s.", a.Count, who)
	if a.Locked {
		body += " Die Freigabe ist nur noch lesbar."
	}
	for _, name := range s.notifyAudience() {
		s.opts.WebPush.SendTo(name, webpush.Message{Title: "🛡️ Ransomware-Wächter: " + a.Share, Body: body, URL: "/agent?feature=guard", Tag: "guard-" + a.ID})
	}
}

func (s *Server) shareByName(name string) (shares.Share, bool) {
	if s.opts.Shares == nil {
		return shares.Share{}, false
	}
	for _, sh := range s.opts.Shares.List() {
		if sh.Name == name {
			return sh, true
		}
	}
	return shares.Share{}, false
}

func (s *Server) updateShare(sh shares.Share) error {
	var volumes []string
	if s.opts.Storage != nil {
		volumes = s.opts.Storage.Names()
	}
	if err := s.opts.Shares.Update(sh.Name, sh, volumes, s.userNames()); err != nil {
		return err
	}
	s.opts.Shares.Kick()
	return nil
}

// guardLockShare makes the share read-only for everyone and ends the
// connections of the writing device.
func (s *Server) guardLockShare(ctx context.Context, a *guardAlert) error {
	ps := s.protect()
	ps.accessMu.Lock()
	sh, ok := s.shareByName(a.Share)
	if !ok {
		ps.accessMu.Unlock()
		return shares.ErrNotFound
	}
	sh.Access = maps.Clone(sh.Access)
	saved := map[string]string{}
	for user, acc := range sh.Access {
		if acc == shares.ReadWrite {
			saved[user] = acc
			sh.Access[user] = shares.ReadOnly
		}
	}
	if sh.NFS.Mode == shares.ReadWrite || sh.NFS.Mode == "" && sh.NFS.Enabled {
		a.SavedNFS = sh.NFS.Mode
		if a.SavedNFS == "" {
			a.SavedNFS = "legacy"
		}
		sh.NFS.Mode, sh.NFS.Enabled = shares.ReadOnly, true
	}
	err := s.updateShare(sh)
	ps.accessMu.Unlock()
	if err != nil {
		return fmt.Errorf("share not locked: %w", err)
	}
	a.Locked, a.Saved = true, saved
	if err := protect.Disconnect(ctx, ps.runner, a.Share, a.Client.IP); err != nil {
		slog.Warn("connections of the attacking device not ended", "share", a.Share, "err", err)
	}
	return nil
}

// guardUnlockShare gives back the access the guard took; entries changed
// in between stay as they are.
func (s *Server) guardUnlockShare(a *guardAlert) error {
	if !a.Locked {
		return nil
	}
	ps := s.protect()
	ps.accessMu.Lock()
	defer ps.accessMu.Unlock()
	sh, ok := s.shareByName(a.Share)
	if !ok {
		a.Locked = false
		return nil
	}
	sh.Access = maps.Clone(sh.Access)
	for user, old := range a.Saved {
		if sh.Access[user] == shares.ReadOnly {
			sh.Access[user] = old
		}
	}
	if a.SavedNFS != "" && sh.NFS.Mode == shares.ReadOnly {
		if a.SavedNFS == "legacy" {
			sh.NFS.Mode = ""
		} else {
			sh.NFS.Mode = a.SavedNFS
		}
	}
	if err := s.updateShare(sh); err != nil {
		return err
	}
	if sh.GuardLocked != nil {
		_ = s.opts.Shares.UnlockGuard(sh.Name)
	}
	a.Locked = false
	return nil
}

// guardRefresh copies the files of running incidents into their alerts.
func (s *Server) guardRefresh(det *protect.Detector) {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s.guardLoadLocked(ps)
	changed := false
	for i := range ps.alerts {
		a := &ps.alerts[i]
		if a.State != "active" {
			continue
		}
		inc, ok := det.Snapshot(a.Share)
		if !ok || inc.Count == a.Count {
			continue
		}
		a.Count, a.Last, a.Files, a.More = inc.Count, inc.Last.UTC(), trimFiles(inc.Files), inc.More+max(0, len(inc.Files)-maxStoredFiles)
		changed = true
	}
	if changed {
		s.guardSaveLocked(ps)
	}
}

func trimFiles(f []protect.Affected) []protect.Affected {
	if len(f) > maxStoredFiles {
		return slices.Clone(f[:maxStoredFiles])
	}
	return f
}

// guardStopped closes an incident; in mode auto the files come back.
func (s *Server) guardStopped(inc protect.Incident, g agentGuard) {
	ps := s.protect()
	ps.mu.Lock()
	s.guardLoadLocked(ps)
	var auto []string
	for i := range ps.alerts {
		a := &ps.alerts[i]
		if a.Share == inc.Share && a.State == "active" {
			a.State, a.Count, a.Last, a.Files, a.More = "stopped", inc.Count, inc.Last.UTC(), trimFiles(inc.Files), inc.More+max(0, len(inc.Files)-maxStoredFiles)
			if a.Mode == guardAuto && (a.Snapshot != "" || a.Held != "") {
				auto = append(auto, a.ID)
			}
		}
	}
	s.guardSaveLocked(ps)
	ps.mu.Unlock()
	for _, id := range auto {
		if _, err := s.guardRestore(id, "auto"); err != nil {
			slog.Error("automatic restore after a ransomware alarm failed", "alert", id, "err", err)
		}
	}
}

// ---------- actions ----------

func (s *Server) guardAlert(id string) (guardAlert, bool) {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s.guardLoadLocked(ps)
	for _, a := range ps.alerts {
		if a.ID == id {
			return a, true
		}
	}
	return guardAlert{}, false
}

func (s *Server) guardPut(a guardAlert) {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for i := range ps.alerts {
		if ps.alerts[i].ID == a.ID {
			ps.alerts[i] = a
		}
	}
	s.guardSaveLocked(ps)
}

var errGuardActive = errors.New("the attack is still going on; wait until it stopped")

// guardRestore puts the files back from the snapshot. The share stays
// read-only (the device may still be infected); reopening is a separate
// step.
func (s *Server) guardRestore(id, by string) (guardAlert, error) {
	a, ok := s.guardAlert(id)
	if !ok {
		return a, agent.ErrNotFound
	}
	if a.State == "active" {
		return a, errGuardActive
	}
	snap := a.Held
	if snap == "" {
		snap = a.Snapshot
	}
	if snap == "" {
		return a, protect.ErrNoSnapshot
	}
	sh, ok := s.shareByName(a.Share)
	if !ok {
		return a, shares.ErrNotFound
	}
	shareDir := s.opts.Shares.Path(sh)
	rel, err := filepath.Rel(s.opts.Storage.MountPoint(sh.Volume), shareDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return a, fmt.Errorf("share outside its volume")
	}
	snapDir := filepath.Join(s.snapshotPath(sh.Volume, snap), rel)
	if _, err := os.Stat(snapDir); err != nil {
		return a, fmt.Errorf("the snapshot has no folder of the share: %w", err)
	}
	ps := s.protect()
	ps.mu.Lock()
	det := ps.detector
	ps.mu.Unlock()
	// Our own writes are no attack.
	if det != nil {
		det.Mute(a.Share, "", ps.now().Add(time.Hour))
	}
	res := protect.Restore(shareDir, snapDir, a.Files, time.Now().UTC().Format("20060102-150405"))
	if det != nil {
		det.Mute(a.Share, "", ps.now().Add(10*time.Second))
	}
	a.Restored, a.State, a.By = &res, "restored", by
	a.Closed = time.Now().UTC()
	s.guardPut(a)
	slog.Info("files restored after a ransomware alarm", "share", a.Share, "snapshot", snap, "restored", res.Restored, "recycled", res.Recycled, "failed", len(res.Failed), "by", by)
	return a, nil
}

func (s *Server) handleGuard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.guardStatusFor(maxGuardAlerts))
}

// guardStatusFor is the state for the page and the agent's tool.
func (s *Server) guardStatusFor(limit int) map[string]any {
	g := s.agentSettings().Features.Guard
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s.guardLoadLocked(ps)
	list := slices.Clone(ps.alerts)
	slices.Reverse(list)
	if len(list) > limit {
		list = list[:limit]
	}
	for i := range list {
		if len(list[i].Files) > 500 {
			list[i].More += len(list[i].Files) - 500
			list[i].Files = list[i].Files[:500]
		}
	}
	return map[string]any{
		"enabled": g.Enabled, "mode": g.mode(), "watching": ps.watchCancel != nil && ps.watchErr == "", "error": ps.watchErr,
		"overflows": ps.overflows, "alerts": list,
	}
}

func (s *Server) guardAction(w http.ResponseWriter, r *http.Request, f func(*guardAlert) error) {
	id := r.PathValue("id")
	noteTarget(r, id)
	a, ok := s.guardAlert(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such alert")
		return
	}
	if err := f(&a); err != nil {
		writeError(w, http.StatusConflict, "guard_action", err.Error())
		return
	}
	a.By = currentUser(r).Name
	if a.Closed.IsZero() {
		a.Closed = time.Now().UTC()
	}
	s.guardPut(a)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleGuardRestore(w http.ResponseWriter, r *http.Request) {
	noteTarget(r, r.PathValue("id"))
	a, err := s.guardRestore(r.PathValue("id"), currentUser(r).Name)
	switch {
	case errors.Is(err, agent.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such alert")
	case err != nil:
		writeError(w, http.StatusConflict, "guard_restore", err.Error())
	default:
		writeJSON(w, http.StatusOK, a)
	}
}

func (s *Server) handleGuardReopen(w http.ResponseWriter, r *http.Request) {
	s.guardAction(w, r, func(a *guardAlert) error {
		if a.State == "active" {
			return errGuardActive
		}
		if err := s.guardUnlockShare(a); err != nil {
			return err
		}
		if a.State == "stopped" {
			a.State = "reopened"
		}
		return nil
	})
}

// handleGuardDismiss: it was wanted (e.g. an own encryption): reopen,
// restore nothing, and do not alarm on that device and share for an hour.
func (s *Server) handleGuardDismiss(w http.ResponseWriter, r *http.Request) {
	s.guardAction(w, r, func(a *guardAlert) error {
		if err := s.guardUnlockShare(a); err != nil {
			return err
		}
		ps := s.protect()
		ps.mu.Lock()
		det := ps.detector
		ps.mu.Unlock()
		if det != nil {
			det.Forget(a.Share)
			det.Mute(a.Share, "pid:"+strconv.Itoa(a.Client.PID), ps.now().Add(guardFalseMute))
		}
		a.State = "dismissed"
		return nil
	})
}
