package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// 1.15 (115-4): the inboxes of all mailboxes as one list, newest first,
// each mailbox in its own order, page by page without skipping a mail.
func TestMergeInboxesPages(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 11, 19, 8, min, 0, 0, time.UTC) }
	hit := func(acc string, uid uint32, min int) unifiedHit {
		return unifiedHit{Summary: tmail.Summary{UID: uid, Date: at(min)}, Account: acc, Folder: "INBOX"}
	}
	// a: uids 9, 8, 7 (8 older than 7 – the server's order counts); b: 5, 4.
	all := []unifiedHit{hit("a", 9, 50), hit("a", 8, 10), hit("a", 7, 40), hit("b", 5, 45), hit("b", 4, 5)}
	parts := []unifiedPart{{id: "a", more: true}, {id: "b"}}
	out, next := mergeInboxes(all, parts, 3)
	got := ""
	for _, m := range out {
		got += fmt.Sprintf("%s%d ", m.Account, m.UID)
	}
	if got != "a9 b5 a8 " || next != "a:8,b:5" {
		t.Fatalf("page: %q next %q", got, next)
	}
	// All of b fits, a has more on the server.
	out, next = mergeInboxes(all, parts, 10)
	if len(out) != 5 || next != "a:7" {
		t.Fatalf("whole: %d next %q", len(out), next)
	}
	if c := unifiedCursor("a:8,b:5,../x:3,c:z"); len(c) != 2 || c["a"] != 8 || c["b"] != 5 {
		t.Fatalf("cursor %v", c)
	}
}

func TestMailUnifiedRoute(t *testing.T) {
	ts, srv, _ := newMailServer(t)
	ts.grantMail("alex")
	first := ts.addMailbox(ts.client, srv)
	second := ts.addMailbox(ts.client, srv)
	for i := range 3 {
		srv.Deliver("INBOX", fmt.Sprintf("From: a@example.org\r\nTo: alex@example.org\r\nSubject: Nr %d\r\nDate: Thu, 19 Nov 2026 0%d:00:00 +0100\r\nMessage-ID: <u%d@example.org>\r\n\r\nText\r\n", i, i+1, i))
	}
	res, body := ts.do(ts.client, "GET", "/api/v1/mail/unified?kind=inbox&limit=4", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unified: %d %s", res.StatusCode, body)
	}
	var page unifiedPage
	if err := json.Unmarshal(body, &page); err != nil || len(page.Messages) != 4 || page.Total != 6 || page.Next == "" || len(page.Failed) != 0 {
		t.Fatalf("page: %v %s", err, body)
	}
	seen := map[string]bool{}
	for _, m := range page.Messages {
		seen[m.Account] = true
		if m.Folder != "INBOX" {
			t.Errorf("folder %q", m.Folder)
		}
	}
	if !seen[first] || !seen[second] {
		t.Errorf("both mailboxes: %v", seen)
	}
	res, body = ts.do(ts.client, "GET", "/api/v1/mail/unified?kind=inbox&limit=4&cursor="+page.Next, nil)
	var rest unifiedPage
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &rest) != nil || len(rest.Messages) != 2 || rest.Next != "" {
		t.Fatalf("second page: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.client, "GET", "/api/v1/mail/unified?kind=all", nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown kind: %d", res.StatusCode)
	}
	if res, body := ts.do(ts.client, "GET", "/api/v1/mail/unified?kind=flagged", nil); res.StatusCode != http.StatusOK {
		t.Errorf("flagged: %d %s", res.StatusCode, body)
	}
}
