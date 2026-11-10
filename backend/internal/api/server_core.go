package api

import (
	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

// serverCore is what the helpers read that stay with the server because
// every area leans on them (PLAN 8a): the activity log of each request
// (activityLog, recordEvent, recordActivity), the gates of maintenance and
// application backups (admitOperation, appLifecycle), the rules of every
// console (serveStream, consoleIdleAfter), "is work running" (storageBusy,
// PowerBusy), the health and TLS checks of the handler chain, the OpenAPI
// description, where personal notifications go (notifyDir,
// notifyAudience) and the removal of the retired assistant.
//
// It is built when it is asked for, like an area: tests replace the
// activity log, the planned maintenance and the coordinator of application
// backups on a running server, and these helpers must see that at once.
type serverCore struct {
	store        *config.Store
	version      string
	users        *auth.Users
	storage      *storage.Manager
	update       *update.Manager
	terminal     *terminal.Manager
	certificate  *tlscert.Manager
	activity     *activity.Manager
	maintenance  *maintenance.Manager
	applications *appbackup.Manager
}

// core is what the server's own helpers read.
func (s *Server) core() serverCore {
	return serverCore{store: s.opts.Store, version: s.opts.Version, users: s.opts.Users, storage: s.opts.Storage, update: s.opts.Update,
		terminal: s.opts.Terminal, certificate: s.opts.Certificate, activity: s.opts.Activity, maintenance: s.opts.Maintenance,
		applications: s.opts.ApplicationBackups}
}
