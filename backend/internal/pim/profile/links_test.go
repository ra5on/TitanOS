package profile

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func testLinks() (*Links, *time.Time) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	l := &Links{}
	l.Now = func() time.Time { return now }
	return l, &now
}

func TestLinkOnce(t *testing.T) {
	l, _ := testLinks()
	link, token, err := l.New(Request{Account: "alex", Device: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidToken(token) || len(token) != 43 || strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q", token)
	}
	if link.State != StatePending || !link.Expires.Equal(time.Date(2026, 11, 18, 10, 10, 0, 0, time.UTC)) {
		t.Fatalf("link %+v", link)
	}
	req, id, ok := l.Take(token)
	if !ok || req.Account != "alex" || id != link.ID {
		t.Fatalf("take: %v %v", req, ok)
	}
	if _, _, ok := l.Take(token); ok {
		t.Fatal("taken twice")
	}
	l.Done(id, "")
	if st, _ := l.Status("alex", link.ID); st.State != StateFetched {
		t.Fatalf("state %s", st.State)
	}
	// Another account does not see it.
	if _, err := l.Status("bea", link.ID); err != ErrNoLink {
		t.Fatalf("bea sees the link: %v", err)
	}
	if err := l.Cancel("bea", link.ID); err != ErrNoLink {
		t.Fatalf("bea cancels the link: %v", err)
	}
}

// Many fetches at once: exactly one gets the request.
func TestLinkTakenOnceInParallel(t *testing.T) {
	l, _ := testLinks()
	_, token, _ := l.New(Request{Account: "alex"})
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := 0
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, ok := l.Take(token); ok {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got != 1 {
		t.Fatalf("taken %d times", got)
	}
}

func TestLinkExpires(t *testing.T) {
	l, now := testLinks()
	link, token, _ := l.New(Request{Account: "alex"})
	*now = now.Add(TTL - time.Second)
	if st, _ := l.Status("alex", link.ID); st.State != StatePending {
		t.Fatalf("state %s", st.State)
	}
	*now = now.Add(time.Second)
	if _, _, ok := l.Take(token); ok {
		t.Fatal("expired link taken")
	}
	if st, _ := l.Status("alex", link.ID); st.State != StateExpired {
		t.Fatalf("state %s", st.State)
	}
	// Some time later it is gone from the table.
	*now = now.Add(keepDone)
	if _, err := l.Status("alex", link.ID); err != ErrNoLink {
		t.Fatalf("still there: %v", err)
	}
	if len(l.entries) != 0 {
		t.Fatalf("%d entries kept", len(l.entries))
	}
}

func TestLinkCancelAndForget(t *testing.T) {
	l, _ := testLinks()
	a, ta, _ := l.New(Request{Account: "alex"})
	_, tb, _ := l.New(Request{Account: "alex"})
	_, tc, _ := l.New(Request{Account: "bea"})
	if err := l.Cancel("alex", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := l.Take(ta); ok {
		t.Fatal("cancelled link taken")
	}
	l.Forget("alex")
	if _, _, ok := l.Take(tb); ok {
		t.Fatal("link of a forgotten account taken")
	}
	if _, _, ok := l.Take(tc); !ok {
		t.Fatal("bea's link gone")
	}
}

func TestLinkLimits(t *testing.T) {
	l, _ := testLinks()
	var first Link
	var firstToken string
	for i := range MaxPerAccount + 1 {
		link, token, err := l.New(Request{Account: "alex"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first, firstToken = link, token
		}
	}
	if st, _ := l.Status("alex", first.ID); st.State != StateCancelled {
		t.Fatalf("oldest link %s", st.State)
	}
	if _, _, ok := l.Take(firstToken); ok {
		t.Fatal("oldest link still valid")
	}
	for i := 0; ; i++ {
		_, _, err := l.New(Request{Account: "u" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
		if err == ErrFull {
			break
		}
		if i > MaxLinks {
			t.Fatal("no limit")
		}
	}
}

func TestValidToken(t *testing.T) {
	for _, s := range []string{"", "abc", strings.Repeat("a", 42), strings.Repeat("a", 44), strings.Repeat("+", 43), strings.Repeat("a", 42) + "/"} {
		if ValidToken(s) {
			t.Errorf("%q valid", s)
		}
	}
}
