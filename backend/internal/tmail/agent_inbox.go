package tmail

import (
	"context"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"strings"
)

// InboxCursor identifies the mailbox generation and first unprocessed UID of a watch.
type InboxCursor struct {
	Validity uint32 `json:"validity"`
	Next     uint32 `json:"next"`
}

// AgentInbox examines at most one bounded range of new UIDs in the specified account's
// INBOX. On creation or UIDVALIDITY rollover the baseline starts at the next new
// message. The native IMAP session enforces account grants, connection and response limits.
func (m *Manager) AgentInbox(ctx context.Context, user, id, sender string, from InboxCursor) (InboxCursor, []Summary, error) {
	next := from
	rows := []Summary{}
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, "INBOX"); err != nil {
			return err
		}
		status, err := c.Status("INBOX", &imap.StatusOptions{UIDValidity: true, UIDNext: true}).Wait()
		if err != nil {
			return err
		}
		if status.UIDValidity == 0 || status.UIDNext == 0 {
			return ErrInvalid
		}
		if from.Validity != status.UIDValidity || from.Next == 0 {
			next = InboxCursor{status.UIDValidity, uint32(status.UIDNext)}
			return nil
		}
		if from.Next >= uint32(status.UIDNext) {
			return nil
		}
		stop := uint32(min(uint64(from.Next)+127, uint64(status.UIDNext)-1))
		criteria := &imap.SearchCriteria{UID: []imap.UIDSet{{{Start: imap.UID(from.Next), Stop: imap.UID(stop)}}}, Header: []imap.SearchCriteriaHeaderField{{Key: "FROM", Value: sender}}}
		hits, err := uidSearch(c, criteria, true)
		if err != nil {
			return err
		}
		ids := uidsFrom(hits.All, from.Next, 16)
		if len(ids) > 0 {
			messages, err := collect(c.Fetch(uidSet(ids...), &imap.FetchOptions{UID: true, Envelope: true}))
			if err != nil {
				return err
			}
			for _, msg := range messages {
				r := summaryOf(msg)
				if r.UID >= from.Next && r.UID <= stop && strings.EqualFold(r.From.Addr, sender) && (r.MessageID != "" || !r.Date.IsZero()) {
					rows = append(rows, r)
				}
			}
			if len(ids) == 16 {
				stop = ids[len(ids)-1]
			}
		}
		next.Next = stop + 1
		return nil
	})
	return next, rows, err
}
