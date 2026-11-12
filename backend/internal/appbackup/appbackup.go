// Package appbackup coordinates cold application backups and bounded PostgreSQL exports.
package appbackup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// pointerFile is the pointer to an interrupted application backup on the
// stick. Up to 0.76 it was written at the start of every run and removed
// at its end: two stick writes per backup (PLAN 79-12). A run now keeps
// its pointer next to its journal on the volume (volumePointer) and the
// stick is written only when an interrupted run is found after a restart,
// so that an older titand (after a rollback) still sees it and fences
// application changes. A pointer an older titand left is read as before.
const pointerFile = "backup/application-pending.json"

// volumePointer names, in .titanos/appbackups of the journal's volume, the
// run that is underway: {id, volume, job}. It is written before anything
// is stopped and removed when everything runs again.
const volumePointer = "pending.json"

var operationID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// ErrJournalMissing means the pointer on the stick names an operation whose
// journal is not on its (mounted) volume, for example after a stick backup
// taken during the run was restored. Nothing is known that could be
// restarted; the pointer can only be forgotten.
var ErrJournalMissing = errors.New("the journal of this application backup is not on its volume")

type Member struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Identity string `json:"identity"`
	Running  bool   `json:"running"`
	Method   string `json:"method"`
	Phase    string `json:"phase"`
	Error    string `json:"error"`
}
type Operation struct {
	ID      string    `json:"id"`
	Volume  string    `json:"volume"`
	Job     string    `json:"job"`
	Created time.Time `json:"created"`
	Members []Member  `json:"members"`
}
type Manager struct {
	Store     *config.Store
	Docker    *docker.Manager
	VMs       *vm.Manager
	Mounted   func() []string
	MountRoot string
	Disabled  bool
	Gate      sync.RWMutex
	// Settled tells that the volumes were read and a first attempt to
	// mount them was made (nil: at once). Until then, and until the mounted
	// volumes were looked through once, Busy fences application changes.
	Settled func() bool
	mu      sync.Mutex
	pending *Operation
	active  bool
	loaded  bool
	// onStick: pointerFile exists (read at the start or written since).
	onStick bool
	// looked are the mounted volumes whose pointer was looked for since
	// they were mounted; scanned that this happened at least once.
	looked  map[string]bool
	scanned bool
	// inventory replaces prepareDocker in tests.
	inventory func(ctx context.Context, applications []backup.Application, protected []string) (*docker.BackupLease, []Member, []string, error)
}

func validPointer(op *Operation) bool {
	return op != nil && operationID.MatchString(op.ID) && naming.ValidVolume(op.Volume) && naming.ValidBackup(op.Job)
}

func (m *Manager) Load() error {
	var op *Operation
	if err := m.Store.ReadJSON(pointerFile, &op); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if op != nil && !validPointer(op) {
		return errors.New("invalid application recovery pointer")
	}
	m.mu.Lock()
	m.pending = op
	m.onStick = op != nil
	m.loaded = true
	// Without volumes to look at (tests, development mode) nothing more
	// can turn up.
	m.scanned = m.scanned || m.Mounted == nil || m.Disabled
	m.mu.Unlock()
	return nil
}

// Run looks for the pointer of an interrupted run on every volume once
// after it was mounted. The mounted volumes come from RAM; a volume is read
// once (one small file), never cyclically, and never the stick.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		m.Discover()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Discover reads the pointer of volumes mounted since the last look. A run
// that was cut off (titand or the NAS went down) is taken up as pending,
// and only then noted on the stick for older versions.
func (m *Manager) Discover() {
	// A run or a recovery owns the pointer right now.
	if !m.Gate.TryRLock() {
		return
	}
	defer m.Gate.RUnlock()
	m.discover()
}

// discover is Discover for a caller that holds Gate.
func (m *Manager) discover() {
	if m.Mounted == nil || m.Disabled {
		m.mu.Lock()
		m.scanned = true
		m.mu.Unlock()
		return
	}
	if m.Settled != nil && !m.Settled() {
		return
	}
	mounted := m.Mounted()
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return
	}
	for volume := range m.looked {
		if !slices.Contains(mounted, volume) {
			delete(m.looked, volume) // mounted again later: looked at again
		}
	}
	var look []string
	if m.pending == nil && !m.active {
		for _, volume := range mounted {
			if !m.looked[volume] {
				look = append(look, volume)
			}
		}
	}
	m.mu.Unlock()
	for _, volume := range look {
		op, err := readVolumePointer(m.MountRoot, volume, mounted)
		if err != nil {
			slog.Warn("pointer of an application backup not readable; looked at again after the next start", "volume", volume, "err", err)
		}
		m.mu.Lock()
		if m.looked == nil {
			m.looked = map[string]bool{}
		}
		m.looked[volume] = true
		found := op != nil && m.pending == nil
		if found {
			m.pending = op
		}
		m.mu.Unlock()
		if !found {
			continue
		}
		slog.Warn("an application backup was cut off; review it under Tools › Jobs", "volume", op.Volume, "job", op.Job)
		// An event, not a cycle: an older titand reads only the stick.
		if err := m.Store.WriteJSON(pointerFile, op); err != nil {
			slog.Warn("interrupted application backup not noted on the stick", "err", err)
		} else {
			m.mu.Lock()
			m.onStick = true
			m.mu.Unlock()
		}
		break
	}
	m.mu.Lock()
	m.scanned = true
	m.mu.Unlock()
}

// readVolumePointer reads volumePointer of a volume without creating
// anything there. nil without one.
func readVolumePointer(mountRoot, volume string, mounted []string) (*Operation, error) {
	root, err := volumedata.OpenExisting(mountRoot, volume, "appbackups", mounted)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer root.Close()
	var op *Operation
	if err := volumedata.ReadJSON(root, volumePointer, &op, 1<<16); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !validPointer(op) || op.Volume != volume {
		return nil, errors.New("invalid application recovery pointer")
	}
	return &Operation{ID: op.ID, Volume: op.Volume, Job: op.Job}, nil
}
func (m *Manager) Status() (*Operation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return nil, m.active
	}
	out := *m.pending
	out.Members = slices.Clone(out.Members)
	return &out, m.active
}

// Busy also fences interrupted work and unreadable recovery pointers. A service
// restart must not silently permit application changes or an app downgrade.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.loaded || !m.scanned || m.pending != nil || m.active
}
func (m *Manager) save(root *os.Root, op *Operation) error {
	if err := volumedata.WriteJSON(root, op.ID+"/operation.json", op); err != nil {
		return err
	}
	m.mu.Lock()
	copy := *op
	copy.Members = slices.Clone(op.Members)
	m.pending = &copy
	m.mu.Unlock()
	return nil
}
func (m *Manager) clear(root *os.Root, op *Operation) error {
	if err := m.forget(root); err != nil {
		return err
	}
	return root.RemoveAll(op.ID)
}

// forget removes the pointer: the one on the volume and, only if there is
// one, the one on the stick. A run that was never cut off does not touch
// the stick at all.
func (m *Manager) forget(root *os.Root) error {
	if err := root.Remove(volumePointer); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	onStick := m.onStick
	m.mu.Unlock()
	if onStick {
		if err := m.Store.WriteJSON(pointerFile, nil); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.pending, m.onStick = nil, false
	m.mu.Unlock()
	return nil
}
func (m *Manager) Prepare(ctx context.Context, job backup.Job, volume string, rawPaths []string) (paths []string, finish func() error, result error) {
	if m.Disabled {
		return nil, nil, backup.ErrDisabled
	}
	if !m.Gate.TryLock() {
		return nil, nil, errors.New("application changes are in progress")
	}
	releaseGate := true
	defer func() {
		if releaseGate {
			m.Gate.Unlock()
		}
	}()
	// A run cut off before the last start must be found first: the volumes
	// mounted by now are looked at here, not only by the next round of Run.
	m.discover()
	m.mu.Lock()
	busy := !m.loaded || !m.scanned || m.pending != nil || m.active
	m.mu.Unlock()
	if busy {
		return nil, nil, errors.New("an application backup needs recovery")
	}
	root, err := volumedata.Open(m.MountRoot, volume, "appbackups", m.Mounted())
	if err != nil {
		return nil, nil, err
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		root.Close()
		return nil, nil, err
	}
	op := Operation{ID: hex.EncodeToString(id[:]), Volume: volume, Job: job.ID, Created: time.Now().UTC(), Members: []Member{}}
	leases := map[string]*vm.BackupLease{}
	var dl *docker.BackupLease
	closeLeases := func() {
		if dl != nil {
			dl.Close()
			dl = nil
		}
		for _, l := range leases {
			l.Close()
		}
	}
	fail := func(err error) ([]string, func() error, error) { closeLeases(); root.Close(); return nil, nil, err }
	for _, a := range job.Applications {
		if a.Kind != "vm" {
			continue
		}
		l, err := m.VMs.BackupLease(a.Name, m.Mounted())
		if err != nil {
			return fail(err)
		}
		leases[a.Name] = l
		op.Members = append(op.Members, Member{Kind: a.Kind, Name: a.Name, Identity: l.Identity(), Running: l.Running, Method: a.Method, Phase: "planned"})
		paths = append(paths, l.Paths()...)
	}
	var dockerMembers []Member
	var dockerPaths []string
	inventory := m.prepareDocker
	if m.inventory != nil {
		inventory = m.inventory
	}
	dl, dockerMembers, dockerPaths, err = inventory(ctx, job.Applications, append(slices.Clone(rawPaths), paths...))
	if err != nil {
		return fail(err)
	}
	op.Members = append(op.Members, dockerMembers...)
	paths = append(paths, dockerPaths...)
	if err := m.checkVMWriters(leases, append(slices.Clone(rawPaths), paths...)); err != nil {
		return fail(err)
	}
	if err := m.validateApplicationPaths(job, paths); err != nil {
		return fail(err)
	}
	if err := root.Mkdir(op.ID, 0700); err != nil {
		return fail(err)
	}
	if err := m.save(root, &op); err != nil {
		return fail(err)
	}
	// The pointer lies next to the journal, not on the stick (PLAN 79-12).
	if err := volumedata.WriteJSON(root, volumePointer, Operation{ID: op.ID, Volume: volume, Job: job.ID}); err != nil {
		// Nothing was stopped yet and nothing points to this operation:
		// it is no pending one. Left set, "a backup is running" would
		// hold every change of an app until titand restarts.
		m.mu.Lock()
		m.pending = nil
		m.mu.Unlock()
		_ = root.RemoveAll(op.ID) // cleanup, best effort: an empty journal folder harms nothing
		return fail(err)
	}
	m.mu.Lock()
	m.active = true
	m.mu.Unlock()
	releaseGate = false
	finish = func() (finalErr error) {
		defer root.Close()
		defer m.Gate.Unlock()
		defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock() }()
		// Each restart is attempted independently, even if a previous one failed.
		for i := range op.Members {
			member := &op.Members[i]
			if !member.Running || member.Method != "stop" {
				continue
			}
			restartCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			var err error
			if member.Kind == "docker" {
				items, e := dl.Containers(restartCtx)
				if e != nil {
					err = e
				} else if !slices.ContainsFunc(items, func(c docker.Container) bool { return c.ID == member.Identity && c.State == "running" }) {
					err = dl.Start(restartCtx, member.Identity)
				}
			} else {
				if dl != nil {
					dl.Close()
					dl = nil
				}
				err = leases[member.Name].Restart(restartCtx, m.Mounted())
			}
			cancel()
			member.Phase = "resumed"
			member.Error = ""
			if err != nil {
				member.Phase = "restart-failed"
				member.Error = err.Error()
				finalErr = errors.Join(finalErr, fmt.Errorf("restart %s: %w", member.Name, err))
			}
			if err := m.save(root, &op); err != nil {
				finalErr = errors.Join(finalErr, err)
			}
		}
		closeLeases()
		if finalErr == nil {
			finalErr = m.clear(root, &op)
		}
		return finalErr
	}
	// Docker restarts must precede VM restarts (GPU handoff can take Docker's lock).
	slices.SortStableFunc(op.Members, func(a, b Member) int {
		if a.Kind == b.Kind {
			return 0
		}
		if a.Kind == "docker" {
			return -1
		}
		return 1
	})
	for i := range op.Members {
		member := &op.Members[i]
		if member.Method == "stop" && member.Running {
			member.Phase = "stopping"
			if err := m.save(root, &op); err != nil {
				return nil, nil, errors.Join(err, finish())
			}
			if member.Kind == "docker" {
				err = dl.Stop(ctx, member.Identity)
			} else {
				err = leases[member.Name].Stop(ctx)
			}
			if err != nil {
				return nil, nil, errors.Join(err, finish())
			}
			member.Phase = "stopped"
			if err := m.save(root, &op); err != nil {
				return nil, nil, errors.Join(err, finish())
			}
		}
		if member.Method == "postgres-export" {
			dump := filepath.Join(m.MountRoot, volume, ".titanos", "appbackups", op.ID, member.Identity+".sql")
			user, err := dl.PostgresUser(ctx, member.Identity)
			if err != nil {
				return nil, nil, errors.Join(err, finish())
			}
			if err := dumpPostgres(ctx, member.Identity, user, dump); err != nil {
				return nil, nil, errors.Join(err, finish())
			}
			paths = append(paths, dump)
			member.Phase = "exported"
			if err := m.save(root, &op); err != nil {
				return nil, nil, errors.Join(err, finish())
			}
		}
	}
	// Include the captured application identities and pre-backup states in new
	// archives so a restored copy can be reviewed independently of live settings.
	paths = append(paths, filepath.Join(m.MountRoot, volume, ".titanos", "appbackups", op.ID, "operation.json"))
	return paths, finish, nil
}
func within(p, parent string) bool {
	return p == parent || strings.HasPrefix(p, parent+string(filepath.Separator))
}

type boundedWriter struct {
	writer io.Writer
	left   int64
	cancel context.CancelFunc
}

func (w *boundedWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.left {
		w.cancel()
		return 0, errors.New("PostgreSQL export exceeds 8 GiB")
	}
	n, err := w.writer.Write(b)
	w.left -= int64(n)
	return n, err
}

// postgresDumpArgs runs pg_dumpall as the OS user postgres over the local
// socket, connecting as the image's superuser role. No shell and no password
// argument; the role is one argv element.
func postgresDumpArgs(id, user string) ([]string, error) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(id) {
		return nil, errors.New("invalid container identity")
	}
	if user == "" || len(user) > 63 || strings.ContainsFunc(user, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, errors.New("invalid PostgreSQL user")
	}
	return []string{"exec", "--user", "postgres", id, "pg_dumpall", "--no-password", "--username=" + user}, nil
}

func dumpPostgres(ctx context.Context, id, user, target string) error {
	args, err := postgresDumpArgs(id, user)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = &boundedWriter{file, 8 << 30, cancel}
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("PostgreSQL export failed: %w", err)
	}
	return file.Sync()
}

// Review reads the execution journal only on an explicit authorized request.
func (m *Manager) Review() (*Operation, bool, error) {
	op, active := m.Status()
	if op == nil || active {
		return op, active, nil
	}
	if m.Mounted == nil {
		return op, active, volumedata.ErrUnavailable
	}
	root, err := volumedata.Open(m.MountRoot, op.Volume, "appbackups", m.Mounted())
	if err != nil {
		return op, active, err
	}
	defer root.Close()
	var saved Operation
	if err := volumedata.ReadJSON(root, op.ID+"/operation.json", &saved, 1<<20); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return op, active, ErrJournalMissing
		}
		return op, active, err
	}
	if saved.ID != op.ID || saved.Volume != op.Volume || saved.Job != op.Job {
		return op, active, errors.New("application recovery identity mismatch")
	}
	return &saved, active, nil
}

func (m *Manager) Recover(ctx context.Context, confirm string, restart bool) error {
	if m.Disabled {
		return backup.ErrDisabled
	}
	if !m.Gate.TryLock() {
		return errors.New("application work is running")
	}
	defer m.Gate.Unlock()
	p, active := m.Status()
	if p == nil || active || p.ID != confirm {
		return errors.New("repeat the recovery operation ID")
	}
	root, err := volumedata.Open(m.MountRoot, p.Volume, "appbackups", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	var op Operation
	if err := volumedata.ReadJSON(root, p.ID+"/operation.json", &op, 1<<20); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if restart {
			return ErrJournalMissing
		}
		// Without a journal there is nothing to restart or remove: only the
		// pointer is forgotten, so that application changes work again.
		if err := m.forget(root); err != nil {
			return err
		}
		m.rediscover()
		return nil
	}
	if op.ID != p.ID || op.Volume != p.Volume {
		return errors.New("application recovery identity mismatch")
	}
	if restart {
		dl, err := m.Docker.BackupLease(ctx)
		if err != nil {
			return err
		}
		defer dl.Close()
		containers, err := dl.Containers(ctx)
		if err != nil {
			return err
		}
		var failures error
		for _, member := range op.Members {
			if !member.Running || member.Method != "stop" {
				continue
			}
			if member.Kind == "docker" {
				i := slices.IndexFunc(containers, func(c docker.Container) bool { return c.ID == member.Identity && c.Name == member.Name })
				if i < 0 {
					failures = errors.Join(failures, errors.New("original container identity no longer exists"))
					continue
				}
				if containers[i].State != "running" {
					failures = errors.Join(failures, dl.Start(ctx, member.Identity))
				}
			}
		}
		dl.Close()
		for _, member := range op.Members {
			if member.Kind == "vm" && member.Running && member.Method == "stop" {
				if m.VMs.BackupIdentity(member.Name) != member.Identity {
					failures = errors.Join(failures, errors.New("original VM identity no longer exists"))
					continue
				}
				if !m.VMs.RunningNames()[member.Name] {
					failures = errors.Join(failures, m.VMs.Start(ctx, member.Name, m.Mounted()))
				}
			}
		}
		if failures != nil {
			return failures
		}
	}
	if err := m.clear(root, &op); err != nil {
		return err
	}
	m.rediscover()
	return nil
}

// rediscover looks through every mounted volume again after a recovery
// (caller holds Gate): another volume may hold a pointer that was passed
// over while this one was pending.
func (m *Manager) rediscover() {
	m.mu.Lock()
	m.looked = nil
	m.mu.Unlock()
	m.discover()
}
