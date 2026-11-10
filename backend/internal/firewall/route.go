package firewall

import (
	"context"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// KernelRoute returns a RouteDevice that asks the kernel for the way to a
// private address ("ip route get", which follows the routing rules of
// Tailscale and NetBird) and keeps each answer for half a minute. Other
// addresses are told by their number, without a question.
func KernelRoute() func(netip.Addr) string {
	type answer struct {
		device string
		at     time.Time
	}
	var mu sync.Mutex
	known := map[netip.Addr]answer{}
	return func(addr netip.Addr) string {
		if !addr.IsPrivate() {
			return ""
		}
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		if a, ok := known[addr]; ok && now.Sub(a.at) < 30*time.Second {
			return a.device
		}
		if len(known) >= 1024 {
			clear(known)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, "ip", "-o", "route", "get", addr.String()).Output() // no answer: told by address
		device := routeDevice(string(out))
		known[addr] = answer{device, now}
		return device
	}
}

// routeDevice reads the interface from a line of "ip -o route get".
func routeDevice(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields[:max(0, len(fields)-1)] {
		if f == "dev" {
			return fields[i+1]
		}
	}
	return ""
}
