package appproxy

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAvahiBusReal publishes names over a real avahi (only with
// TITANOS_AVAHI_REAL=<address>, e.g. in a container with dbus and
// avahi-daemon of trixie): two names on the NAS's own address – which
// failed with "Local name collision" through /etc/avahi/hosts – then a
// changed set, then none. The names are checked from outside the test
// with avahi-resolve.
func TestAvahiBusReal(t *testing.T) {
	ip := os.Getenv("TITANOS_AVAHI_REAL")
	if ip == "" {
		t.Skip("set TITANOS_AVAHI_REAL to the address to publish on")
	}
	ctx := context.Background()
	bus := &avahiBus{}
	names := strings.Fields(os.Getenv("TITANOS_AVAHI_NAMES"))
	if err := bus.Publish(ctx, ip, names); err != nil {
		t.Fatal(err)
	}
	if !bus.Alive(ctx) {
		t.Fatal("group not alive after publishing")
	}
	// Publishing the same set again replaces it in the same group.
	if err := bus.Publish(ctx, ip, names); err != nil {
		t.Fatal(err)
	}
	t.Logf("published %v on %s; holding", names, ip)
	if hold := os.Getenv("TITANOS_AVAHI_HOLD"); hold != "" {
		f, _ := os.Create(hold)
		f.Close()
		// The caller resolves the names, then removes the file.
		for {
			if _, err := os.Stat(hold); os.IsNotExist(err) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
