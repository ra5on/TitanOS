package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const personalFeature = "personal-agent"

// A plan and consent belong to exactly one job. The submitted request stays immutable for
// idempotency. A prior step contributes plain result text, never tool names, arguments or URLs.
type personalPlan struct {
	Origin      string                     `json:"origin,omitempty"`
	Effect      string                     `json:"effect,omitempty"`
	SkillID     string                     `json:"skillId,omitempty"`
	StepArgs    map[string]json.RawMessage `json:"stepArgs,omitempty"`
	GrantStamp  string                     `json:"grantStamp,omitempty"`
	Expected    string                     `json:"expected,omitempty"`
	CancelAfter bool                       `json:"cancelAfter,omitempty"`
	Claim       string                     `json:"claim,omitempty"`
	PauseAfter  bool                       `json:"pauseAfter,omitempty"`
	Steps       []string                   `json:"steps"`
	Index       int                        `json:"index"`
	Prior       string                     `json:"prior,omitempty"`
	Ask         bool                       `json:"ask,omitempty"`
	ReadOK      bool                       `json:"readOK,omitempty"`
	Approval    string                     `json:"approval,omitempty"`
}
type personalAccount struct {
	Effects     map[string]string            `json:"effects,omitempty"`
	Watches     map[string]personalMailWatch `json:"watches"`
	Attachments []personalAttachment         `json:"attachments,omitempty"`
	Chats       []pa.Chat                    `json:"chats"`
	Jobs        []pa.Job                     `json:"jobs"`
	Plans       map[string]*personalPlan     `json:"plans"`
	Limits      map[string]pa.Level          `json:"limits"`
	Settings    pa.Settings                  `json:"settings"`
	Skills      []pa.Skill                   `json:"skills"`
	Memory      []string                     `json:"memory"`
}

func newPersonalAccount() personalAccount {
	return personalAccount{Effects: map[string]string{}, Watches: map[string]personalMailWatch{}, Chats: []pa.Chat{}, Jobs: []pa.Job{}, Plans: map[string]*personalPlan{}, Limits: map[string]pa.Level{}, Skills: []pa.Skill{}, Memory: []string{},
		Settings: pa.Settings{Notifications: pa.NoticePrefs{Result: true, Blocker: true, Approval: true}, Rights: map[string]pa.Level{}, Colors: map[string]string{}, Theme: "system", Motion: "full", DotSize: 10}}
}

type personalRuntime struct {
	replyVoices map[*personalReplyRun]struct{} // guarded by mu; live HTTP playback requests

	watch      sync.Mutex
	watchDue   map[string]time.Time
	processing map[string]bool
	mu         sync.Mutex // serializes atomic account-file transactions; never held across a tool call
	tick       sync.Mutex // one claim per scheduler transaction
	active     map[string]context.CancelFunc
	tools      map[string]personalTool
	known      map[string]time.Time // next possible job, or zero after scanning an idle account
	leases     map[string]personalLease
	cursor     int
}
type personalLease struct {
	Owner   string
	Created time.Time
	Job     string
	Until   time.Time
}

func personalID() string { var id [16]byte; _, _ = rand.Read(id[:]); return hex.EncodeToString(id[:]) }
func personalKey(u auth.User) string {
	// Recreating a NAS account with the same login must not give access to its predecessor.
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d", u.Name, u.UID, u.Created.UnixNano())))
	return hex.EncodeToString(digest[:])
}
func (s *Server) personalRoot() (*os.Root, func(), error) {
	if dir := s.opts.PersonalDataDir; dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, nil, err
		}
		root, err := os.OpenRoot(dir)
		return root, func() {}, err
	}
	if s.opts.Storage == nil || s.opts.SystemData == nil {
		return nil, nil, volumedata.ErrUnavailable
	}
	v, release := s.opts.SystemData.Use(personalFeature)
	if v == "" {
		release()
		return nil, nil, volumedata.ErrUnavailable
	}
	root, err := volumedata.Open(filepath.Dir(s.opts.Storage.MountPoint(v)), v, personalFeature, s.opts.Storage.Mounted())
	if err != nil {
		release()
		return nil, nil, err
	}
	return root, release, nil
}

var errPersonalMissing = errors.New("personal agent item not found")
var errPersonalInvalid = errors.New("invalid personal agent request")
var errPersonalDenied = errors.New("personal agent permission denied")
var errPersonalConflict = errors.New("personal agent state changed")
var errPersonalFull = errors.New("personal agent account storage limit reached")

func (s *Server) personalTxn(u auth.User, write bool, fn func(*personalAccount) error) error {
	s.personal.mu.Lock()
	defer s.personal.mu.Unlock()
	root, release, err := s.personalRoot()
	if err != nil {
		return err
	}
	defer release()
	defer root.Close()
	return s.personalAccountTx(root, u, write, fn)
}

const personalAccountMax = 64 << 20

func (s *Server) personalAccountTx(root *os.Root, u auth.User, write bool, fn func(*personalAccount) error) error {
	now, ok := s.opts.Users.Get(u.Name)
	if !ok || now.Disabled || !now.Created.Equal(u.Created) || now.UID != u.UID {
		return errPersonalDenied
	}
	key := personalKey(u)
	name := key + ".json"
	a := newPersonalAccount()
	err := volumedata.ReadJSON(root, name, &a, personalAccountMax)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fn(&a); err != nil {
		return err
	}
	if write {
		contents, err := json.Marshal(&a)
		if err != nil {
			return err
		}
		if len(contents)+1 > personalAccountMax {
			return errPersonalFull
		}
		if err := volumedata.Write(root, name, append(contents, '\n')); err != nil {
			return err
		}
	}
	s.personal.known[key] = a.nextDue()
	s.personal.watchDue[key] = a.nextWatch()
	return nil
}
func (a *personalAccount) nextDue() time.Time {
	if a.Settings.Paused {
		return time.Time{}
	}
	var next time.Time
	for _, j := range a.Jobs {
		if j.Status != "planned" && j.Status != "running" {
			continue
		}
		due := j.Request.RunAt
		if due.IsZero() {
			due = time.Unix(1, 0)
		}
		if next.IsZero() || due.Before(next) {
			next = due
		}
	}
	return next
}
func (a *personalAccount) find(id string) (*pa.Job, error) {
	for i := range a.Jobs {
		if a.Jobs[i].ID == id {
			return &a.Jobs[i], nil
		}
	}
	return nil, errPersonalMissing
}
