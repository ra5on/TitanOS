package tmail

import (
	"context"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// ThreadReplies returns only the headers of messages referencing the exact
// selected Message-ID in one selected folder. Subjects never establish identity.
func (m *Manager) ThreadReplies(ctx context.Context, user, id, folder, messageID string) ([]Summary, error) {
	return m.threadHeaders(ctx, user, id, folder, messageID, false)
}

// ThreadMembers includes the root message plus descendants that cite its exact
// identifier in this folder. It never follows a link to another account or folder.
func (m *Manager) ThreadMembers(ctx context.Context, user, id, folder, root string) ([]Summary, error) {
	return m.threadHeaders(ctx, user, id, folder, root, true)
}
func (m *Manager) threadHeaders(ctx context.Context, user, id, folder, messageID string, includeRoot bool) ([]Summary, error) {
	if len(messageID) > 998 || len(messageID) < 3 || messageID[0] != '<' || messageID[len(messageID)-1] != '>' || strings.ContainsAny(messageID, "\r\n\x00 \t") {
		return nil, ErrInvalid
	}
	var rows []Summary
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		header := func(name string) imap.SearchCriteria {
			return imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: name, Value: messageID}}}
		}
		criteria := imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{header("In-Reply-To"), header("References")}}}
		if includeRoot {
			criteria = imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{criteria, header("Message-ID")}}}
		}
		found, err := uidSearch(c, &criteria, true)
		if err != nil {
			return err
		}
		if uidCount(found.All) > 40 {
			return ErrTooLarge
		}
		ids := uidsDesc(found.All, 0, 40)
		if len(ids) == 0 {
			return nil
		}
		opts := &imap.FetchOptions{UID: true, Envelope: true, BodySection: []*imap.FetchItemBodySection{fresh(listReferencesSection)}}
		messages, err := collect(c.Fetch(uidSet(ids...), opts))
		if err != nil {
			return err
		}
		for _, item := range messages {
			row := summaryOf(item)
			if includeRoot && row.MessageID == messageID {
				rows = append(rows, row)
				continue
			}
			// IMAP SEARCH HEADER matches substrings. Validate the message-id locally.
			for _, ref := range strings.Fields(row.InReplyTo + " " + row.References) {
				if ref == messageID && row.MessageID != messageID {
					rows = append(rows, row)
					break
				}
			}
		}
		return nil
	})
	return rows, err
}
