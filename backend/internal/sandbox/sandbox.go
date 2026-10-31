// Package sandbox runs helpers that parse files from the shares (ffmpeg,
// pdftoppm, pdftotext, tesseract) in a throwaway systemd unit: as a dynamic
// user without rights, without network, on a read-only system, with no
// capabilities, a system call filter and a memory limit. A crafted video or
// PDF that breaks the parser then reaches nothing it could use.
//
// The helper gets its input on stdin and answers on stdout. systemd-run
// --pipe hands over the very file descriptors, so a regular file on stdin
// stays seekable. Where systemd-run is missing or titand is not root
// (development, tests) the helper runs directly.
package sandbox

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Limits of one helper run.
type Limits struct {
	// MemoryMax in bytes; 0 means 512 MiB.
	MemoryMax int64
	// Runtime ends the unit even if titand is gone; 0 means two minutes.
	Runtime time.Duration
}

var (
	once      sync.Once
	systemRun string
)

// available finds systemd-run once; only root may start system units.
func available() string {
	once.Do(func() {
		if os.Geteuid() != 0 || os.Getenv("TITANOS_NO_SANDBOX") != "" {
			return
		}
		if path, err := exec.LookPath("systemd-run"); err == nil {
			systemRun = path
		} else {
			slog.Warn("systemd-run missing: file helpers run without sandbox")
		}
	})
	return systemRun
}

// properties are the settings of the unit, see systemd.exec(5).
func properties(l Limits) []string {
	memory := l.MemoryMax
	if memory <= 0 {
		memory = 512 << 20
	}
	runtime := l.Runtime
	if runtime <= 0 {
		runtime = 2 * time.Minute
	}
	return []string{
		"DynamicUser=yes", "PrivateNetwork=yes", "PrivateTmp=yes", "PrivateDevices=yes", "PrivateIPC=yes",
		"ProtectSystem=strict", "ProtectHome=yes", "ProtectProc=invisible", "ProcSubset=pid",
		"ProtectKernelTunables=yes", "ProtectKernelModules=yes", "ProtectKernelLogs=yes", "ProtectControlGroups=yes",
		"ProtectClock=yes", "ProtectHostname=yes", "NoNewPrivileges=yes", "RestrictNamespaces=yes",
		"RestrictRealtime=yes", "RestrictSUIDSGID=yes", "LockPersonality=yes", "CapabilityBoundingSet=",
		"RestrictAddressFamilies=AF_UNIX", "SystemCallArchitectures=native", "SystemCallFilter=@system-service",
		"UMask=0077", "MemoryMax=" + strconv.FormatInt(memory, 10), "MemorySwapMax=0", "TasksMax=64",
		"RuntimeMaxSec=" + strconv.Itoa(int(runtime.Seconds())), "Environment=LC_ALL=C PATH=/usr/bin:/bin",
	}
}

// Command returns the command for program, in a sandbox where one can be
// made. Set Stdin, Stdout and Stderr as for exec.Cmd; no other descriptors
// reach the helper.
func Command(ctx context.Context, l Limits, program string, args ...string) *exec.Cmd {
	run := available()
	if run == "" {
		cmd := exec.CommandContext(ctx, program, args...)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
		return cmd
	}
	full := []string{"--pipe", "--wait", "--quiet", "--collect", "--service-type=exec"}
	for _, p := range properties(l) {
		full = append(full, "-p", p)
	}
	full = append(full, "--", program)
	return exec.CommandContext(ctx, run, append(full, args...)...)
}
