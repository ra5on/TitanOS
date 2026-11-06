// Package imapmini is a minimal IMAP4rev1 client for the paperwork
// autopilot (PLAN 54-D): LOGIN, SELECT INBOX, UID SEARCH UNSEEN, UID FETCH
// BODY.PEEK[], UID STORE +FLAGS (\Seen), LOGOUT. It never deletes, moves or
// expunges. Only TLS (implicit, port 993) or STARTTLS, always with the
// certificate checked. The password is never part of an error.
package imapmini

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Security of the connection.
const (
	TLS      = "tls"      // implicit TLS (993)
	StartTLS = "starttls" // plain connect, then STARTTLS (143)
)

// MaxMessage bounds one fetched mail (literals larger are refused).
const MaxMessage = 30 << 20

// Config is how to reach a mailbox.
type Config struct {
	Host     string
	Port     int
	Security string
	User     string
	Password string
	// TLS is an optional base configuration (tests give their own RootCAs);
	// ServerName and verification always stay on.
	TLS *tls.Config
	// Dial replaces net.Dialer (tests).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Client is one connection.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	tag  int
}

var ErrAuth = errors.New("IMAP: login refused (user or password wrong)")

// Dial connects and logs in.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Host == "" || cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, errors.New("IMAP: host and port are needed")
	}
	if cfg.Security != TLS && cfg.Security != StartTLS {
		return nil, errors.New("IMAP: only TLS or STARTTLS")
	}
	dial := cfg.Dial
	if dial == nil {
		d := &net.Dialer{Timeout: 20 * time.Second}
		dial = d.DialContext
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	raw, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("IMAP: cannot reach %s: %w", addr, err)
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLS != nil {
		tc = cfg.TLS.Clone()
		tc.InsecureSkipVerify = false
	}
	tc.ServerName = cfg.Host
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Minute)
	}
	_ = raw.SetDeadline(deadline)
	c := &Client{conn: raw}
	if cfg.Security == TLS {
		t := tls.Client(raw, tc)
		if err := t.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("IMAP: TLS: %w", err)
		}
		c.conn = t
	}
	c.r = bufio.NewReaderSize(c.conn, 64<<10)
	greet, err := c.line()
	if err != nil || !strings.HasPrefix(greet, "* OK") {
		c.conn.Close()
		return nil, fmt.Errorf("IMAP: no greeting")
	}
	if cfg.Security == StartTLS {
		if _, err := c.cmd("STARTTLS"); err != nil {
			c.conn.Close()
			return nil, err
		}
		t := tls.Client(raw, tc)
		if err := t.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("IMAP: STARTTLS: %w", err)
		}
		c.conn = t
		c.r = bufio.NewReaderSize(t, 64<<10)
	}
	if _, err := c.cmdSecret("LOGIN " + quote(cfg.User) + " " + quote(cfg.Password)); err != nil {
		c.conn.Close()
		return nil, ErrAuth
	}
	return c, nil
}

// quote makes an IMAP quoted string; characters a quoted string cannot
// carry are refused by the server (we never build literals from them).
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '\r' || r == '\n' {
			continue
		}
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func (c *Client) line() (string, error) {
	s, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(s) > 1<<20 {
		return "", errors.New("IMAP: line too long")
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// response is the untagged lines and the literals that came with them.
type response struct {
	lines    []string
	literals [][]byte
}

func (c *Client) cmd(command string) (response, error) { return c.send(command, false) }

// cmdSecret sends a command whose text must never appear in an error.
func (c *Client) cmdSecret(command string) (response, error) { return c.send(command, true) }

func (c *Client) send(command string, secret bool) (response, error) {
	c.tag++
	tag := "T" + strconv.Itoa(c.tag)
	verb := strings.SplitN(command, " ", 2)[0]
	if _, err := io.WriteString(c.conn, tag+" "+command+"\r\n"); err != nil {
		return response{}, fmt.Errorf("IMAP %s: %w", verb, err)
	}
	var res response
	for {
		l, err := c.line()
		if err != nil {
			return res, fmt.Errorf("IMAP %s: %w", verb, err)
		}
		if strings.HasPrefix(l, tag+" ") {
			status := strings.TrimPrefix(l, tag+" ")
			if strings.HasPrefix(status, "OK") {
				return res, nil
			}
			if secret {
				return res, fmt.Errorf("IMAP %s refused", verb)
			}
			return res, fmt.Errorf("IMAP %s: %s", verb, clip(status))
		}
		// A literal {n} follows the line.
		if n, ok := literalSize(l); ok {
			if n > MaxMessage {
				return res, fmt.Errorf("IMAP: message larger than %d MiB", MaxMessage>>20)
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return res, fmt.Errorf("IMAP %s: %w", verb, err)
			}
			res.literals = append(res.literals, buf)
			rest, err := c.line()
			if err != nil {
				return res, err
			}
			l += " " + rest
		}
		res.lines = append(res.lines, l)
	}
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func literalSize(l string) (int, bool) {
	if !strings.HasSuffix(l, "}") {
		return 0, false
	}
	i := strings.LastIndexByte(l, '{')
	if i < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(l[i+1:len(l)-1], "+"))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// SelectInbox opens INBOX (read-write, so that \Seen can be set).
func (c *Client) SelectInbox() error {
	_, err := c.cmd("SELECT INBOX")
	return err
}

// Unseen returns the UIDs of the unread mails.
func (c *Client) Unseen() ([]uint32, error) {
	res, err := c.cmd("UID SEARCH UNSEEN")
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, l := range res.lines {
		if !strings.HasPrefix(l, "* SEARCH") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(l, "* SEARCH")) {
			if n, err := strconv.ParseUint(f, 10, 32); err == nil {
				out = append(out, uint32(n))
			}
		}
	}
	return out, nil
}

// Fetch reads a whole mail without marking it read.
func (c *Client) Fetch(uid uint32) ([]byte, error) {
	res, err := c.cmd(fmt.Sprintf("UID FETCH %d BODY.PEEK[]", uid))
	if err != nil {
		return nil, err
	}
	if len(res.literals) == 0 {
		return nil, fmt.Errorf("IMAP: mail %d not there", uid)
	}
	return res.literals[0], nil
}

// MarkSeen sets \Seen on a mail; nothing else is ever changed.
func (c *Client) MarkSeen(uid uint32) error {
	_, err := c.cmd(fmt.Sprintf("UID STORE %d +FLAGS.SILENT (\\Seen)", uid))
	return err
}

// Close logs out and closes the connection.
func (c *Client) Close() error {
	_, _ = c.cmd("LOGOUT")
	return c.conn.Close()
}
