package protect

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// The emergency access (PLAN 54-S): an owner registers persons (accounts
// of the NAS) with a waiting time and the shares they may read. A person
// asks with a reason; the owners are told at once and may decline. Without
// an answer the access is granted when the waiting time is over: read-only
// on the chosen shares and the handbook, for GrantFor, then it ends by
// itself. The state lives on the data volume (Store), so a restart neither
// forgets a deadline nor grants early.

// GrantFor is how long a granted emergency access lasts.
const GrantFor = 30 * 24 * time.Hour

// Request states.
const (
	StateWaiting  = "waiting"
	StateDeclined = "declined"
	StateGranted  = "granted"
	StateEnded    = "ended"
	StateCanceled = "canceled"
)

// Person is someone registered for the emergency access.
type Person struct {
	Account  string   `json:"account"`
	WaitDays int      `json:"waitDays"`
	Shares   []string `json:"shares"`
}

// Request is one ask for the emergency access and what became of it.
type Request struct {
	ID      string    `json:"id"`
	Account string    `json:"account"`
	Reason  string    `json:"reason"`
	Created time.Time `json:"created"`
	Due     time.Time `json:"due"`
	State   string    `json:"state"`
	Shares  []string  `json:"shares"`
	// By is who decided ("" when the waiting time decided).
	By      string    `json:"by,omitempty"`
	Decided time.Time `json:"decided,omitzero"`
	Ends    time.Time `json:"ends,omitzero"`
	// Added are the shares the grant gave read access; only these are
	// taken back when it ends.
	Added []string `json:"added,omitempty"`
	// Code is set when the person used a printed one-time code.
	Code bool `json:"code,omitempty"`
}

// Open reports whether a request still counts (waiting or granted).
func (r Request) Open() bool { return r.State == StateWaiting || r.State == StateGranted }

// EmergencyState is what is kept on the volume.
type EmergencyState struct {
	Requests []Request `json:"requests"`
	// Codes are the SHA-256 of printed one-time codes per account.
	Codes map[string]string `json:"codes,omitempty"`
	// CodeFails counts wrong codes per account; from MaxCodeTries on, codes
	// are refused until Until (doubling, at most a day).
	CodeFails map[string]CodeFail `json:"codeFails,omitempty"`
}

// CodeFail is the count of wrong codes of an account.
type CodeFail struct {
	N     int       `json:"n"`
	Until time.Time `json:"until,omitzero"`
}

// MaxCodeTries wrong codes are allowed before codes are refused for a while.
const MaxCodeTries = 5

// codeLock is how long codes are refused after n wrong ones.
func codeLock(n int) time.Duration {
	if n < MaxCodeTries {
		return 0
	}
	d := 15 * time.Minute << min(n-MaxCodeTries, 7)
	return min(d, 24*time.Hour)
}

// Store keeps a value on the data volume.
type Store interface {
	Load(v any) error // ErrNotFound-like errors are reported by NotFound
	Save(v any) error
}

// Errors of the emergency access.
var (
	ErrNotRegistered = errors.New("this account is not registered for the emergency access")
	ErrOpenRequest   = errors.New("there is already an open request")
	ErrNoRequest     = errors.New("no such request")
	ErrDecided       = errors.New("the request was already decided")
	ErrBadCode       = errors.New("the code is not valid")
	ErrCodeLocked    = errors.New("too many wrong codes; try again later")
)

// Emergency is the emergency access with its state.
type Emergency struct {
	mu       sync.Mutex
	store    Store
	notFound func(error) bool
	state    EmergencyState
	loaded   bool
}

// NewEmergency reads nothing yet; the state is loaded on first use.
func NewEmergency(store Store, notFound func(error) bool) *Emergency {
	return &Emergency{store: store, notFound: notFound}
}

func (e *Emergency) loadLocked() error {
	if e.loaded {
		return nil
	}
	var st EmergencyState
	if err := e.store.Load(&st); err != nil && !e.notFound(err) {
		return err
	}
	e.state, e.loaded = st, true
	return nil
}

func (e *Emergency) saveLocked(next EmergencyState) error {
	if err := e.store.Save(next); err != nil {
		return err
	}
	e.state = next
	return nil
}

func (e *Emergency) clone() EmergencyState {
	st := EmergencyState{Requests: slices.Clone(e.state.Requests)}
	for i := range st.Requests {
		st.Requests[i].Added = slices.Clone(st.Requests[i].Added)
	}
	if e.state.Codes != nil {
		st.Codes = maps.Clone(e.state.Codes)
	}
	if e.state.CodeFails != nil {
		st.CodeFails = maps.Clone(e.state.CodeFails)
	}
	return st
}

// State returns a copy of the state.
func (e *Emergency) State() (EmergencyState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return EmergencyState{}, err
	}
	return e.clone(), nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Ask files a request of a registered person. With a valid printed code
// (when codes are on) it is granted at once; the code is used up.
func (e *Emergency) Ask(p *Person, account, reason, code string, now time.Time) (Request, error) {
	if p == nil || p.Account != account {
		return Request{}, ErrNotRegistered
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return Request{}, err
	}
	if slices.ContainsFunc(e.state.Requests, func(r Request) bool { return r.Account == account && r.Open() }) {
		return Request{}, ErrOpenRequest
	}
	wait := p.WaitDays
	if wait <= 0 {
		wait = 3
	}
	reason = strings.TrimSpace(reason)
	if r := []rune(reason); len(r) > 500 {
		reason = string(r[:500])
	}
	now = now.UTC()
	req := Request{ID: newID(), Account: account, Reason: reason, Created: now, Due: now.Add(time.Duration(wait) * 24 * time.Hour), State: StateWaiting, Shares: slices.Clone(p.Shares)}
	next := e.clone()
	if code != "" {
		fail := next.CodeFails[account]
		if now.Before(fail.Until) {
			return Request{}, ErrCodeLocked
		}
		want, ok := next.Codes[account]
		if !ok || subtle.ConstantTimeCompare([]byte(want), []byte(hashCode(code))) != 1 {
			fail.N++
			if d := codeLock(fail.N); d > 0 {
				fail.Until = now.Add(d)
			}
			if next.CodeFails == nil {
				next.CodeFails = map[string]CodeFail{}
			}
			next.CodeFails[account] = fail
			if err := e.saveLocked(next); err != nil {
				return Request{}, err
			}
			return Request{}, ErrBadCode
		}
		delete(next.Codes, account)
		delete(next.CodeFails, account)
		req.Code, req.Due = true, now
	}
	next.Requests = append(next.Requests, req)
	next.Requests = pruneRequests(next.Requests, 100)
	if err := e.saveLocked(next); err != nil {
		return Request{}, err
	}
	return req, nil
}

func (e *Emergency) change(id string, f func(*Request) error) (Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return Request{}, err
	}
	next := e.clone()
	i := slices.IndexFunc(next.Requests, func(r Request) bool { return r.ID == id })
	if i < 0 {
		return Request{}, ErrNoRequest
	}
	if err := f(&next.Requests[i]); err != nil {
		return Request{}, err
	}
	if err := e.saveLocked(next); err != nil {
		return Request{}, err
	}
	return next.Requests[i], nil
}

// Decline ends a waiting request.
func (e *Emergency) Decline(id, by string, now time.Time) (Request, error) {
	return e.change(id, func(r *Request) error {
		if r.State != StateWaiting {
			return ErrDecided
		}
		r.State, r.By, r.Decided = StateDeclined, by, now.UTC()
		return nil
	})
}

// Cancel withdraws a waiting request (the person, or a removed person).
func (e *Emergency) Cancel(id, by string, now time.Time) (Request, error) {
	return e.change(id, func(r *Request) error {
		if r.State != StateWaiting {
			return ErrDecided
		}
		r.State, r.By, r.Decided = StateCanceled, by, now.UTC()
		return nil
	})
}

// Grant gives the access of a waiting request (by "" for the waiting time).
// added are the shares the caller gave read access.
func (e *Emergency) Grant(id, by string, added []string, now time.Time) (Request, error) {
	return e.change(id, func(r *Request) error {
		if r.State != StateWaiting {
			return ErrDecided
		}
		now = now.UTC()
		r.State, r.By, r.Decided, r.Ends, r.Added = StateGranted, by, now, now.Add(GrantFor), slices.Clone(added)
		return nil
	})
}

// End finishes a granted access (expired, or ended by an owner).
func (e *Emergency) End(id, by string, now time.Time) (Request, error) {
	return e.change(id, func(r *Request) error {
		if r.State != StateGranted {
			return ErrDecided
		}
		r.State = StateEnded
		if by != "" {
			r.By = by
		}
		if now.Before(r.Ends) {
			r.Ends = now.UTC()
		}
		return nil
	})
}

// Due lists the waiting requests whose time is over and the grants that
// have ended by now.
func (e *Emergency) Due(now time.Time) (grant, end []Request, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return nil, nil, err
	}
	for _, r := range e.state.Requests {
		switch {
		case r.State == StateWaiting && !now.Before(r.Due):
			grant = append(grant, r)
		case r.State == StateGranted && !now.Before(r.Ends):
			end = append(end, r)
		}
	}
	return grant, end, nil
}

// Active returns the granted request of an account, if any.
func (e *Emergency) Active(account string, now time.Time) (Request, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadLocked() != nil {
		return Request{}, false
	}
	for _, r := range e.state.Requests {
		if r.Account == account && r.State == StateGranted && now.Before(r.Ends) {
			return r, true
		}
	}
	return Request{}, false
}

// NewCode makes a printed one-time code for an account and keeps only its
// hash; the code itself is shown once.
func (e *Emergency) NewCode(account string) (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var sb strings.Builder
	for i, x := range b {
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(x)%len(alphabet)])
	}
	code := sb.String()
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return "", err
	}
	next := e.clone()
	if next.Codes == nil {
		next.Codes = map[string]string{}
	}
	next.Codes[account] = hashCode(code)
	if err := e.saveLocked(next); err != nil {
		return "", err
	}
	return code, nil
}

// HasCode reports whether a printed code waits for an account.
func (e *Emergency) HasCode(account string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadLocked() != nil {
		return false
	}
	_, ok := e.state.Codes[account]
	return ok
}

// DropCodes forgets the codes of accounts not in keep.
func (e *Emergency) DropCodes(keep []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return err
	}
	next := e.clone()
	changed := false
	for k := range next.Codes {
		if !slices.Contains(keep, k) {
			delete(next.Codes, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return e.saveLocked(next)
}

func hashCode(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
	code = strings.ReplaceAll(code, "-", "")
	sum := sha256.Sum256([]byte("titanos-emergency:" + code))
	return hex.EncodeToString(sum[:])
}

// pruneRequests drops the oldest decided requests beyond keep. Waiting and
// granted ones always stay: their deadline or their read access must end.
func pruneRequests(list []Request, keep int) []Request {
	over := len(list) - keep
	if over <= 0 {
		return list
	}
	out := make([]Request, 0, len(list))
	for _, r := range list {
		if over > 0 && !r.Open() {
			over--
			continue
		}
		out = append(out, r)
	}
	return out
}

// Detach takes shares out of what a grant added, because their access was
// changed by someone else since: the end of the grant leaves them alone.
func (e *Emergency) Detach(id string, shares []string) (Request, error) {
	return e.change(id, func(r *Request) error {
		r.Added = slices.DeleteFunc(r.Added, func(s string) bool { return slices.Contains(shares, s) })
		return nil
	})
}
