package tmail

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Search over all folders of a mailbox (1.12, docs/TMAIL.md "1.12"). Each
// folder is a request of its own on the mailbox's connection, so that a
// list or a message opened meanwhile gets its turn between two folders;
// the whole search has a time budget, a bound on folders and hits, and an
// account searches once at a time.

// SearchHit is a line of a search with the folder it lies in.
type SearchHit struct {
	Summary
	Folder string `json:"folder"`
}

// SearchResult: the newest hits of all folders searched; Total counts
// the hits of these folders, Folders the folders searched. Truncated: not
// every folder was searched (more than maxSearchFolders, out of time, or
// a folder refused the search).
type SearchResult struct {
	Messages  []SearchHit `json:"messages"`
	Total     int         `json:"total"`
	Folders   int         `json:"folders"`
	Truncated bool        `json:"truncated"`
}

const (
	// maxSearchFolders bounds the folders one search goes through.
	maxSearchFolders = 100
	// maxSearchHits bounds the hits of a search (and per folder).
	maxSearchHits = 50
)

// searchTime is how long a search goes through folders (a variable for
// the tests); what is found by then is answered.
var searchTime = 45 * time.Second

// hasCriteria tells whether a query searches for something.
func (q ListQuery) hasCriteria() bool {
	return strings.TrimSpace(q.Q) != "" || strings.TrimSpace(q.From) != "" || q.Attachment || !q.Since.IsZero() ||
		!q.Until.IsZero() || q.Unread || q.Flagged
}

// searchTurn lets an account search once at a time.
func (m *Manager) searchTurn(user string) (func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.searching == nil {
		m.searching = map[string]bool{}
	}
	if m.searching[user] {
		return nil, false
	}
	m.searching[user] = true
	return func() {
		m.mu.Lock()
		delete(m.searching, user)
		m.mu.Unlock()
	}, true
}

// searchOrder: the inbox first, then the folders with a role, then the
// others by name.
func searchOrder(list []Folder) {
	rank := map[string]int{RoleInbox: 0, RoleArchive: 1, RoleSent: 2, RoleDrafts: 3, "": 4, RoleJunk: 5, RoleTrash: 6}
	slices.SortStableFunc(list, func(a, b Folder) int {
		return cmp.Or(cmp.Compare(rank[a.Role], rank[b.Role]), strings.Compare(a.Path, b.Path))
	})
}

// Search finds mails in all folders of a mailbox (q.Folder and q.Before
// are not used).
func (m *Manager) Search(ctx context.Context, user, id string, q ListQuery) (SearchResult, error) {
	res := SearchResult{Messages: []SearchHit{}}
	if q.Limit <= 0 || q.Limit > maxSearchHits {
		q.Limit = maxSearchHits
	}
	if !q.validFilter() || !q.hasCriteria() {
		return res, ErrInvalid
	}
	done, ok := m.searchTurn(user)
	if !ok {
		return res, ErrBusy
	}
	defer done()
	var folders []Folder
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, _ *session) error {
		var err error
		folders, err = listFolders(c, false)
		return err
	})
	if err != nil {
		return res, err
	}
	searchOrder(folders)
	if len(folders) > maxSearchFolders {
		folders, res.Truncated = folders[:maxSearchFolders], true
	}
	type hit struct {
		folder string
		uid    uint32
		date   time.Time
	}
	var hits []hit
	deadline := time.Now().Add(searchTime)
	criteria := searchCriteria(q)
	for _, f := range folders {
		if time.Now().After(deadline) {
			res.Truncated = true
			break
		}
		var found []hit
		count := 0
		err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
			if _, err := selectFolder(c, s, f.Path); err != nil {
				return err
			}
			data, err := uidSearch(c, criteria, true)
			if err != nil {
				return err
			}
			count = uidCount(data.All)
			pick := uidsDesc(data.All, 0, q.Limit)
			if len(pick) == 0 {
				return nil
			}
			msgs, err := collect(c.Fetch(uidSet(pick...), &imap.FetchOptions{UID: true, InternalDate: true}))
			if err != nil {
				return err
			}
			for _, b := range msgs {
				found = append(found, hit{f.Path, uint32(b.UID), b.InternalDate})
			}
			return nil
		})
		switch {
		case err == nil:
			res.Folders++
			res.Total += count
			hits = append(hits, found...)
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalid):
			// The folder went away or refused this search: the others go on.
			res.Truncated = true
		default:
			return res, err
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(b.date.Compare(a.date), strings.Compare(a.folder, b.folder), cmp.Compare(b.uid, a.uid))
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	// The lines of the hits, folder by folder, in the order found.
	byFolder := map[string][]uint32{}
	var order []string
	for _, h := range hits {
		if byFolder[h.folder] == nil {
			order = append(order, h.folder)
		}
		byFolder[h.folder] = append(byFolder[h.folder], h.uid)
	}
	lines := map[string]map[uint32]Summary{}
	for _, folder := range order {
		list, err := m.summaries(ctx, user, id, folder, byFolder[folder])
		if err != nil && !errors.Is(err, ErrNotFound) {
			return res, err
		}
		lines[folder] = map[uint32]Summary{}
		for _, s := range list {
			lines[folder][s.UID] = s
		}
	}
	for _, h := range hits {
		if s, ok := lines[h.folder][h.uid]; ok {
			res.Messages = append(res.Messages, SearchHit{Summary: s, Folder: h.folder})
		}
	}
	return res, nil
}

// Archive moves mails into the archive of the mailbox (the folder with
// the role archive; "Archive" is made if there is none) and returns its
// path.
func (m *Manager) Archive(ctx context.Context, user, id, folder string, uids []uint32) (string, error) {
	if err := checkUIDs(uids); err != nil {
		return "", err
	}
	var archive string
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		var err error
		if archive, err = folderOf(c, RoleArchive, "Archive", true); err != nil {
			return err
		}
		if archive == folder {
			return ErrInvalid
		}
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		_, err = c.Move(uidSet(uids...), archive).Wait()
		return err
	})
	if err != nil {
		return "", err
	}
	m.events.publish(user, Event{Type: "flags", Data: map[string]any{"account": id, "folder": folder, "uids": uids}})
	return archive, nil
}
