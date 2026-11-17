package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/buddy"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/speech"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// mailFeature is T-Mail's folder below .titanos on the system data volume
// (the address book).
const mailFeature = "mail"

// wireMail wires T-Mail (docs/TMAIL.md): the settings are read once now,
// the permanent connections run until titand stops. They are no work of
// the NAS: an app update does not wait for them (updates.Working leaves
// them out) and the end of the daemon closes them without a goodbye.
func (w *titand) wireMail() error {
	w.mail = tmail.New(w.store)
	w.mail.Account = func(name string) (tmail.Account, bool) {
		u, ok := w.users.Get(name)
		return tmail.Account{Name: u.Name, Created: u.Created, Disabled: u.Disabled, Admin: u.Role.CanAdminister()}, ok
	}
	ports := []int{backuptarget.DefaultPort, cluster.RelayPort, buddy.DefaultPort, speech.WhisperPort, speech.PiperPort}
	web := webPorts(w.opts)
	ports = append(ports, web.HTTPS)
	if web.HTTP > 0 {
		ports = append(ports, web.HTTP)
	}
	w.mail.OwnPorts = ports
	w.mail.Volume = func() (*os.Root, func(), error) {
		volume, release := w.systemData.Use(mailFeature)
		if volume == "" {
			release()
			return nil, nil, volumedata.ErrUnavailable
		}
		root, err := volumedata.Open("/mnt", volume, mailFeature, w.volumes.Mounted())
		if err != nil {
			release()
			return nil, nil, err
		}
		return root, func() { _ = root.Close(); release() }, nil // closing a directory changes nothing on disk
	}
	// New mail: the bell of owners and administrators says only that there
	// is mail (the bell is readable by the KI-Agent's tool "notifications",
	// which must not learn senders or subjects); the push message to the
	// account's own devices names sender and subject if the account allows
	// content.
	w.mail.Notify = func(n tmail.Notice) {
		kind := "mail_new"
		if n.Count > 1 {
			kind = "mail_new_many"
		}
		if account, ok := w.users.Get(n.User); ok && account.Role.CanAdminister() {
			w.notifier.NotifyTo(notify.Info, kind, n.MailboxName, []notify.Target{{User: n.User, Bell: true}}, false, notify.About{Ref: "mail", Link: "/mail"})
		}
		title, body := w.notifier.Wording(kind, n.MailboxName)
		if n.Content && w.notifier.PersonalContent(n.User) != "none" && n.Count == 1 {
			title, body = n.From, n.Subject
			if strings.TrimSpace(body) == "" {
				_, body = w.notifier.Wording(kind, n.MailboxName)
			}
		}
		w.pushes.SendMail(n.User, webpush.Message{Title: title, Body: body, Tag: "mail:" + n.Mailbox, URL: "/mail"})
	}
	if err := w.mail.Load(); err != nil {
		slog.Warn("T-Mail settings not readable from /config", "err", err)
	}
	w.background(func() { w.mail.Run(w.ctx) })
	return nil
}
