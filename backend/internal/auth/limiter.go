package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// limiterFreeAttempts is the number of wrong passwords before waiting starts.
	limiterFreeAttempts = 5
	limiterBaseDelay    = 30 * time.Second
	limiterMaxDelay     = 15 * time.Minute
	// limiterForget is how long after the last failure the count starts over.
	limiterForget = time.Hour
	// limiterMaxKeys bounds the memory an attacker can make the limiter use.
	limiterMaxKeys = 10000
	// limiterMaxKey bounds the length of a key in the table. Keys carry a
	// name as the client sent it (a Basic header may hold most of a
	// megabyte), and 10 000 such keys would hold gigabytes. A longer key is
	// kept as its start and a hash of the whole: still one key per name,
	// and no account name comes near the bound.
	limiterMaxKey = 128
)

// limiterKey is the form of a key in the table (see limiterMaxKey).
func limiterKey(key string) string {
	if len(key) <= limiterMaxKey {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return key[:64] + "#" + hex.EncodeToString(sum[:16])
}

// Limiter slows down password guessing. Failures are counted per key; callers
// use one key for the account name and one for the remote address.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

type limiterEntry struct {
	failures     int
	last         time.Time
	blockedUntil time.Time
}

// NewLimiter returns a limiter without any recorded failures.
func NewLimiter() *Limiter {
	return &Limiter{entries: map[string]*limiterEntry{}, now: time.Now}
}

// RetryAfter returns how long the caller has to wait before the next attempt
// for any of the keys is allowed; zero means now.
func (l *Limiter) RetryAfter(keys ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var wait time.Duration
	for _, key := range keys {
		if e, ok := l.entries[limiterKey(key)]; ok && e.blockedUntil.After(now) {
			wait = max(wait, e.blockedUntil.Sub(now))
		}
	}
	return wait
}

// Fail records a wrong password for each key.
func (l *Limiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.entries) >= limiterMaxKeys {
		for key, e := range l.entries {
			if now.Sub(e.last) > limiterForget {
				delete(l.entries, key)
			}
		}
	}
	for _, key := range keys {
		key = limiterKey(key)
		e, ok := l.entries[key]
		if !ok || now.Sub(e.last) > limiterForget {
			if !ok && len(l.entries) >= limiterMaxKeys {
				// A full table must not let guesses through uncounted:
				// make room by forgetting the key with the fewest failures
				// that failed longest ago, a blocked one only if all are
				// blocked. Keys under attack stay.
				l.evict(now)
			}
			e = &limiterEntry{}
			l.entries[key] = e
		}
		e.failures++
		e.last = now
		if over := e.failures - limiterFreeAttempts; over >= 0 {
			delay := limiterMaxDelay
			if over < 6 {
				delay = min(limiterBaseDelay<<over, limiterMaxDelay)
			}
			e.blockedUntil = now.Add(delay)
		}
	}
}

// evict forgets one key, see Fail: of the keys that are not blocked the one
// with the fewest failures, among those the one that failed longest ago. A
// flood of keys that failed once (random names, fresh addresses) therefore
// pushes out its own keys and not the count of an account that is being
// guessed between two of its blocks.
func (l *Limiter) evict(now time.Time) {
	oldest, oldestBlocked := "", ""
	for key, e := range l.entries {
		if e.blockedUntil.After(now) {
			if oldestBlocked == "" || e.last.Before(l.entries[oldestBlocked].last) {
				oldestBlocked = key
			}
			continue
		}
		if oldest == "" {
			oldest = key
			continue
		}
		if o := l.entries[oldest]; e.failures < o.failures || e.failures == o.failures && e.last.Before(o.last) {
			oldest = key
		}
	}
	if oldest == "" {
		oldest = oldestBlocked
	}
	delete(l.entries, oldest)
}

// Reset forgets the failures of a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, limiterKey(key))
}
