package speech

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestSynthesizeClosesPiperConnectionOnStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	entered, exited := make(chan struct{}), make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			exited <- err
			return
		}
		defer conn.Close()
		rd := bufio.NewReader(conn)
		if _, err = readEvent(rd); err != nil {
			exited <- err
			return
		}
		close(entered)
		_, err = rd.ReadByte()
		exited <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := Synthesize(ctx, ln.Addr().String(), "Lokale Testantwort", ""); result <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("missing synthesis request")
	}
	cancel()
	select {
	case err := <-exited:
		if err != io.EOF {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Piper connection remained open")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("returned success after abort")
		}
	case <-time.After(time.Second):
		t.Fatal("synthesizer still running")
	}
}
