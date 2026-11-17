package api

// T-Mail (docs/TMAIL.md): the mail program of every account that has the
// app grant. Every route is for the web session only (browserOnly), never
// for API tokens, MCP, the KI-Agent or requests titand makes in the name
// of an account, and stays on this node (clusterLocal): no route is in
// publicRoutes or the tool catalog (TestMailIsNoToolAndNoPublicRoute).
// Mailboxes are the account's own: another account's ID is not found.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// tmailArea is T-Mail; an area with narrow dependencies (PLAN 8a, see
// fansArea). mail holds the mailboxes, users the accounts for the grant,
// files and roots the NAS files of an account (attachments from and to
// them), whileLoginHolds ends the event stream with its login.
type tmailArea struct {
	mail            *tmail.Manager
	users           *auth.Users
	files           *files.Manager
	roots           func(r *http.Request) []files.Root
	whileLoginHolds func(r *http.Request, user auth.User) (context.Context, context.CancelFunc)
}

// tmailArea is the area of the server.
func (s *Server) tmailArea() tmailArea {
	return tmailArea{mail: s.opts.Mail, users: s.opts.Users, files: s.opts.Files, roots: s.filesArea().requestRoots,
		whileLoginHolds: s.whileLoginHolds}
}

func (a tmailArea) register(mux *routeMux, g guards) {
	// Only from the web session of this node.
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	admin := func(h http.HandlerFunc) http.HandlerFunc { return g.admin(g.clusterLocal(g.browserOnly(h))) }
	// The grant, per account (Benutzer › Konto › Apps), and the own one.
	mux.HandleFunc("GET /api/v1/users/{name}/mail", admin(a.handleGrant))
	mux.HandleFunc("PUT /api/v1/users/{name}/mail", admin(a.handleGrantSet))
	mux.HandleFunc("GET /api/v1/mail/access", web(a.handleAccess))
	// Everything else needs the grant.
	mail := func(h http.HandlerFunc) http.HandlerFunc { return web(a.granted(h)) }
	mux.HandleFunc("GET /api/v1/mail/providers", mail(a.handleProviders))
	mux.HandleFunc("GET /api/v1/mail/accounts", mail(a.handleAccounts))
	mux.HandleFunc("POST /api/v1/mail/accounts", mail(a.handleAccountAdd))
	mux.HandleFunc("POST /api/v1/mail/accounts/test", mail(a.handleAccountTest))
	mux.HandleFunc("PUT /api/v1/mail/accounts/{id}", mail(a.handleAccountSet))
	mux.HandleFunc("DELETE /api/v1/mail/accounts/{id}", mail(a.handleAccountRemove))
	mux.HandleFunc("GET /api/v1/mail/accounts/{id}/folders", mail(a.handleFolders))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/folders", mail(a.handleFolderCreate))
	mux.HandleFunc("PUT /api/v1/mail/accounts/{id}/folders", mail(a.handleFolderRename))
	mux.HandleFunc("DELETE /api/v1/mail/accounts/{id}/folders", mail(a.handleFolderDelete))
	mux.HandleFunc("GET /api/v1/mail/accounts/{id}/messages", mail(a.handleMessages))
	mux.HandleFunc("GET /api/v1/mail/accounts/{id}/message", mail(a.handleMessage))
	mux.HandleFunc("GET /api/v1/mail/accounts/{id}/part", mail(a.handlePart))
	mux.HandleFunc("GET /api/v1/mail/accounts/{id}/cid", mail(a.handleCID))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/part/save", mail(a.handlePartSave))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/flags", mail(a.handleFlags))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/move", mail(a.handleMove))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/delete", mail(a.handleDelete))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/send", mail(a.handleSend(false)))
	mux.HandleFunc("POST /api/v1/mail/accounts/{id}/draft", mail(a.handleSend(true)))
	mux.HandleFunc("GET /api/v1/mail/contacts", mail(a.handleContacts))
	mux.HandleFunc("DELETE /api/v1/mail/contacts", mail(a.handleContactForget))
	mux.HandleFunc("GET /api/v1/mail/settings", mail(a.handleSettings))
	mux.HandleFunc("PUT /api/v1/mail/settings", mail(a.handleSettingsSet))
	mux.HandleFunc("GET /api/v1/mail/events", mail(a.handleEvents))
}

// granted lets only accounts with the grant through (403 mail_disabled).
func (a tmailArea) granted(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.mail == nil || !a.mail.Allowed(currentUser(r).Name) {
			writeError(w, http.StatusForbidden, "mail_disabled", "T-Mail is not enabled for this account")
			return
		}
		next(w, r)
	}
}

// mailStatus is the HTTP status of each error code of T-Mail.
var mailStatus = map[string]int{
	"mail_disabled": http.StatusForbidden, "mail_auth_failed": http.StatusConflict, "mail_unreachable": http.StatusBadGateway,
	"mail_tls": http.StatusBadGateway, "mail_host_forbidden": http.StatusBadRequest, "mail_timeout": http.StatusGatewayTimeout,
	"mail_too_large": http.StatusRequestEntityTooLarge, "mail_not_found": http.StatusNotFound, "mail_folder_exists": http.StatusConflict,
	"mail_send_failed": http.StatusBadGateway, "mail_invalid": http.StatusBadRequest, "mail_no_password": http.StatusConflict,
	"mail_settings_unreadable": http.StatusServiceUnavailable, "mail_password_required": http.StatusBadRequest,
	"mail_rate_limited": http.StatusTooManyRequests,
}

// writeMailError answers with the code of an error. Only the refusal of
// an SMTP server and invalid input carry their text (never a password:
// the package keeps none in its errors).
func writeMailError(w http.ResponseWriter, err error) {
	code := tmail.Code(err)
	status, ok := mailStatus[code]
	if !ok {
		status = http.StatusBadGateway
	}
	message := map[string]string{
		"mail_disabled": "T-Mail is not enabled for this account", "mail_auth_failed": "the mail server refused the login",
		"mail_unreachable": "the mail server is not reachable", "mail_tls": "no TLS or the certificate of the mail server is not valid",
		"mail_host_forbidden": "this address may not be used for mail", "mail_timeout": "the mail server did not answer in time",
		"mail_too_large": "too large", "mail_not_found": "not found", "mail_folder_exists": "the folder exists already",
		"mail_no_password": "no password is stored for this mailbox", "mail_settings_unreadable": "the T-Mail settings could not be read from /config",
		"mail_password_required": "enter the password again for another host",
		"mail_rate_limited":      "too many checks of the settings; try again in a minute",
	}[code]
	if errors.Is(err, tmail.ErrConfined) {
		message = "accounts of the role user may only use mail servers on the internet, not in the home network"
	}
	if code == "mail_rate_limited" {
		w.Header().Set("Retry-After", "60")
	}
	if code == "mail_send_failed" || code == "mail_invalid" {
		message = err.Error()
	}
	if status >= 500 && code != "mail_unreachable" && code != "mail_tls" && code != "mail_timeout" && code != "mail_send_failed" {
		slog.Warn("T-Mail request failed", "code", code)
	}
	writeError(w, status, code, message)
}

// handleGrant tells whether an account has T-Mail: {enabled}.
func (a tmailArea) handleGrant(w http.ResponseWriter, r *http.Request) {
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": a.mail != nil && a.mail.Granted(target.Name)})
}

// handleGrantSet gives or takes T-Mail: {enabled}. Owners are managed by
// owners only.
func (a tmailArea) handleGrantSet(w http.ResponseWriter, r *http.Request) {
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	if !mayManage(currentUser(r), target.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if a.mail == nil {
		writeError(w, http.StatusServiceUnavailable, "mail_unavailable", "T-Mail is not available")
		return
	}
	noteTarget(r, target.Name)
	if err := a.mail.SetAllowed(target.Name, req.Enabled); err != nil {
		if errors.Is(err, tmail.ErrUnreadable) {
			writeMailError(w, err)
			return
		}
		slog.Error("T-Mail grant not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the setting could not be saved to /config")
		return
	}
	slog.Info("T-Mail grant changed", "account", target.Name, "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": a.mail.Granted(target.Name)})
}

// handleAccess tells the desktop whether to show T-Mail: {enabled}.
func (a tmailArea) handleAccess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": a.mail != nil && a.mail.Allowed(currentUser(r).Name)})
}

func (a tmailArea) handleProviders(w http.ResponseWriter, r *http.Request) {
	if email := r.URL.Query().Get("email"); email != "" {
		// The fields of the provider at the top (host, port and security
		// under imap/smtp, appPassword), known beside them.
		p, ok := tmail.ProviderFor(email)
		writeJSON(w, http.StatusOK, struct {
			Known bool `json:"known"`
			tmail.Provider
		}{ok, p})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": tmail.Providers()})
}

func (a tmailArea) handleAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.mail.Mailboxes(currentUser(r).Name))
}

func (a tmailArea) handleAccountAdd(w http.ResponseWriter, r *http.Request) {
	var in tmail.MailboxInput
	if !readJSON(w, r, &in) {
		return
	}
	view, err := a.mail.AddMailbox(currentUser(r).Name, in)
	if err != nil {
		a.writeSaveError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (a tmailArea) handleAccountSet(w http.ResponseWriter, r *http.Request) {
	var in tmail.MailboxInput
	if !readJSON(w, r, &in) {
		return
	}
	view, err := a.mail.UpdateMailbox(currentUser(r).Name, r.PathValue("id"), in)
	if err != nil {
		a.writeSaveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a tmailArea) handleAccountRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.mail.RemoveMailbox(currentUser(r).Name, r.PathValue("id")); err != nil {
		a.writeSaveError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeSaveError answers a failed change of the settings.
func (a tmailArea) writeSaveError(w http.ResponseWriter, err error) {
	for _, known := range []error{tmail.ErrInvalid, tmail.ErrNotFound, tmail.ErrDisabled, tmail.ErrUnreadable, tmail.ErrPasswordRequired} {
		if errors.Is(err, known) {
			writeMailError(w, err)
			return
		}
	}
	slog.Error("T-Mail settings not saved", "err", err)
	writeError(w, http.StatusInternalServerError, "config_write_failed", "the setting could not be saved to /config")
}

// handleAccountTest checks the login to IMAP and SMTP: {id?, …fields,
// password?} → {imap:{ok,code}, smtp:{ok,code}}.
func (a tmailArea) handleAccountTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		tmail.MailboxInput
		ID string `json:"id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	res, err := a.mail.CheckSettings(r.Context(), currentUser(r).Name, req.ID, req.MailboxInput)
	if err != nil {
		writeMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a tmailArea) handleContacts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.mail.Contacts(currentUser(r).Name, r.URL.Query().Get("q")))
}

func (a tmailArea) handleContactForget(w http.ResponseWriter, r *http.Request) {
	a.mail.ForgetContact(currentUser(r).Name, r.URL.Query().Get("addr"))
	w.WriteHeader(http.StatusNoContent)
}

func (a tmailArea) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.mail.Settings(currentUser(r).Name))
}

// maxSettingsBody bounds PUT /mail/settings (50 signatures of 16 KiB).
const maxSettingsBody = 2 << 20

func (a tmailArea) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req tmail.Settings
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBody)
	if !readJSONBody(w, r, &req) {
		return
	}
	out, err := a.mail.SetSettings(currentUser(r).Name, req)
	if err != nil {
		a.writeSaveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEvents is the event stream of the open program: mail, status and
// flags. It ends with the login and with the grant.
func (a tmailArea) handleEvents(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rc := http.NewResponseController(w)
	events, cancel := a.mail.Subscribe(user.Name)
	defer cancel()
	holds, stop := a.whileLoginHolds(r, user)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": T-Mail\n\n")
	_ = rc.Flush() // a writer without flushing still gets the events at the end
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-holds.Done():
			return
		case <-keepAlive.C:
			if !a.mail.Allowed(user.Name) {
				return
			}
			fmt.Fprint(w, ": keep-alive\n\n")
		case ev, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
		}
		_ = rc.Flush() // see above
	}
}

// mailForgetUser removes T-Mail of an account (deleted, or made anew).
func (s *Server) mailForgetUser(name string) {
	if s.opts.Mail != nil {
		_ = s.opts.Mail.ForgetUser(name) // logged by the package; what stays names its owner
	}
}
