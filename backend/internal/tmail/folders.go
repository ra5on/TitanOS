package tmail

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Folder roles (from SPECIAL-USE, else the name).
const (
	RoleInbox   = "inbox"
	RoleSent    = "sent"
	RoleDrafts  = "drafts"
	RoleTrash   = "trash"
	RoleJunk    = "junk"
	RoleArchive = "archive"
)

// Folder is one folder of a mailbox.
type Folder struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Unread int    `json:"unread"`
	Total  int    `json:"total"`
	// Delim separates the levels of Path (1.12: the tree of the web UI);
	// empty when the server names none.
	Delim string `json:"delim,omitempty"`
}

const maxFolders = 500

// roleNames are names of folders in common languages and providers.
var roleNames = map[string][]string{
	RoleSent:    {"sent", "sent items", "sent messages", "sent mail", "gesendet", "gesendete objekte", "gesendete elemente", "gesendete nachrichten", "[gmail]/sent mail"},
	RoleDrafts:  {"drafts", "draft", "entwürfe", "entwurf", "[gmail]/drafts"},
	RoleTrash:   {"trash", "deleted items", "deleted messages", "bin", "papierkorb", "gelöschte objekte", "gelöschte elemente", "[gmail]/trash"},
	RoleJunk:    {"junk", "spam", "junk e-mail", "junk-e-mail", "unerwünscht", "[gmail]/spam"},
	RoleArchive: {"archive", "archiv", "archives"},
}

var specialUse = map[imap.MailboxAttr]string{
	imap.MailboxAttrSent: RoleSent, imap.MailboxAttrDrafts: RoleDrafts, imap.MailboxAttrTrash: RoleTrash,
	imap.MailboxAttrJunk: RoleJunk, imap.MailboxAttrArchive: RoleArchive,
}

// listFolders lists the folders with their roles; status adds the counts.
func listFolders(c *imapclient.Client, status bool) ([]Folder, error) {
	opts := &imap.ListOptions{}
	if c.Caps().Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}
	statusOpts := &imap.StatusOptions{NumMessages: true, NumUnseen: true}
	listStatus := status && (c.Caps().Has(imap.CapListStatus) || c.Caps().Has(imap.CapIMAP4rev2))
	if listStatus {
		opts.ReturnStatus = statusOpts
	}
	// Read one by one and stop after maxFolders: the rest of the answer is
	// not read, the connection is given up instead.
	cmd := c.List("", "*", opts)
	var list []*imap.ListData
	cut := false
	for data := cmd.Next(); data != nil; data = cmd.Next() {
		if len(list) == maxFolders {
			// Too many folders; the next request connects anew.
			abandon(c, func() {
				for cmd.Next() != nil {
				}
				_ = cmd.Close() // the connection is gone
			})
			cut = true
			break
		}
		list = append(list, data)
	}
	if !cut {
		if err := cmd.Close(); err != nil {
			return nil, err
		}
	}
	out := []Folder{}
	taken := map[string]bool{}
	for _, l := range list {
		if slices.Contains(l.Attrs, imap.MailboxAttrNoSelect) || slices.Contains(l.Attrs, imap.MailboxAttrNonExistent) {
			continue
		}
		f := Folder{Path: l.Mailbox, Name: l.Mailbox}
		if l.Delim != 0 {
			f.Delim = string(l.Delim)
			if i := strings.LastIndex(l.Mailbox, string(l.Delim)); i >= 0 {
				f.Name = l.Mailbox[i+1:]
			}
		}
		if strings.EqualFold(l.Mailbox, "INBOX") {
			f.Role = RoleInbox
		}
		for _, attr := range l.Attrs {
			for a, role := range specialUse {
				if strings.EqualFold(string(a), string(attr)) && f.Role == "" && !taken[role] {
					f.Role = role
				}
			}
		}
		if f.Role != "" {
			taken[f.Role] = true
		}
		if l.Status != nil {
			f.Total, f.Unread = count(l.Status.NumMessages), count(l.Status.NumUnseen)
		}
		out = append(out, f)
	}
	// Names only where SPECIAL-USE named nothing.
	for i := range out {
		if out[i].Role != "" {
			continue
		}
		lower := strings.ToLower(out[i].Path)
		for role, names := range roleNames {
			if !taken[role] && (slices.Contains(names, lower) || slices.Contains(names, strings.ToLower(out[i].Name)) && !strings.Contains(lower, "/")) {
				out[i].Role, taken[role] = role, true
				break
			}
		}
	}
	if status && !listStatus {
		// All STATUS at once, then the answers (1.13): one round trip to
		// the provider instead of one per folder.
		cmds := make([]*imapclient.StatusCommand, len(out))
		for i := range out {
			cmds[i] = c.Status(out[i].Path, statusOpts)
		}
		for i, cmd := range cmds {
			data, err := cmd.Wait()
			if err != nil {
				continue
			}
			out[i].Total, out[i].Unread = count(data.NumMessages), count(data.NumUnseen)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Role == RoleInbox) != (out[j].Role == RoleInbox) {
			return out[i].Role == RoleInbox
		}
		return false
	})
	return out, nil
}

func count(n *uint32) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

// Folders lists the folders of a mailbox with their counts.
// They come from the background connection (lanes.go), or kept from a
// moment ago when nothing happened in the mailbox since.
func (m *Manager) Folders(ctx context.Context, user, id string) ([]Folder, error) {
	gen := m.events.generation(user, id)
	if out, ok := m.cachedFolders(user, id, gen, time.Now()); ok {
		return out, nil
	}
	var out []Folder
	err := m.withIMAP(background(ctx), user, id, commandTimeout, func(c *imapclient.Client, _ *session) error {
		var err error
		out, err = listFolders(c, true)
		return err
	})
	if err == nil {
		m.keepFolders(user, id, out, gen, time.Now())
	}
	for _, f := range out {
		if f.Role == RoleInbox {
			m.setUnread(user, id, f.Unread)
		}
	}
	return out, err
}

// folderOf finds the folder of a role; create makes one with name if
// there is none.
func folderOf(c *imapclient.Client, role, name string, create bool) (string, error) {
	list, err := listFolders(c, false)
	if err != nil {
		return "", err
	}
	for _, f := range list {
		if f.Role == role {
			return f.Path, nil
		}
	}
	if !create {
		return "", ErrNotFound
	}
	if err := c.Create(name, nil).Wait(); err != nil {
		var imapErr *imap.Error
		if !errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAlreadyExists {
			return "", err
		}
	}
	return name, nil
}

func validFolder(path string) bool {
	return path != "" && len(path) <= 1024 && !strings.ContainsAny(path, "\r\n\x00*%") && !strings.EqualFold(path, "INBOX")
}

// CreateFolder makes a folder.
func (m *Manager) CreateFolder(ctx context.Context, user, id, path string) error {
	if !validFolder(path) {
		return ErrInvalid
	}
	return m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, _ *session) error {
		return c.Create(path, nil).Wait()
	})
}

// RenameFolder renames a folder (never the INBOX).
func (m *Manager) RenameFolder(ctx context.Context, user, id, path, to string) error {
	if !validFolder(path) || !validFolder(to) {
		return ErrInvalid
	}
	return m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if c.Mailbox() != nil {
			s.selected = ""
			if err := c.Unselect().Wait(); err != nil {
				return err
			}
		}
		return c.Rename(path, to, nil).Wait()
	})
}

// DeleteFolder deletes a folder with its mail at the provider (never the
// INBOX).
func (m *Manager) DeleteFolder(ctx context.Context, user, id, path string) error {
	if !validFolder(path) {
		return ErrInvalid
	}
	return m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if c.Mailbox() != nil {
			s.selected = ""
			if err := c.Unselect().Wait(); err != nil {
				return err
			}
		}
		return c.Delete(path).Wait()
	})
}
