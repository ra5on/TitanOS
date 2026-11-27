package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func personalDevData(o options) string {
	if o.dev {
		return filepath.Join(o.configDir, "dev-personal-agent")
	}
	return ""
}
func (w *titand) startPersonalBroker(ctx context.Context) {
	socket := pa.BrokerSocket
	uid := os.Geteuid()
	if w.opts.dev {
		if uid == 0 {
			slog.Warn("Agent development worker requires an unprivileged user")
			return
		}
		// A short socket path also works with long checkout and temporary configuration paths.
		dir, err := os.MkdirTemp("", "titanos-personal-broker-")
		if err != nil {
			slog.Warn("Agent development socket", "error", err)
			return
		}
		socket = filepath.Join(dir, "broker.sock")
		executable, err := os.Executable()
		if err != nil {
			slog.Warn("Agent worker executable", "error", err)
			return
		}
		w.background(func() {
			defer os.Remove(dir)
			cmd := exec.CommandContext(ctx, executable, "-personal-agent-worker", "-personal-agent-socket", socket)
			if err := cmd.Run(); err != nil && ctx.Err() == nil {
				slog.Warn("Agent worker stopped", "error", err)
			}
		})
	} else {
		systemUser, err := user.Lookup(pa.WorkerUser)
		if err != nil {
			slog.Error("Agent service user unavailable", "error", err)
			return
		}
		uid, err = strconv.Atoi(systemUser.Uid)
		if err != nil || uid == 0 {
			slog.Error("Invalid Agent service user")
			return
		}
	}
	w.background(func() {
		if err := w.handler.ServePersonalBroker(ctx, socket, uid); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Agent broker stopped", "error", err)
		}
	})
}
func runPersonalWorker(ctx context.Context, socket string) int {
	if err := pa.RunWorker(ctx, socket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
