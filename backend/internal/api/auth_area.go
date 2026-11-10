package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

// authArea is who a request is and what proves it: the first setup and the
// restore of a backup onto a new stick, login and logout, the session of a
// request (authenticate, cookieState, activeUser), the lock screen, the
// second factor, passkeys, the known devices, one's own account with its
// sessions and picture, the account list of the login page, and entering
// the password again (auth.go, account.go, session_lock.go, passkeys.go,
// login_device.go, login_accounts.go, account_picture.go and parts of
// api.go, docker.go, configbackup.go). It is one area because these call
// each other throughout; an area with narrow dependencies all the same
// (PLAN 8a, see fansArea). The receiver is au: a is taken in these files.
//
// users are the accounts; store holds pictures and what the login page
// keeps; firewall blocks an address after too many attempts; update,
// shares and ssh are told about the first owner and a changed password;
// terminal knows whether consoles ask for the password again;
// configBackup and power restore a backup and restart. sessions, limiter,
// prefs, passkeyState, pictureState, loginState, loadMu and lastLoadTry
// are state of the server that the area points at. recordEvent writes a
// login event to the activity log, loginAppearance reads the look of the
// login page, webSession lets only a person at a web session through, and
// goJob starts the restart after a restore.
type authArea struct {
	users         *auth.Users
	store         *config.Store
	firewall      *firewall.Manager
	update        *update.Manager
	shares        *shares.Manager
	ssh           *sshd.Manager
	terminal      *terminal.Manager
	configBackup  *configbackup.Manager
	power         system.Power
	version       string
	secureCookies bool

	sessions     sessionsFunc
	limiter      *auth.Limiter
	prefs        *preferences
	passkeyState *passkeyChallenges
	pictureState *accountPictures
	loginState   *loginAccountsState
	loadMu       *sync.Mutex
	lastLoadTry  *time.Time

	recordEvent     func(r *http.Request, user, action, target string, ok bool)
	loginAppearance func() (loginAppearance, bool)
	webSession      webSessionFunc
	goJob           jobFunc
}

// authArea is the area of the server. Everything that needs the account
// of a request, the session cookie or a fresh proof of the password asks
// it; the guards of the routes (requireUser, requireAdmin) stay with the
// server and call it.
func (s *Server) authArea() authArea {
	return authArea{users: s.opts.Users, store: s.opts.Store, firewall: s.opts.Firewall, update: s.opts.Update, shares: s.opts.Shares,
		ssh: s.opts.SSH, terminal: s.opts.Terminal, configBackup: s.opts.ConfigBackup, power: s.opts.Power, version: s.opts.Version,
		secureCookies: s.opts.SecureCookies,
		sessions:      s.sessionStore, limiter: s.limiter, prefs: s.prefs, passkeyState: &s.passkeys, pictureState: &s.accountPictures,
		loginState: &s.loginAccounts, loadMu: &s.loadMu, lastLoadTry: &s.lastLoadTry,
		recordEvent: s.recordEvent, loginAppearance: s.loginAppearance, webSession: s.requireWebSession, goJob: s.goJob}
}
