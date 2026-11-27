package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

type savedReply struct {
	Text      string `json:"text"`
	DraftUID  uint32 `json:"draftUid"`
	DraftID   string `json:"draftMessageId"`
	DraftDate string `json:"draftDate"`
}
type mailFileReceipt struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
	SHA  string `json:"sha256"`
}
type sendEvidence struct {
	SenderName  string            `json:"senderName"`
	Mailbox     string            `json:"mailbox"`
	DraftUID    uint32            `json:"draftUid,omitempty"`
	DraftDate   string            `json:"draftDate,omitempty"`
	Account     string            `json:"account"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Cc          []string          `json:"cc"`
	Bcc         []string          `json:"bcc"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text"`
	Attachments []mailFileReceipt `json:"attachments"`
	DraftID     string            `json:"draftMessageId,omitempty"`
	InReplyTo   string            `json:"inReplyTo"`
	References  string            `json:"references"`
	SourceSHA   string            `json:"sourceSHA256"`
}

func mailEffectKey(_ auth.User, _ pa.Request, p pa.Preview) string {
	var shown sendEvidence
	if json.Unmarshal(p.Data, &shown) == nil && shown.DraftUID != 0 && shown.DraftID != "" {
		return personalDigest([]string{shown.Account, strconv.FormatUint(uint64(shown.DraftUID), 10), shown.DraftID})
	}
	return personalDigest(p.Data)
}

func (m personalMailTools) loadSavedReply(ctx context.Context, u auth.User, a selectedMail, args savedReply) (tmail.Outgoing, error) {
	var out tmail.Outgoing
	folders, err := m.mail.Folders(ctx, u.Name, a.box.ID)
	if err != nil {
		return out, err
	}
	at := slices.IndexFunc(folders, func(f tmail.Folder) bool { return f.Role == tmail.RoleDrafts })
	if at < 0 || args.DraftID == "" || args.DraftDate == "" {
		return out, errPersonalSource
	}
	d, err := m.mail.ReadQuiet(ctx, u.Name, a.box.ID, folders[at].Path, args.DraftUID)
	if err != nil || d.RawText || d.Truncated || d.MessageID != args.DraftID || d.Date.Format(time.RFC3339Nano) != args.DraftDate || d.InReplyTo != a.message.MessageID || !strings.EqualFold(d.From.Addr, a.box.Email) {
		return out, errPersonalSource
	}
	out = tmail.Outgoing{To: d.To, Cc: d.Cc, Bcc: d.Bcc, Text: d.Text, Subject: d.Subject, InReplyTo: d.InReplyTo, References: d.References, DraftUID: d.UID}
	if len(d.Attachments) > 100 || len(d.To)+len(d.Cc)+len(d.Bcc) == 0 || len(out.Text) > 40<<10 {
		return out, tmail.ErrTooLarge
	}
	remaining := int64(tmail.MaxAttachments)
	for _, part := range d.Attachments {
		if part.Inline || remaining <= 0 {
			return out, errPersonalSource
		}
		data, actual, err := m.mail.PartBytes(ctx, u.Name, a.box.ID, d.Folder, d.UID, part.Part, remaining)
		if err != nil {
			return out, err
		}
		if int64(len(data)) > remaining {
			return out, tmail.ErrTooLarge
		}
		out.Files = append(out.Files, tmail.File{Name: actual.Name, Type: actual.Type, Data: data})
		remaining -= int64(len(data))
	}
	return out, nil
}

func (m personalMailTools) sendPlan(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, selectedMail, tmail.Outgoing, error) {
	var out tmail.Outgoing
	var p pa.Preview
	a, err := m.read(ctx, u, r)
	if err != nil {
		return p, a, out, err
	}
	if len(a.pdfs) != 0 {
		return p, a, out, errPersonalSource
	}
	var args savedReply
	if len(r.Args) > 0 && json.Unmarshal(r.Args, &args) != nil {
		return p, a, out, errors.New("invalid reply")
	}
	msg := a.message
	if args.DraftUID != 0 {
		out, err = m.loadSavedReply(ctx, u, a, args)
		if err != nil {
			return p, a, out, err
		}
	} else {
		body := args.Text
		if body == "" {
			body = r.PreviousText
		}
		if strings.TrimSpace(body) == "" || len(body) > 24000 {
			return p, a, out, errors.New("enter the exact reply text")
		}
		recipients, err := replyTargets(msg)
		if err != nil {
			return p, a, out, err
		}
		out = tmail.Outgoing{To: recipients, Subject: replySubject(msg.Subject), Text: body, InReplyTo: msg.MessageID, References: strings.TrimSpace(msg.References + " " + msg.MessageID)}
	}
	out.Authorize = pa.Reauthorize
	out.ExpectedFrom = &tmail.Address{Name: a.box.SenderName, Addr: a.box.Email}
	out.Answers = &tmail.MailRef{Folder: msg.Folder, UID: msg.UID}
	shown := sendEvidence{SenderName: a.box.SenderName, Mailbox: a.box.Name, DraftUID: args.DraftUID, DraftDate: args.DraftDate, Account: a.box.ID, From: a.box.Email, To: addrLines(out.To), Cc: addrLines(out.Cc), Bcc: addrLines(out.Bcc), Subject: out.Subject, Text: out.Text, Attachments: []mailFileReceipt{}, DraftID: args.DraftID, InReplyTo: out.InReplyTo, References: out.References, SourceSHA: fmt.Sprintf("%x", sha256.Sum256(jsonPIM([]string{msg.MessageID, msg.Subject, msg.Text, msg.From.Addr, msg.References})))}
	lines := []string{strings.Join(addrLines([]tmail.Address{*out.ExpectedFrom}), "") + " → " + strings.Join(shown.To, ", ")}
	if len(shown.Cc) > 0 {
		lines = append(lines, "Cc: "+strings.Join(shown.Cc, ", "))
	}
	if len(shown.Bcc) > 0 {
		lines = append(lines, "Bcc: "+strings.Join(shown.Bcc, ", "))
	}
	lines = append(lines, shown.Subject, fmt.Sprintf("Anhänge: %d", len(out.Files)))
	for _, f := range out.Files {
		shown.Attachments = append(shown.Attachments, mailFileReceipt{f.Name, f.Type, len(f.Data), fmt.Sprintf("%x", sha256.Sum256(f.Data))})
		lines = append(lines, fmt.Sprintf("%s · %s · %d B", f.Name, f.Type, len(f.Data)))
	}
	lines = append(lines, shown.Text)
	return pa.Preview{Title: "Antwort senden", Text: strings.Join(lines, "\n"), Data: jsonPIM(shown), Sources: []pa.Source{a.source}}, a, out, nil
}

func (m personalMailTools) prepareSend(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	release, err := m.mail.Begin(ctx, u.Name, 3*tmail.MaxAttachments)
	if err != nil {
		return pa.Preview{}, err
	}
	defer release()
	p, _, _, err := m.sendPlan(ctx, u, r)
	return p, err
}
func (m personalMailTools) send(ctx context.Context, u auth.User, r pa.Request, approved pa.Preview) (pa.Result, error) {
	release, err := m.mail.Begin(ctx, u.Name, 3*tmail.MaxAttachments)
	if err != nil {
		return pa.Result{}, fmt.Errorf("%w: %v", pa.ErrNoEffect, err)
	}
	defer release()
	fresh, a, out, err := m.sendPlan(ctx, u, r)
	if err != nil {
		return pa.Result{}, fmt.Errorf("%w: %v", pa.ErrNoEffect, err)
	}
	if !bytes.Equal(fresh.Data, approved.Data) || ctx.Err() != nil {
		return pa.Result{}, fmt.Errorf("%w: reply changed or authorization interrupted", pa.ErrNoEffect)
	}
	receipt, err := m.mail.Send(ctx, u.Name, a.box.ID, out)
	return sentEvidence(receipt, err, fresh.Sources, "Antwort")
}

func sentEvidence(receipt tmail.SendResult, err error, sources []pa.Source, noun string) (pa.Result, error) {
	if err != nil {
		if errors.Is(err, tmail.ErrSMTPUnconfirmed) {
			return pa.Result{}, err
		}
		if errors.Is(err, pa.ErrNoEffect) {
			return pa.Result{}, err
		}
		for _, sentinel := range []error{tmail.ErrSMTPRejected, tmail.ErrAuthFailed, tmail.ErrDisabled, tmail.ErrNoPassword, tmail.ErrInvalid, tmail.ErrTooLarge, tmail.ErrNotFound, tmail.ErrTLS, tmail.ErrHostForbidden} {
			if errors.Is(err, sentinel) {
				return pa.Result{}, fmt.Errorf("%w: SMTP took no message", pa.ErrNoEffect)
			}
		}
		return pa.Result{}, errors.New("SMTP acceptance unconfirmed; inspect Sent before a new send")
	}
	text := "SMTP hat die " + noun + " angenommen."
	if receipt.ProviderFilesSent {
		text += " Der Anbieter übernimmt das Ablegen im Gesendet-Ordner."
	} else if receipt.SentCopy {
		text += " Kopie im Ordner Gesendet bestätigt."
	} else {
		text += " Das Speichern der Kopie im Ordner Gesendet konnte nicht bestätigt werden."
	}
	return pa.Result{Title: noun + " gesendet", Text: text, Sources: sources, Data: jsonPIM(map[string]any{"accepted": true, "sentCopy": receipt.SentCopy && !receipt.ProviderFilesSent, "providerFilesSent": receipt.ProviderFilesSent, "messageId": receipt.MessageID, "acceptedAt": receipt.AcceptedAt})}, nil
}
