package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestHandshakeErrorsOncePerHourAndClient(t *testing.T) {
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	var lines []string
	l := &serverLog{now: func() time.Time { return now }, seen: map[string]*handshakes{}, output: func(msg string, args ...any) {
		lines = append(lines, fmt.Sprint(append([]any{msg}, args...)...))
	}}
	for port := range 48 {
		fmt.Fprintf(l, "http: TLS handshake error from 100.121.251.25:%d: remote error: tls: unknown certificate\n", 53500+port)
	}
	fmt.Fprintf(l, "http: TLS handshake error from 192.168.1.5:4000: EOF\n")
	fmt.Fprintf(l, "http: panic serving 1.2.3.4:5: boom\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %q", lines)
	}
	now = now.Add(time.Hour)
	fmt.Fprintf(l, "http: TLS handshake error from 100.121.251.25:1: remote error: tls: unknown certificate\n")
	if len(lines) != 4 || !strings.Contains(lines[3], "48") {
		t.Fatalf("after an hour: %q", lines)
	}
}
