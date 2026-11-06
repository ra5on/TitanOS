package main

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// readClipboard returns the CLIPBOARD selection as text (at most 1 MiB;
// larger contents are ignored).
func readClipboard() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/xclip", "-selection", "clipboard", "-o", "-t", "UTF8_STRING")
	cmd.Env = baseEnv()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", false
	}
	if err := cmd.Start(); err != nil {
		return "", false
	}
	b, _ := io.ReadAll(io.LimitReader(out, proto.MaxClipboard+1))
	_, _ = io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil || len(b) > proto.MaxClipboard {
		return "", false
	}
	return strings.ToValidUTF8(string(b), "�"), true
}

// writeClipboard owns the CLIPBOARD selection with text. xclip forks
// into the background and serves the selection until another client
// takes it; tini reaps it.
func writeClipboard(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/xclip", "-selection", "clipboard", "-i", "-t", "UTF8_STRING")
	cmd.Env = baseEnv()
	cmd.Stdin = bytes.NewReader([]byte(text))
	return cmd.Run()
}
