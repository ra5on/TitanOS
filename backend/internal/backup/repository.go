package backup

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// PublicKey returns the key this NAS shows to SFTP servers, making and
// storing it first if there is none. It goes into authorized_keys there.
func (m *Manager) PublicKey() (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return "", ErrNotLoaded
	}
	if data, err := m.store.Read(keyFile + ".pub"); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(private, "titanos-backup")
	if err != nil {
		return "", err
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " titanos-backup"
	if err := m.store.Write(keyFile, pem.EncodeToMemory(block)); err != nil {
		return "", err
	}
	if err := m.store.Write(keyFile+".pub", []byte(line+"\n")); err != nil {
		return "", err
	}
	return line, nil
}

// --- running restic ---------------------------------------------------------

// rememberHosts adds the lines ssh learnt to the stored known_hosts. Two
// calls may have learnt something at the same time; neither loses the other's.
func (m *Manager) rememberHosts(learnt []byte) {
	m.hostsMu.Lock()
	defer m.hostsMu.Unlock()
	stored, err := m.store.Read(hostFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("backup: key of the server not remembered", "err", err)
		return
	}
	merged := strings.TrimRight(string(stored), "\n")
	known := map[string]bool{}
	for _, line := range strings.Split(merged, "\n") {
		known[line] = true
	}
	added := false
	for _, line := range strings.Split(string(learnt), "\n") {
		if line = strings.TrimSpace(line); line != "" && !known[line] {
			known[line] = true
			if merged != "" {
				merged += "\n"
			}
			merged += line
			added = true
		}
	}
	if !added {
		return
	}
	if err := m.store.Write(hostFile, []byte(merged+"\n")); err != nil {
		slog.Warn("backup: key of the server not remembered", "err", err)
	}
}

// repository returns the address restic takes, extra arguments and the
// environment for a job. cleanup removes what was put in place for it.
func (m *Manager) repository(job Job) (repo string, args, env []string, cleanup func(), err error) {
	cleanup = func() {}
	d := job.Destination
	if plainCopy(d.Type) {
		return "", nil, nil, cleanup, invalid("plain copies have no restic snapshots; restore files with the file manager")
	}
	cache := ""
	if volume := m.homeVolume(job); volume != "" {
		// restic's cache is large; it never goes into the RAM.
		cache = filepath.Join(m.mountRoot, volume, stateDir, "cache")
	}
	env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "RESTIC_PASSWORD=" + job.Password, "RESTIC_PROGRESS_FPS=1"}
	if cache != "" {
		env = append(env, "RESTIC_CACHE_DIR="+cache)
	} else {
		args = append(args, "--no-cache")
	}
	switch d.Type {
	case "usb", "smb":
		dir, release, err := m.holdTarget(job)
		if err != nil {
			return "", nil, nil, cleanup, err
		}
		cleanup = release
		repo = filepath.Join(dir, d.Folder)
	case "local":
		if !slices.ContainsFunc(m.mounted(), func(volume string) bool { return volume == d.Volume }) {
			return "", nil, nil, cleanup, fmt.Errorf("the volume %s of the backup is not mounted", d.Volume)
		}
		repo = filepath.Join(m.mountRoot, d.Volume, d.Folder)
	case "s3":
		repo = "s3:" + strings.TrimRight(d.Endpoint, "/") + "/" + d.Bucket
		if d.Prefix != "" {
			repo += "/" + strings.Trim(d.Prefix, "/")
		}
		env = append(env, "AWS_ACCESS_KEY_ID="+d.AccessKey, "AWS_SECRET_ACCESS_KEY="+d.SecretKey)
	case "rest":
		repo = "rest:" + d.URL
		env = append(env, "RESTIC_REST_USERNAME="+d.Username, "RESTIC_REST_PASSWORD="+d.Password)
	case "sftp":
		if _, err := m.PublicKey(); err != nil {
			return "", nil, nil, cleanup, err
		}
		// ssh wants its key in a file only root can read; /config is
		// asleep most of the time. Both lie in the RAM while the job runs.
		// Each call has a folder of its own: a look at the snapshots while
		// the job runs must not take the key away from the backup.
		if err := os.MkdirAll(m.workDir, 0o700); err != nil {
			return "", nil, nil, cleanup, err
		}
		dir, err := os.MkdirTemp(m.workDir, job.ID+"-")
		if err != nil {
			return "", nil, nil, cleanup, err
		}
		cleanup = func() { os.RemoveAll(dir) }
		key, err := m.store.Read(keyFile)
		if err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		// A known_hosts that cannot be read is not an empty one: ssh would
		// take any server under the name.
		hosts, err := m.store.Read(hostFile)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "key"), key, 0o600); err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "known_hosts"), hosts, 0o600); err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		cleanup = func() {
			// A server seen for the first time is remembered: from then
			// on only that one is accepted under its name.
			if now, err := os.ReadFile(filepath.Join(dir, "known_hosts")); err == nil && string(now) != string(hosts) && len(now) > 0 {
				m.rememberHosts(now)
			}
			os.RemoveAll(dir)
		}
		repo = fmt.Sprintf("sftp:%s@%s:%s", d.User, d.Host, d.Path)
		command := fmt.Sprintf("ssh -i %s -o UserKnownHostsFile=%s -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ServerAliveInterval=30 -p %d %s@%s -s sftp",
			filepath.Join(dir, "key"), filepath.Join(dir, "known_hosts"), d.Port, d.User, d.Host)
		args = append(args, "-o", "sftp.command="+command)
	}
	env = append(env, "RESTIC_REPOSITORY="+repo)
	return repo, args, env, cleanup, nil
}

func (m *Manager) mounted() []string {
	var volumes []string
	for _, source := range m.sources() {
		if !slices.Contains(volumes, source.Volume) {
			volumes = append(volumes, source.Volume)
		}
	}
	// A volume without shares can be the place a backup goes to.
	entries, _ := os.ReadDir(m.mountRoot)
	mounts, _ := os.ReadFile(m.mountsFile)
	for _, entry := range entries {
		if strings.Contains(string(mounts), " "+filepath.Join(m.mountRoot, entry.Name())+" ") && !slices.Contains(volumes, entry.Name()) {
			volumes = append(volumes, entry.Name())
		}
	}
	return volumes
}

// restic runs the program and hands each line it prints to line. It
// returns what it wrote as errors.
func (m *Manager) run(ctx context.Context, env, args []string, line func(string)) (string, error) {
	return m.runFiles(ctx, env, args, line, nil)
}
func (m *Manager) runFiles(ctx context.Context, env, args []string, line func(string), files []*os.File) (string, error) {
	cmd := exec.CommandContext(ctx, m.restic, args...)
	cmd.Env = env
	cmd.ExtraFiles = files
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var problems strings.Builder
	cmd.Stderr = &limited{w: &problems, left: 8000}
	// A cancelled job ends restic and what it started (ssh, for SFTP):
	// they get a group of their own, and the signal goes to all of it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 20 * time.Second
	if err := cmd.Start(); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		if line != nil {
			line(scanner.Text())
		}
	}
	_, _ = io.Copy(io.Discard, stdout) // draining the body for connection reuse only
	err = cmd.Wait()
	return strings.TrimSpace(problems.String()), err
}

// limited keeps the first bytes of what is written to it.
type limited struct {
	w    io.Writer
	left int
}

func (l *limited) Write(p []byte) (int, error) {
	if take := min(len(p), l.left); take > 0 {
		_, _ = l.w.Write(p[:take]) // a sink for the first lines of output; losing them loses nothing
		l.left -= take
	}
	return len(p), nil
}

// lastLines returns the end of what restic said, without the password
// should it ever echo its environment.
func lastLines(text, secret string) string {
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "…")
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}
