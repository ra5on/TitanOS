package tmail

import (
	"context"
	"errors"
)

// ErrPasswordRequired: a stored password is only ever sent to the hosts it
// was stored for. Changing the IMAP or SMTP host (or using a stored
// password for a check against other hosts) needs the password again.
var ErrPasswordRequired = errors.New("tmail: enter the password again for another host")

// Check is the answer of one protocol in a check of the settings.
type Check struct {
	OK   bool   `json:"ok"`
	Code string `json:"code"`
}

// CheckResult is POST /api/v1/mail/accounts/test.
type CheckResult struct {
	IMAP Check `json:"imap"`
	SMTP Check `json:"smtp"`
}

func sameHosts(a Mailbox, in MailboxInput) bool {
	return a.IMAP.Host == in.IMAP.Host && a.SMTP.Host == in.SMTP.Host
}

// CheckSettings logs in to IMAP and SMTP with settings the person entered
// (nothing is stored). Without a password the stored one of mailbox id
// is used, for the same hosts only. A check that goes through with the
// stored settings ends the stop after a refused login.
func (m *Manager) CheckSettings(ctx context.Context, user, id string, in MailboxInput) (CheckResult, error) {
	if !m.Allowed(user) {
		return CheckResult{}, ErrDisabled
	}
	in, err := cleanInput(in)
	if err != nil {
		return CheckResult{}, err
	}
	password := in.Password
	var stored Mailbox
	if password == "" {
		if stored, err = m.mailbox(user, id); err != nil {
			return CheckResult{}, err
		}
		if !sameHosts(stored, in) {
			return CheckResult{}, ErrPasswordRequired
		}
		if password, err = m.password(user, stored); err != nil {
			return CheckResult{}, err
		}
	}
	mb := Mailbox{ID: id, Email: in.Email, User: in.User, IMAP: in.IMAP, SMTP: in.SMTP}
	var out CheckResult
	c, err := m.connect(ctx, mb, password, nil)
	if err == nil {
		_ = c.Close() // only the login was asked for
		out.IMAP.OK = true
	} else {
		out.IMAP.Code = Code(err)
	}
	if err := m.smtpSend(ctx, mb, password, "", nil, nil); err == nil {
		out.SMTP.OK = true
	} else {
		out.SMTP.Code = Code(err)
	}
	if out.IMAP.OK && out.SMTP.OK && in.Password == "" && stored.User == in.User && stored.IMAP == in.IMAP && stored.SMTP == in.SMTP {
		m.clearAuthFailed(user, id)
		m.Sync(user)
	}
	return out, nil
}
