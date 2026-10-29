package firewall

import (
	"slices"
	"strconv"
)

// PortSet is one protocol with its ports ("445", "8000-8100"). Protocol
// icmp stands for echo requests (ping) of IPv4 and IPv6 and has no ports.
type PortSet struct {
	Protocol string   `json:"protocol"`
	Ports    []string `json:"ports"`
}

// Service is a line of the zone matrix.
type Service struct {
	ID      string    `json:"id"`
	Ports   []PortSet `json:"ports"`
	Default Zones     `json:"default"`
}

// WebPorts are the ports titand serves the web UI on; 0 means the usual
// port (80, 443), HTTP -1 that plain HTTP is off.
type WebPorts struct{ HTTP, HTTPS int }

func (w *WebPorts) or() WebPorts {
	out := WebPorts{HTTP: 80, HTTPS: 443}
	if w != nil {
		if w.HTTPS > 0 {
			out.HTTPS = w.HTTPS
		}
		if w.HTTP != 0 {
			out.HTTP = w.HTTP
		}
	}
	return out
}

// NFS needs fixed ports for mountd, statd and lockd; the image pins them
// (/etc/nfs.conf.d/titanos-ports.conf, /etc/modprobe.d/titanos-nfs.conf).
const (
	nfsMountd = "20048"
	nfsStatd  = "32765-32766"
	nfsLockd  = "32768"
)

// Services lists every line of the matrix. web nil means 80 and 443.
func Services(web *WebPorts) []Service {
	w := web.or()
	webui := []string{strconv.Itoa(w.HTTPS)}
	if w.HTTP > 0 {
		webui = append([]string{strconv.Itoa(w.HTTP)}, webui...)
	}
	nfs := []string{"111", "2049", nfsMountd, nfsStatd, nfsLockd}
	return []Service{
		{ID: "webui", Ports: []PortSet{{"tcp", webui}}, Default: local},
		// App HTTPS names share the port of the web UI; titand itself tells
		// both apart by name (see AllowHTTP).
		{ID: "apps", Ports: []PortSet{{"tcp", []string{strconv.Itoa(w.HTTPS)}}}, Default: local},
		{ID: "smb", Ports: []PortSet{{"tcp", []string{"139", "445"}}, {"udp", []string{"137-138"}}}, Default: local},
		{ID: "nfs", Ports: []PortSet{{"tcp", nfs}, {"udp", nfs}}, Default: local},
		{ID: "ssh", Ports: []PortSet{{"tcp", []string{"22"}}}, Default: local},
		// mDNS (avahi), WS-Discovery (wsdd2) and LLMNR (resolved) only make sense
		// where the NAS can be seen: in the home network.
		{ID: "discovery", Ports: []PortSet{{"udp", []string{"3702", "5353", "5355"}}, {"tcp", []string{"5355", "5357"}}}, Default: homeOnly},
		{ID: "ping", Ports: []PortSet{{"icmp", nil}}, Default: local},
		// NUT for other devices on the same UPS, the receiving side of
		// "Another TitanOS" (listens on Tailscale addresses only).
		{ID: "ups", Ports: []PortSet{{"tcp", []string{"3493"}}}, Default: local},
		{ID: "backuptarget", Ports: []PortSet{{"tcp", []string{"8750"}}}, Default: local},
		// The encrypted tunnels of Tailscale and NetBird. Their peers come
		// from the internet; the VPN authenticates them itself.
		{ID: "vpn", Ports: []PortSet{{"udp", []string{"41641", "51820"}}}, Default: Zones{Home: true, VPN: true, Internet: true}},
	}
}

func serviceByID(services []Service, id string) (Service, bool) {
	i := slices.IndexFunc(services, func(s Service) bool { return s.ID == id })
	if i < 0 {
		return Service{}, false
	}
	return services[i], true
}
