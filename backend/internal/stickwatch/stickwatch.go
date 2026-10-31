// Package stickwatch notices when the boot stick fails. A worn-out stick
// shows read or write errors in the kernel log long before the NAS no
// longer starts; then is the time for a new stick and the backup of the
// stick. It follows the kernel messages and also hears about writes to
// /config that failed (Failed).
package stickwatch

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Device is where the stick shows up by its label.
const Device = "/dev/disk/by-label/TITANOS"

// quiet is how long after one report the next waits.
const quiet = 6 * time.Hour

// failure are kernel messages that mean the device does not read or write
// reliably.
var failure = regexp.MustCompile(`(?i)(I/O error|Buffer I/O error|critical medium error|unrecovered read error|FAT-fs \([^)]*\): .*error|blk_update_request: .*error|Medium Error|tried to access beyond end of device)`)

// Watcher reports errors of the stick.
type Watcher struct {
	// Report is told once per quiet period, with what happened.
	Report func(what string)
	// resolve finds the kernel names of the stick, e.g. sdb and sdb1.
	resolve func() []string
	now     func() time.Time

	mu   sync.Mutex
	last time.Time
}

// New returns a watcher for the real stick.
func New() *Watcher {
	return &Watcher{resolve: stickNames, now: time.Now}
}

// stickNames are the partition and the disk the stick label points to.
func stickNames() []string {
	target, err := filepath.EvalSymlinks(Device)
	if err != nil {
		return nil
	}
	part := filepath.Base(target)
	disk := strings.TrimRightFunc(part, func(r rune) bool { return r >= '0' && r <= '9' })
	if strings.HasPrefix(part, "nvme") || strings.HasPrefix(part, "mmcblk") {
		if i := strings.LastIndex(part, "p"); i > 0 {
			disk = part[:i]
		}
	}
	return []string{part, disk}
}

// Line looks at one kernel message.
func (w *Watcher) Line(line string) {
	if !failure.MatchString(line) {
		return
	}
	for _, name := range w.resolve() {
		if name != "" && regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\b`).MatchString(line) {
			w.report("read/write error: " + strings.TrimSpace(line))
			return
		}
	}
}

// Failed is called when a write to /config failed.
func (w *Watcher) Failed(err error) {
	w.report("writing the configuration failed: " + err.Error())
}

func (w *Watcher) report(what string) {
	w.mu.Lock()
	now := w.now()
	if !w.last.IsZero() && now.Sub(w.last) < quiet {
		w.mu.Unlock()
		return
	}
	w.last = now
	w.mu.Unlock()
	slog.Error("the boot stick shows errors", "what", what)
	if w.Report != nil {
		w.Report(what)
	}
}

// Follow reads the kernel messages until ctx ends.
func (w *Watcher) Follow(ctx context.Context) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "journalctl", "-k", "--follow", "--lines=0", "-o", "cat")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			w.read(out)
			cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

func (w *Watcher) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 64<<10)
	for scanner.Scan() {
		w.Line(scanner.Text())
	}
}
