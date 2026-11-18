package auth

import (
	"strconv"
	"strings"
	"testing"
)

// A key carries a name as the client sent it. Without a bound, a full
// table of names the size of a header held gigabytes (security review
// 1.11, S-1); a long key is kept short and still counts as one name.
func TestLimiterKeepsLongKeysShort(t *testing.T) {
	l := NewLimiter()
	long := strings.Repeat("x", 512<<10)
	for i := range 200 {
		l.Fail("cal:10.0.0.1", "cal-user:"+long+strconv.Itoa(i))
	}
	total := 0
	for key := range l.entries {
		if len(key) > limiterMaxKey {
			t.Fatalf("key of %d bytes kept", len(key))
		}
		total += len(key)
	}
	if total > 201*limiterMaxKey {
		t.Fatalf("%d bytes of keys", total)
	}
	// The same long name maps to the same entry, another one does not.
	name := "cal-user:" + long + "a"
	for range limiterFreeAttempts {
		l.Fail(name)
	}
	if l.RetryAfter(name) == 0 {
		t.Fatal("a long name is not counted as one key")
	}
	if l.RetryAfter("cal-user:"+long+"b") > 0 {
		t.Fatal("two long names share a key")
	}
	l.Reset(name)
	if l.RetryAfter(name) > 0 {
		t.Fatal("Reset misses a long key")
	}
}
