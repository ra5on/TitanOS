package personalagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const BrokerSocket = "/run/titanos-personal-agent/broker.sock"
const WorkerUser = "_titanos-agent"

// Transactions carry opaque leases. Cookies, account names, text, file paths and model keys
// never cross this process boundary; the broker binds every lease to an authorized native job.
type Command struct {
	Op    string `json:"op"`
	Lease string `json:"lease,omitempty"`
}
type Reply struct {
	Lease string `json:"lease,omitempty"`
	Apply bool   `json:"apply,omitempty"`
	OK    bool   `json:"ok"`
}

func exchange(ctx context.Context, socket string, cmd Command) (Reply, error) {
	var out Reply
	conn, err := new(net.Dialer).DialContext(ctx, "unix", socket)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := conn.SetDeadline(time.Now().Add(12 * time.Minute)); err != nil {
		return out, err
	}
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return out, err
	}
	if err := json.NewDecoder(io.LimitReader(conn, 2048)).Decode(&out); err != nil {
		return out, err
	}
	if !out.OK {
		return out, errors.New("agent broker refused operation")
	}
	return out, nil
}

// RunWorker schedules independent jobs without a browser. One blocked mailbox does not block another account.
func RunWorker(ctx context.Context, socket string) error {
	if os.Geteuid() == 0 {
		return errors.New("agent worker needs an unprivileged user")
	}
	var group sync.WaitGroup
	group.Go(func() {
		for ctx.Err() == nil {
			_, _ = exchange(ctx, socket, Command{Op: "watch"})
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	})
	for range 2 {
		group.Go(func() { workerLoop(ctx, socket) })
	}
	group.Wait()
	return nil
}
func workerLoop(ctx context.Context, socket string) {
	for ctx.Err() == nil {
		job, err := exchange(ctx, socket, Command{Op: "claim"})
		if err == nil && job.Lease != "" {
			next, err := exchange(ctx, socket, Command{Op: "prepare", Lease: job.Lease})
			if err == nil && next.Apply {
				_, _ = exchange(ctx, socket, Command{Op: "apply", Lease: job.Lease})
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1200 * time.Millisecond):
		}
	}
}
