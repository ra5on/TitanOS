package tmail

import (
	"net/mail"
	"slices"
	"strings"
)

// Provider is a known mail provider: where IMAP and SMTP are, whether an
// app password is needed (or advised), and whether a login with password
// works at all. The list is fixed in titand; nothing is looked up on the
// internet (docs/TMAIL.md, GET /providers).
type Provider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	IMAP    Server   `json:"imap"`
	SMTP    Server   `json:"smtp"`
	// AppPassword: the provider wants an app password (or one is needed
	// with two-factor login).
	AppPassword bool `json:"appPassword"`
	// Supported is false where the provider no longer accepts a password
	// (only OAuth, not in this version).
	Supported bool `json:"supported"`
	// Hint names what to switch on at the provider (key of the web UI's
	// texts: tmail.provider.<id>).
	Hint string `json:"hint"`
}

var providers = []Provider{
	{ID: "gmx", Name: "GMX", Domains: []string{"gmx.de", "gmx.net", "gmx.at", "gmx.ch", "gmx.com"},
		IMAP: Server{"imap.gmx.net", 993, SecurityTLS}, SMTP: Server{"mail.gmx.net", 587, SecurityStartTLS}, Supported: true, Hint: "imap_enable"},
	{ID: "webde", Name: "WEB.DE", Domains: []string{"web.de"},
		IMAP: Server{"imap.web.de", 993, SecurityTLS}, SMTP: Server{"smtp.web.de", 587, SecurityStartTLS}, Supported: true, Hint: "imap_enable"},
	{ID: "ionos", Name: "IONOS (1&1)", Domains: []string{"ionos.de", "online.de", "1und1.de"},
		IMAP: Server{"imap.ionos.de", 993, SecurityTLS}, SMTP: Server{"smtp.ionos.de", 465, SecurityTLS}, Supported: true},
	{ID: "tonline", Name: "Telekom (T-Online)", Domains: []string{"t-online.de", "magenta.de"},
		IMAP: Server{"secureimap.t-online.de", 993, SecurityTLS}, SMTP: Server{"securesmtp.t-online.de", 465, SecurityTLS},
		AppPassword: true, Supported: true, Hint: "mail_password"},
	{ID: "icloud", Name: "iCloud", Domains: []string{"icloud.com", "me.com", "mac.com"},
		IMAP: Server{"imap.mail.me.com", 993, SecurityTLS}, SMTP: Server{"smtp.mail.me.com", 587, SecurityStartTLS},
		AppPassword: true, Supported: true, Hint: "app_password"},
	{ID: "gmail", Name: "Gmail", Domains: []string{"gmail.com", "googlemail.com"},
		IMAP: Server{"imap.gmail.com", 993, SecurityTLS}, SMTP: Server{"smtp.gmail.com", 465, SecurityTLS},
		AppPassword: true, Supported: true, Hint: "app_password_2fa"},
	{ID: "outlook", Name: "Outlook / Hotmail", Domains: []string{"outlook.com", "outlook.de", "hotmail.com", "hotmail.de", "live.com", "live.de", "msn.com"},
		IMAP: Server{"outlook.office365.com", 993, SecurityTLS}, SMTP: Server{"smtp-mail.outlook.com", 587, SecurityStartTLS},
		Supported: false, Hint: "oauth_only"},
	{ID: "posteo", Name: "Posteo", Domains: []string{"posteo.de", "posteo.net", "posteo.at", "posteo.ch", "posteo.org", "posteo.eu"},
		IMAP: Server{"posteo.de", 993, SecurityTLS}, SMTP: Server{"posteo.de", 465, SecurityTLS}, Supported: true},
	{ID: "mailboxorg", Name: "mailbox.org", Domains: []string{"mailbox.org"},
		IMAP: Server{"imap.mailbox.org", 993, SecurityTLS}, SMTP: Server{"smtp.mailbox.org", 465, SecurityTLS}, Supported: true},
	{ID: "freenet", Name: "freenet", Domains: []string{"freenet.de"},
		IMAP: Server{"mx.freenet.de", 993, SecurityTLS}, SMTP: Server{"mx.freenet.de", 587, SecurityStartTLS}, Supported: true, Hint: "imap_enable"},
	{ID: "yahoo", Name: "Yahoo", Domains: []string{"yahoo.com", "yahoo.de"},
		IMAP: Server{"imap.mail.yahoo.com", 993, SecurityTLS}, SMTP: Server{"smtp.mail.yahoo.com", 465, SecurityTLS},
		AppPassword: true, Supported: true, Hint: "app_password"},
}

// Providers returns the fixed list.
func Providers() []Provider {
	out := slices.Clone(providers)
	for i := range out {
		out[i].Domains = slices.Clone(out[i].Domains)
	}
	return out
}

// ProviderFor suggests the provider of an address (false if unknown).
func ProviderFor(email string) (Provider, bool) {
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil {
		return Provider{}, false
	}
	_, domain, _ := strings.Cut(addr.Address, "@")
	domain = strings.ToLower(domain)
	for _, p := range Providers() {
		if slices.Contains(p.Domains, domain) {
			return p, true
		}
	}
	return Provider{}, false
}
