// Package terminal gives administrators a root shell on the NAS, shown in
// the terminal panel of the web UI.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const settingsFile = "system/terminal.json"

var (
	// ErrOff means terminals were switched off in the settings.
	ErrOff = errors.New("terminal: switched off in the settings")
	// ErrUnavailable means this titand does not hand out a shell.
	ErrUnavailable = errors.New("terminal: no shell in development mode")
)

// Settings is /config/system/terminal.json.
type Settings struct {
	// Enabled allows shells on the NAS and in containers. It is on unless
	// someone switched it off.
	Enabled bool `json:"enabled"`
	// NoConfirm skips the password prompt before consoles and downloads of
	// stick backups. Off unless the owner chose it: a console is a root
	// shell, and without the prompt a stolen login cookie is enough.
	NoConfirm bool `json:"noConfirm,omitempty"`
}

// Manager knows whether terminals are allowed and starts shells.
type Manager struct {
	store *config.Store
	// Disabled refuses the shell on the NAS. Set in development mode, where
	// it would be a shell on the developer's machine.
	Disabled bool
	// command is what a session runs.
	command []string

	mu        sync.Mutex
	enabled   bool
	noConfirm bool
	// off is closed when terminals are switched off, to end running sessions.
	off chan struct{}
}

// NewManager returns a manager for the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, command: []string{"/bin/bash", "--login"}, enabled: true, off: make(chan struct{})}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	settings := Settings{Enabled: true}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled, m.noConfirm = settings.Enabled, settings.NoConfirm
	return nil
}

// ConfirmRequired reports whether consoles ask for the password again.
func (m *Manager) ConfirmRequired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.noConfirm
}

// SetConfirmRequired chooses whether consoles ask for the password again.
func (m *Manager) SetConfirmRequired(required bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.WriteJSON(settingsFile, Settings{Enabled: m.enabled, NoConfirm: !required}); err != nil {
		return err
	}
	m.noConfirm = !required
	return nil
}

// Enabled reports whether terminals are allowed.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// SetEnabled allows or forbids terminals. Forbidding ends running sessions.
func (m *Manager) SetEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.WriteJSON(settingsFile, Settings{Enabled: enabled, NoConfirm: m.noConfirm}); err != nil {
		return err
	}
	if m.enabled && !enabled {
		close(m.off)
		m.off = make(chan struct{})
	}
	m.enabled = enabled
	return nil
}

// Off returns a channel that is closed when terminals are switched off.
func (m *Manager) Off() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.off
}

// Session is a running shell behind a pseudo terminal.
type Session struct {
	master *os.File
	cmd    *exec.Cmd
	once   sync.Once
}

// Shell starts a root shell on the NAS.
func (m *Manager) Shell(ctx context.Context) (*Session, error) {
	if m.Disabled {
		return nil, ErrUnavailable
	}
	if !m.Enabled() {
		return nil, ErrOff
	}
	return start(m.command)
}

// Start runs a command behind a pseudo terminal, like the shell: the
// consoles of system containers (lxc-attach, lxc-console) use it.
func Start(command []string) (*Session, error) {
	if len(command) == 0 {
		return nil, ErrUnavailable
	}
	return start(command)
}

func start(command []string) (*Session, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Session, error) {
		master.Close()
		return nil, err
	}
	// Unlock the other end of the pseudo terminal and find out its name.
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return fail(err)
	}
	number, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return fail(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fail(err)
	}
	defer slave.Close()
	unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 80, Row: 24})

	// No timeout on purpose: the shell lives as long as its console. The
	// console ends it (Session.Close) after 15 minutes without input.
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.Env = []string{
		"TERM=xterm-256color", "LANG=C.UTF-8", "HOME=" + homeDir(), "SHELL=" + command[0],
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	cmd.Dir = homeDir()
	// A session of its own with the pseudo terminal as its terminal, so
	// that Ctrl+C and the like reach what runs in the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	return &Session{master: master, cmd: cmd}, nil
}

// homeDir is where a shell starts: the home of whoever titand runs as.
func homeDir() string {
	// A service has no HOME in its environment; ask the account database.
	if account, err := user.Current(); err == nil {
		if info, err := os.Stat(account.HomeDir); err == nil && info.IsDir() {
			return account.HomeDir
		}
	}
	return "/"
}

func (s *Session) Read(p []byte) (int, error) {
	n, err := s.master.Read(p)
	// Linux reports the end of the shell as an I/O error on the terminal.
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

func (s *Session) Write(p []byte) (int, error) { return s.master.Write(p) }

// Resize tells the shell the size of the terminal.
func (s *Session) Resize(cols, rows int) {
	if cols > 0 && rows > 0 && cols <= 1000 && rows <= 1000 {
		unix.IoctlSetWinsize(int(s.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	}
}

// Close ends the shell and everything started in it.
func (s *Session) Close() error {
	s.once.Do(func() {
		// The whole session gets the hangup a closed terminal window sends.
		syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
		s.master.Close()
		go func() {
			if s.cmd.Wait() != nil {
				// Whatever ignored the hangup.
				syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
	})
	return nil
}
