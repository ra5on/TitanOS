package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// proc is a child process in its own process group.
type proc struct {
	name string
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

var debug = os.Getenv("VIEWER_DEBUG") == "1"

// baseEnv is the environment of all children.
func baseEnv() []string {
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + homeDir,
		"DISPLAY=" + display,
		"XDG_RUNTIME_DIR=" + runDir,
		"XDG_CONFIG_HOME=" + homeDir + "/.config",
		"XDG_CACHE_HOME=" + homeDir + "/.cache",
		"PULSE_SERVER=unix:" + pulseSocket,
		"GST_REGISTRY=" + runDir + "/gst-registry.bin",
		"LANG=C.UTF-8",
	}
}

// startProc starts a child. stdout may be nil (discarded). stderr lines
// are logged with the name as prefix if logStderr or VIEWER_DEBUG=1.
func startProc(name string, args []string, env []string, stdout io.Writer, logStderr bool, extra ...*os.File) (*proc, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.ExtraFiles = extra
	cmd.Env = env
	cmd.Dir = runDir
	cmd.Stdout = stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	var stderr io.ReadCloser
	if logStderr || debug {
		var err error
		if stderr, err = cmd.StderrPipe(); err != nil {
			return nil, err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &proc{name: name, cmd: cmd, done: make(chan struct{})}
	if stderr != nil {
		go func() {
			sc := bufio.NewScanner(stderr)
			sc.Buffer(make([]byte, 4096), 64<<10)
			n := 0
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}
				if n++; n > 200 && !debug { // keep logs bounded
					continue
				}
				log.Printf("[%s] %s", name, line)
			}
		}()
	}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *proc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// stop sends SIGTERM to the process itself, so it can end its children
// in order, waits for it, then gives the rest of the process group the
// remaining time to exit on its own (Chromium's network service writes
// the cookie database after the browser process is gone) and finally
// sends SIGKILL to the group.
func (p *proc) stop(timeout time.Duration) {
	if p == nil {
		return
	}
	pgid := p.cmd.Process.Pid
	deadline := time.Now().Add(timeout)
	if !p.exited() {
		_ = syscall.Kill(pgid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(timeout):
		}
	}
	for time.Now().Before(deadline) && syscall.Kill(-pgid, 0) == nil {
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-p.done
}
