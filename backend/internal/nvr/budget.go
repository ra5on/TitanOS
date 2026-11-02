package nvr

// Outcome is what a camera answered to a login attempt.
type Outcome int

const (
	// OutcomeOther is anything that says nothing about the password: no
	// answer, refused connection, wrong path.
	OutcomeOther Outcome = iota
	OutcomeOK
	// OutcomeUnauthorized is "wrong user or password" (HTTP 401, RTSP 401).
	OutcomeUnauthorized
	// OutcomeLocked is the camera saying that the account is locked or that
	// it refuses logins for now (e.g. HTTP 403/423 with a lock message).
	OutcomeLocked
)

// DefaultAuthAttempts is how many rejected logins one search may cause.
// Cameras of some makers lock the account after about five wrong passwords
// for several minutes; the budget stays well below that.
const DefaultAuthAttempts = 3

// AuthBudget stops a search before it locks the account of a camera: it
// counts rejected logins and stops after the limit or at once after a lock.
type AuthBudget struct {
	max    int
	failed int
	locked bool
}

// NewAuthBudget returns a budget of max rejected logins (at least one).
func NewAuthBudget(max int) *AuthBudget {
	if max < 1 {
		max = 1
	}
	return &AuthBudget{max: max}
}

// Allow tells whether another login attempt is permitted.
func (b *AuthBudget) Allow() bool { return !b.locked && b.failed < b.max }

// Record counts the outcome of an attempt.
func (b *AuthBudget) Record(outcome Outcome) {
	switch outcome {
	case OutcomeUnauthorized:
		b.failed++
	case OutcomeLocked:
		b.locked = true
	}
}

// Locked tells that the camera reported a locked account: the search stops
// and the person is told, not retried.
func (b *AuthBudget) Locked() bool { return b.locked }

// Rejected is the number of logins the camera rejected.
func (b *AuthBudget) Rejected() int { return b.failed }
