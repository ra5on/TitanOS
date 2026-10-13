package auth

import (
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
)

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
		if e, ok := l.entries[key]; ok && e.blockedUntil.After(now) {
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
		e, ok := l.entries[key]
		if !ok || now.Sub(e.last) > limiterForget {
			if !ok && len(l.entries) >= limiterMaxKeys {
				continue
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

// Reset forgets the failures of a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
