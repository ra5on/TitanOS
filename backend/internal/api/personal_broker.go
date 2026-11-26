package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"golang.org/x/sys/unix"
)

func personalPeer(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return cred.Uid, nil
}
func personalListen(path string, uid int) (*net.UnixListener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0711); err != nil {
		return nil, err
	}
	// A stale socket is expected after SIGKILL. Never remove a running broker or a regular file.
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("unexpected Agent socket path")
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = c.Close()
			return nil, errors.New("agent broker already running")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err == nil && os.Geteuid() != uid {
		err = os.Chown(path, uid, -1)
	}
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// ServePersonalBroker has no HTTP route. The service may request only server-selected work via an owner-only Unix socket.
func (s *Server) ServePersonalBroker(ctx context.Context, path string, uid int) error {
	l, err := personalListen(path, uid)
	if err != nil {
		return err
	}
	defer func() { _ = l.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stop()
	var work sync.WaitGroup
	defer work.Wait()
	slots := make(chan struct{}, 4)
	for {
		conn, err := l.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		who, err := personalPeer(conn)
		if err != nil || who != uint32(uid) {
			_ = conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		work.Go(func() {
			defer func() { <-slots }()
			defer func() { _ = conn.Close() }()
			s.personalCommand(ctx, conn)
		})
	}
}
func (s *Server) personalCommand(parent context.Context, conn *net.UnixConn) {
	if conn.SetReadDeadline(time.Now().Add(5*time.Second)) != nil {
		return
	}
	var cmd pa.Command
	if json.NewDecoder(io.LimitReader(conn, 512)).Decode(&cmd) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	var reply pa.Reply
	var err error
	switch cmd.Op {
	case "watch":
		err = s.personalPollWatches(ctx)
	case "claim":
		reply.Lease, err = s.personalClaim()
	case "prepare", "apply":
		if len(cmd.Lease) != 32 {
			return
		}
		reply.Apply, err = s.personalStep(ctx, cmd.Lease, cmd.Op == "apply")
	default:
		return
	}
	reply.OK = err == nil
	if conn.SetWriteDeadline(time.Now().Add(5*time.Second)) == nil {
		_ = json.NewEncoder(conn).Encode(reply)
	}
}
