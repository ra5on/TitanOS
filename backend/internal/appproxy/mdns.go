package appproxy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

// The .local names of the apps go to avahi over its D-Bus interface, as
// address records without a reverse entry (AVAHI_PUBLISH_NO_REVERSE, like
// "avahi-publish -a -R"). /etc/avahi/hosts cannot do that: avahi adds a
// reverse entry "address → name" for every line there, and the NAS's own
// name already holds the one of its address – every app name on it failed
// with "Local name collision" and was never published (log of titan,
// 13.11.2026; reproduced with avahi 0.8-16 of trixie).

// namePublisher keeps a set of names on one address published over mDNS;
// tests replace it.
type namePublisher interface {
	// Publish makes ip and names the published set (replacing the one
	// before; no names publishes nothing).
	Publish(ctx context.Context, ip string, names []string) error
	// Alive tells whether the set published last is still held (avahi
	// forgets it when it restarts).
	Alive(ctx context.Context) bool
}

const (
	avahiService   = "org.freedesktop.Avahi"
	avahiServer    = "org.freedesktop.Avahi.Server"
	avahiGroup     = "org.freedesktop.Avahi.EntryGroup"
	avahiNoReverse = uint32(16) // AVAHI_PUBLISH_NO_REVERSE (avahi-common/defs.h)
	avahiUnspec    = int32(-1)  // AVAHI_IF_UNSPEC, AVAHI_PROTO_UNSPEC
	// Entry group states (avahi-common/defs.h).
	avahiGroupCollision = int32(3)
	avahiGroupFailure   = int32(4)
)

// ErrNameInUse means another device in the network announces one of the
// names: avahi gave up on the set.
var ErrNameInUse = errors.New("appproxy: a .local name is in use by another device")

// avahiBus publishes over the system bus. Its entry group belongs to its
// connection: the connection stays open while titand runs.
type avahiBus struct {
	mu    sync.Mutex
	conn  *dbus.Conn
	group dbus.ObjectPath
}

func (a *avahiBus) connect(ctx context.Context) error {
	if a.conn != nil && a.conn.Connected() {
		return nil
	}
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(context.WithoutCancel(ctx)))
	if err != nil {
		return fmt.Errorf("appproxy: system bus: %w", err)
	}
	a.conn, a.group = conn, ""
	return nil
}

// state is the state of the entry group; ok is false when avahi does not
// know the group (it restarted) or cannot be asked.
func (a *avahiBus) state(ctx context.Context) (int32, bool) {
	if a.conn == nil || a.group == "" || !a.conn.Connected() {
		return 0, false
	}
	var state int32
	if err := a.conn.Object(avahiService, a.group).CallWithContext(ctx, avahiGroup+".GetState", 0).Store(&state); err != nil {
		return 0, false
	}
	return state, true
}

func (a *avahiBus) Alive(ctx context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.state(ctx)
	return ok && state != avahiGroupCollision && state != avahiGroupFailure
}

func (a *avahiBus) Publish(ctx context.Context, ip string, names []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.connect(ctx); err != nil {
		return err
	}
	if _, ok := a.state(ctx); !ok {
		a.group = ""
	}
	if a.group == "" {
		var path dbus.ObjectPath
		if err := a.conn.Object(avahiService, "/").CallWithContext(ctx, avahiServer+".EntryGroupNew", 0).Store(&path); err != nil {
			return fmt.Errorf("appproxy: avahi entry group: %w", err)
		}
		a.group = path
	} else if err := a.conn.Object(avahiService, a.group).CallWithContext(ctx, avahiGroup+".Reset", 0).Err; err != nil {
		a.group = ""
		return fmt.Errorf("appproxy: avahi reset: %w", err)
	}
	if len(names) == 0 {
		return nil
	}
	group := a.conn.Object(avahiService, a.group)
	for _, name := range names {
		if err := group.CallWithContext(ctx, avahiGroup+".AddAddress", 0, avahiUnspec, avahiUnspec, avahiNoReverse, name, ip).Err; err != nil {
			return fmt.Errorf("appproxy: avahi address %s: %w", name, err)
		}
	}
	if err := group.CallWithContext(ctx, avahiGroup+".Commit", 0).Err; err != nil {
		return fmt.Errorf("appproxy: avahi commit: %w", err)
	}
	return nil
}
