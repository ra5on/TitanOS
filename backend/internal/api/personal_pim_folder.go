package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// List only unread headers in the folder the user opened. No message body,
// attachment, or IMAP Seen flag is needed for this overview.
func (m personalMailTools) folderCheck(_ context.Context, u auth.User, r pa.Request) error {
	if err := m.allowed(u); err != nil {
		return err
	}
	if len(r.Sources) != 1 || r.Sources[0].Kind != "mail.folder" {
		return errPersonalSource
	}
	ref := r.Sources[0].Ref
	if ref["folder"] == "" || len(ref["folder"]) > 500 || strings.ContainsAny(ref["folder"], "\r\n\x00") {
		return errPersonalSource
	}
	for _, box := range m.mail.Mailboxes(u.Name) {
		if ref["account"] == box.ID {
			return nil
		}
	}
	return errPersonalSource
}
func (m personalMailTools) folderPreview(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	if err := m.folderCheck(ctx, u, r); err != nil {
		return pa.Preview{}, err
	}
	src := r.Sources[0]
	ref := src.Ref
	page, err := m.mail.List(ctx, u.Name, ref["account"], tmail.ListQuery{Folder: ref["folder"], Unread: true, Limit: 5})
	if err != nil {
		return pa.Preview{}, errors.New("selected mailbox folder unavailable")
	}
	lines := []string{}
	sources := []pa.Source{}
	for _, msg := range page.Messages {
		lines = append(lines, msg.From.Addr+" · "+msg.Subject)
		uid := strconv.FormatUint(uint64(msg.UID), 10)
		sources = append(sources, pa.Source{Kind: "mail", ID: ref["account"] + "/" + uid, Label: msg.Subject, Ref: map[string]string{"account": ref["account"], "folder": ref["folder"], "uid": uid, "date": msg.Date.Format(time.RFC3339Nano), "messageId": msg.MessageID}})
	}
	if len(lines) == 0 {
		lines = append(lines, "Keine ungelesenen Nachrichten.")
	}
	return pa.Preview{Title: ref["folder"] + " · Ungelesen", Text: strings.Join(lines, "\n"), Sources: sources, Data: jsonPIM(map[string]any{"folder": ref["folder"], "more": page.Next != 0, "shown": len(page.Messages)})}, nil
}
