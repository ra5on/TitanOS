package tmail

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
)

// The connections of the web UI: one per mailbox while the program is in
// use, one command after the other, closed after a rest (sessionIdle).
// The permanent IDLE connection of a mailbox is separate (watch.go).

// session is one IMAP connection for the web UI.
type session struct {
	mu sync.Mutex
	// cl is the connection; read without mu to close it at any time.
	cl       atomic.Pointer[imapclient.Client]
	fp       string
	selected string
	last     time.Time
	// budget limits what the server may send for the next command.
	budget *budgetConn
}

type connPool struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func key(user, id string) string { return user + "\x00" + id }

func (p *connPool) get(k string) *session {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = map[string]*session{}
	}
	s := p.sessions[k]
	if s == nil {
		// A new connection counts as used now, so that reap does not take
		// it before its first command.
		s = &session{last: time.Now()}
		p.sessions[k] = s
	}
	return s
}

// current tells whether s is still the session of its key (a removed
// mailbox, a forgotten account or a taken grant closed the pool's).
func (p *connPool) current(k string, s *session) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[k] == s
}

// close ends the connection of a mailbox (all of an account with id "").
func (p *connPool) close(user, id string) {
	p.mu.Lock()
	var gone []*session
	for k, s := range p.sessions {
		u, i, _ := strings.Cut(k, "\x00")
		if u == user && (id == "" || i == id) {
			gone = append(gone, s)
			delete(p.sessions, k)
		}
	}
	p.mu.Unlock()
	for _, s := range gone {
		s.kill()
	}
}

// reap closes connections that rested longer than rest.
func (p *connPool) reap(now time.Time, rest time.Duration) {
	p.mu.Lock()
	var gone []*session
	for k, s := range p.sessions {
		if s.mu.TryLock() {
			if now.Sub(s.last) >= rest {
				gone = append(gone, s)
				delete(p.sessions, k)
			}
			s.mu.Unlock()
		}
	}
	p.mu.Unlock()
	for _, s := range gone {
		s.kill()
	}
}

func (p *connPool) closeAll() {
	p.mu.Lock()
	all := p.sessions
	p.sessions = nil
	p.mu.Unlock()
	for _, s := range all {
		s.kill()
	}
}

// kill closes the client without waiting for anything; safe at any time.
func (s *session) kill() {
	if c := s.client(); c != nil {
		_ = c.Close() // the connection is given up; nothing to do on an error
	}
}

func (s *session) client() *imapclient.Client { return s.cl.Load() }

func (s *session) setClient(c *imapclient.Client) { s.cl.Store(c) }

// fingerprint names what a login depends on: a failed login stays failed
// until one of these changes (docs/TMAIL.md "Anbieter schonen").
func fingerprint(mb Mailbox) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{mb.User, mb.IMAP.Host, fmt.Sprint(mb.IMAP.Port), mb.IMAP.Security,
		mb.SMTP.Host, fmt.Sprint(mb.SMTP.Port), mb.SMTP.Security, mb.Password}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// connect opens an IMAP connection to a mailbox and logs in.
func (m *Manager) connect(ctx context.Context, user string, mb Mailbox, password string, handler *imapclient.UnilateralDataHandler) (*imapclient.Client, *budgetConn, error) {
	raw, err := m.dial(ctx, user, mb.IMAP)
	if err != nil {
		return nil, nil, err
	}
	conn := newBudgetConn(raw, budgetLogin)
	opts := &imapclient.Options{TLSConfig: m.tlsConfig(mb.IMAP.Host), UnilateralDataHandler: handler,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader}}
	// Greeting, TLS and login together within twice the time to connect.
	var closed atomic.Bool
	var c *imapclient.Client
	watchdog := time.AfterFunc(2*connectTimeout, func() {
		closed.Store(true)
		_ = conn.Close() // ends a server that does not answer; the error says the rest
	})
	defer watchdog.Stop()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() }) // the request is gone
	defer stop()
	fail := func(err error) (*imapclient.Client, *budgetConn, error) {
		_ = conn.Close() // given up
		switch {
		case conn.tooLarge():
			return nil, nil, ErrTooLarge
		case closed.Load(), ctx.Err() != nil:
			return nil, nil, ErrTimeout
		}
		return nil, nil, err
	}
	if mb.IMAP.Security == SecurityTLS {
		tc := tls.Client(conn, opts.TLSConfig)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fail(tlsError(err))
		}
		c = imapclient.New(tc, opts)
	} else {
		c, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			return fail(tlsError(err))
		}
	}
	if err := c.WaitGreeting(); err != nil {
		return fail(classify(err))
	}
	if c.Caps().Has(imap.AuthCap(sasl.Plain)) {
		err = c.Authenticate(sasl.NewPlainClient("", mb.User, password))
	} else {
		err = c.Login(mb.User, password).Wait()
	}
	if err != nil {
		var imapErr *imap.Error
		if errors.As(err, &imapErr) && imapErr.Code != imap.ResponseCodeUnavailable {
			return fail(ErrAuthFailed)
		}
		return fail(classify(err))
	}
	m.succeeded(user, mb.IMAP)
	conn.set(budgetCommand)
	return c, conn, nil
}

// tlsError is an error while TLS is set up: unreachable and timeouts stay
// what they are, everything else is "no TLS".
func tlsError(err error) error {
	switch c := classify(err); {
	case errors.Is(c, ErrTimeout), errors.Is(c, ErrHostForbidden), errors.Is(c, ErrTLS):
		return c
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route") {
		return classify(err)
	}
	return fmt.Errorf("%w: %s", ErrTLS, safeText(err.Error()))
}

// withIMAP runs work on the web UI's connection of a mailbox within limit;
// a broken connection is closed and made anew next time.
func (m *Manager) withIMAP(ctx context.Context, user, id string, limit time.Duration, work func(c *imapclient.Client, s *session) error) error {
	if !m.Allowed(user) {
		return ErrDisabled
	}
	mb, err := m.mailbox(user, id)
	if err != nil {
		return err
	}
	if m.authFailed(user, mb) {
		return ErrAuthFailed
	}
	s := m.conns.get(key(user, id))
	s.mu.Lock()
	defer s.mu.Unlock()
	// The role is part of the fingerprint: a connection made for an
	// administrator is not used after a demotion.
	fp := fingerprint(mb) + fmt.Sprint(m.confined(user))
	if s.client() != nil && s.fp != fp {
		s.kill()
		s.setClient(nil)
	}
	if c := s.client(); c != nil {
		select {
		case <-c.Closed():
			s.setClient(nil)
		default:
		}
	}
	var expired atomic.Bool
	timer := time.AfterFunc(limit, func() {
		expired.Store(true)
		s.kill()
	})
	defer timer.Stop()
	stop := context.AfterFunc(ctx, s.kill)
	defer stop()
	if s.client() == nil {
		password, err := m.password(user, mb)
		if err != nil {
			return err
		}
		c, budget, err := m.connect(ctx, user, mb, password, nil)
		if err != nil {
			if errors.Is(err, ErrAuthFailed) {
				m.setAuthFailed(user, mb)
			}
			return err
		}
		s.budget = budget
		s.setClient(c)
		s.fp, s.selected = fp, ""
	}
	s.budget.set(budgetCommand)
	err = work(s.client(), s)
	s.last = time.Now()
	if s.budget.tooLarge() {
		err = ErrTooLarge
	} else if expired.Load() {
		err = ErrTimeout
	} else if ctx.Err() != nil && err != nil {
		err = ErrTimeout
	}
	if err != nil && !keepsConnection(err) || !m.conns.current(key(user, id), s) {
		// Broken, or the mailbox, the account or the grant went away while
		// the request ran: nothing stays connected for it.
		s.kill()
		s.setClient(nil)
	}
	return classifyIMAP(err)
}

// keepsConnection tells errors after which the connection is still good:
// answers of the server and refusals of this package.
func keepsConnection(err error) bool {
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		return true
	}
	for _, e := range []error{ErrNotFound, ErrInvalid, ErrFolderExists} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// classifyIMAP maps answers of the server.
func classifyIMAP(err error) error {
	if err == nil {
		return nil
	}
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		switch imapErr.Code {
		case imap.ResponseCodeNonExistent, imap.ResponseCodeTryCreate:
			return fmt.Errorf("%w: %s", ErrNotFound, safeText(imapErr.Text))
		case imap.ResponseCodeAlreadyExists:
			return ErrFolderExists
		case imap.ResponseCodeTooBig, imap.ResponseCodeLimit:
			return ErrTooLarge
		}
		return fmt.Errorf("%w: %s", ErrInvalid, safeText(imapErr.Text))
	}
	return classify(err)
}

// selectFolder selects a folder (read-write) unless it is selected.
func selectFolder(c *imapclient.Client, s *session, folder string) (*imap.SelectData, error) {
	if folder == "" || len(folder) > 1024 || strings.ContainsAny(folder, "\r\n\x00") {
		return nil, ErrInvalid
	}
	if s != nil && s.selected == folder {
		if mb := c.Mailbox(); mb != nil && mb.Name == folder {
			return nil, nil
		}
	}
	data, err := c.Select(folder, nil).Wait()
	if err != nil {
		if s != nil {
			s.selected = ""
		}
		var imapErr *imap.Error
		if errors.As(err, &imapErr) {
			return nil, fmt.Errorf("%w: folder", ErrNotFound)
		}
		return nil, err
	}
	if s != nil {
		s.selected = folder
	}
	return data, nil
}
