package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// consoleIdle ends a console nobody typed into for this long, and
// consoleRecheck is how often an open console checks that the login behind
// it still holds. Atomic so that tests can shorten them while consoles of
// earlier tests still wind down.
var consoleIdle, consoleRecheck atomic.Int64

func init() {
	consoleIdle.Store(int64(15 * time.Minute))
	consoleRecheck.Store(int64(3 * time.Second))
}

// consoleOptions describes one kind of console for serveStream.
type consoleOptions struct {
	// target names the console in the log ("nas", "vm <name>", …).
	target string
	// raw passes every message to the stream as it is (the VNC screen).
	// Otherwise a text message carries the size of the terminal.
	raw bool
	// readLimit is the largest message accepted from the browser; 0 keeps
	// the default of the WebSocket library.
	readLimit int64
	// input reports whether a message from the browser is someone at
	// work, which keeps the console from ending as idle. nil counts all.
	input func(data []byte) bool
}

// serveStream is the one way from a WebSocket to a console: NAS shell,
// container console, serial line and screen of a VM. It keeps the rules
// every console shares in one place: terminals switched on, the Origin check of the WebSocket, the end after
// consoleIdle without input, the end when terminals are switched off, and
// the end when the login behind it no longer holds (see consoleAllowed).
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, opts consoleOptions, open func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error)) {
	user := currentUser(r)
	// The channel is taken before the check: switching terminals off
	// between the two still closes the channel this console waits on.
	off := s.opts.Terminal.Off()
	if !s.opts.Terminal.Enabled() {
		writeError(w, http.StatusForbidden, "terminal_off", "terminals are switched off in the settings")
		return
	}
	// No console asks for the password again: the login of an owner or
	// administrator is enough (owner decision).
	// Accept checks that the page asking is this site, not another one.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if opts.readLimit > 0 {
		conn.SetReadLimit(opts.readLimit)
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream, resize, err := open(ctx)
	if err != nil {
		slog.Debug("console could not be opened", "target", opts.target, "err", err)
		conn.Close(websocket.StatusInternalError, "console could not be opened")
		return
	}
	defer stream.Close()
	slog.Info("console opened", "target", opts.target, "user", user.Name, "ip", remoteIP(r))
	defer slog.Info("console closed", "target", opts.target, "user", user.Name, "ip", remoteIP(r))

	idleAfter := time.Duration(consoleIdle.Load())
	idle := time.AfterFunc(idleAfter, cancel)
	defer idle.Stop()
	go func() {
		recheck := time.NewTicker(time.Duration(consoleRecheck.Load()))
		defer recheck.Stop()
		for {
			select {
			case <-off:
				// Switching terminals off ends the ones that are open.
				conn.Close(websocket.StatusPolicyViolation, "terminals were switched off")
				cancel()
				return
			case <-recheck.C:
				if !s.consoleAllowed(r, user) {
					slog.Info("console ended with its login", "target", opts.target, "user", user.Name, "ip", remoteIP(r))
					conn.Close(websocket.StatusPolicyViolation, "the login ended")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer cancel()
		buf := make([]byte, 64*1024)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				// The shell ended, or the VM was switched off.
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}()

	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if opts.input == nil || opts.input(data) {
			idle.Reset(idleAfter)
		}
		if kind == websocket.MessageText && !opts.raw {
			var size struct{ Cols, Rows int }
			if json.Unmarshal(data, &size) == nil && resize != nil {
				resize(size.Cols, size.Rows)
			}
			continue
		}
		if _, err := stream.Write(data); err != nil {
			return
		}
	}
}

// consoleAllowed repeats, for a console that is already open, the checks
// requireAdmin made when it opened. A console can outlive its WebSocket
// upgrade by hours; logging out, ending the session under "Angemeldete
// Geräte", deleting the account, a password set by an administrator (it
// ends the sessions of the account) and taking the administrator role
// away must end it as well.
//
// The check runs every consoleRecheck against the live session store and
// account list instead of waiting for notifications: sessions end in
// several places (logout, device list, password, two-factor reset, account
// removal, expiry) and accounts can change from another process, so
// looking at the current state is the one way that cannot miss a path.
// Peek does not count the check as use of the session.
func (s *Server) consoleAllowed(r *http.Request, user auth.User) bool {
	name := user.Name
	_, token := r.Context().Value(apiTokenIdentity{}).(string)
	_, agent := r.Context().Value(inProcessIdentity{}).(string)
	// A console opened through another node of the cluster has no cookie
	// here; that node checks its session.
	if !token && !agent && !viaCluster(r) {
		cookie, err := r.Cookie(s.cookieName())
		if err != nil {
			return false
		}
		session, ok := s.sessions.Peek(cookie.Value)
		if !ok || session.User != name {
			return false
		}
	}
	current, ok := s.activeUser(name)
	// The same name may have been given to a new account.
	return ok && current.Created.Equal(user.Created) && current.Role.CanAdminister()
}

// vncInput tells someone at the keyboard or mouse of a VM screen from the
// viewer asking for the next picture. noVNC sends a FramebufferUpdateRequest
// (RFB message type 3, ten bytes) after every update on its own; a message
// made only of those is no input. Everything else (keys, pointer, clipboard,
// the handshake, anything unknown) counts as input.
func vncInput(data []byte) bool {
	if len(data) == 0 || len(data)%10 != 0 {
		return true
	}
	for i := 0; i < len(data); i += 10 {
		if data[i] != 3 || data[i+1] > 1 {
			return true
		}
	}
	return false
}
