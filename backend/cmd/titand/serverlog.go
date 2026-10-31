package main

import (
	"log"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A browser or app that does not trust the certificate of the NAS tries
// again and again; each try was one line "TLS handshake error" in the log,
// dozens per minute. serverLog says it once per address and hour, with how
// often it happened since; everything else of the HTTP server goes to the
// log as it is.

var handshakeError = regexp.MustCompile(`^http: TLS handshake error from (\S+): (.*)$`)

type serverLog struct {
	mu     sync.Mutex
	now    func() time.Time
	seen   map[string]*handshakes
	output func(msg string, args ...any)
}

type handshakes struct {
	since time.Time
	count int
}

func newServerLog() *log.Logger {
	l := &serverLog{now: time.Now, seen: map[string]*handshakes{}, output: slog.Warn}
	return log.New(l, "", 0)
}

func (l *serverLog) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	m := handshakeError.FindStringSubmatch(line)
	if m == nil {
		l.output(line)
		return len(p), nil
	}
	host := m[1]
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	now := l.now()
	l.mu.Lock()
	entry := l.seen[host]
	report := entry == nil || now.Sub(entry.since) >= time.Hour
	count := 1
	if entry != nil {
		count = entry.count + 1
	}
	if report {
		l.seen[host] = &handshakes{since: now}
		if len(l.seen) > 1000 {
			for k, v := range l.seen {
				if now.Sub(v.since) >= time.Hour {
					delete(l.seen, k)
				}
			}
		}
	} else {
		entry.count++
	}
	l.mu.Unlock()
	if report {
		l.output("TLS handshake failed; the client does not trust the certificate or broke off (repeated tries are counted, one line per hour)", "client", host, "reason", m[2], "tries", count)
	}
	return len(p), nil
}
