package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// 1.15 (115-4, drafts 20–24, owner 19 Nov 2026): "Alle Posteingänge" and
// "Markiert" on top of the mailboxes. One list over all mailboxes of the
// account: the inboxes (paged) or the flagged mails of every folder (the
// newest hits of each mailbox, like the search). Each row names its
// mailbox and folder; a mailbox that cannot be read now is named in
// failed and the others still come.

type unifiedHit struct {
	tmail.Summary
	Account string `json:"account"`
	Folder  string `json:"folder"`
}

type unifiedFailure struct {
	Account string `json:"account"`
	Code    string `json:"code"`
}

type unifiedPage struct {
	Messages []unifiedHit     `json:"messages"`
	Total    int              `json:"total"`
	Next     string           `json:"next"`
	Failed   []unifiedFailure `json:"failed"`
}

// unifiedCursor is "<mailbox>:<uid>,…": per mailbox the uid the next page
// starts below; a mailbox at 0 has no more mails.
func unifiedCursor(raw string) map[string]uint32 {
	out := map[string]uint32{}
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		id, uid, ok := strings.Cut(part, ":")
		n, err := strconv.ParseUint(uid, 10, 32)
		if ok && err == nil && orderMailboxID.MatchString(id) {
			out[id] = uint32(n)
		}
	}
	return out
}

func (a tmailArea) handleUnified(w http.ResponseWriter, r *http.Request) {
	q, ok := listQuery(w, r)
	if !ok {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "inbox" && kind != "flagged" {
		writeError(w, http.StatusBadRequest, "mail_invalid", "kind is inbox or flagged")
		return
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	user := currentUser(r).Name
	cursor := unifiedCursor(r.URL.Query().Get("cursor"))
	paged := r.URL.Query().Get("cursor") != ""
	out := unifiedPage{Messages: []unifiedHit{}, Failed: []unifiedFailure{}}
	var parts []unifiedPart
	for _, mb := range a.mail.Mailboxes(user) {
		before, known := cursor[mb.ID]
		if paged && (!known || before == 0) {
			continue
		}
		if kind == "flagged" {
			sq := q
			sq.Flagged, sq.Folder, sq.Before = true, "", 0
			res, err := a.mail.Search(r.Context(), user, mb.ID, sq)
			if err != nil {
				out.Failed = append(out.Failed, unifiedFailure{Account: mb.ID, Code: tmail.VagueCode(err)})
				continue
			}
			out.Total += res.Total
			for _, h := range res.Messages {
				out.Messages = append(out.Messages, unifiedHit{Summary: h.Summary, Account: mb.ID, Folder: h.Folder})
			}
			continue
		}
		lq := q
		lq.Folder, lq.Before = "INBOX", before
		page, err := a.mail.List(r.Context(), user, mb.ID, lq)
		if err != nil {
			out.Failed = append(out.Failed, unifiedFailure{Account: mb.ID, Code: tmail.VagueCode(err)})
			continue
		}
		out.Total += page.Total
		for _, m := range page.Messages {
			out.Messages = append(out.Messages, unifiedHit{Summary: m, Account: mb.ID, Folder: "INBOX"})
		}
		parts = append(parts, unifiedPart{id: mb.ID, more: page.Next != 0})
	}
	if kind == "flagged" {
		slices.SortStableFunc(out.Messages, func(x, y unifiedHit) int { return y.Date.Compare(x.Date) })
	} else {
		out.Messages, out.Next = mergeInboxes(out.Messages, parts, q.Limit)
	}
	writeJSON(w, http.StatusOK, out)
}

// unifiedPart is what one mailbox gave to a page of the inboxes.
type unifiedPart struct {
	id   string
	more bool
}

// mergeInboxes takes the newest limit mails of the inboxes' pages (each
// newest uid first) and keeps every mailbox's own order, so that a page
// always shows a beginning of each mailbox's list: the next page of a
// mailbox starts below its last mail shown and skips none. The cursor
// names each mailbox that has more.
func mergeInboxes(all []unifiedHit, parts []unifiedPart, limit int) ([]unifiedHit, string) {
	lists := map[string][]unifiedHit{}
	for _, m := range all {
		lists[m.Account] = append(lists[m.Account], m)
	}
	head := map[string]int{}
	var out []unifiedHit
	for len(out) < limit {
		best := ""
		for _, p := range parts {
			l := lists[p.id]
			if head[p.id] >= len(l) {
				continue
			}
			if best == "" || l[head[p.id]].Date.After(lists[best][head[best]].Date) {
				best = p.id
			}
		}
		if best == "" {
			break
		}
		out = append(out, lists[best][head[best]])
		head[best]++
	}
	var next []string
	for _, p := range parts {
		l := lists[p.id]
		switch {
		case head[p.id] < len(l):
			// Not all of this page fit: go on at the first one not shown.
			next = append(next, p.id+":"+strconv.FormatUint(uint64(l[head[p.id]].UID)+1, 10))
		case p.more && len(l) > 0:
			next = append(next, p.id+":"+strconv.FormatUint(uint64(l[len(l)-1].UID), 10))
		}
	}
	if out == nil {
		out = []unifiedHit{}
	}
	return out, strings.Join(next, ",")
}
