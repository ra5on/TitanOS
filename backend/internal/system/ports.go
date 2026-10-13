package system

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// ListeningPort is a port something on the NAS accepts traffic on.
type ListeningPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// ListeningPorts returns the TCP ports that are listened on and the UDP
// ports that are bound, for telling which ports are free for an app.
func ListeningPorts() []ListeningPort {
	seen := map[ListeningPort]bool{}
	for _, table := range []struct{ file, protocol, state string }{
		{"/proc/net/tcp", "tcp", "0A"}, {"/proc/net/tcp6", "tcp", "0A"},
		{"/proc/net/udp", "udp", ""}, {"/proc/net/udp6", "udp", ""},
	} {
		data, _ := os.ReadFile(table.file)
		for _, port := range parseSockets(string(data), table.state) {
			seen[ListeningPort{Port: port, Protocol: table.protocol}] = true
		}
	}
	ports := make([]ListeningPort, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Protocol < ports[j].Protocol
	})
	return ports
}

// parseSockets reads the local ports out of /proc/net/tcp or udp. With a
// state, only sockets in that state count (0A is listening).
func parseSockets(table, state string) []int {
	var ports []int
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		// sl local_address rem_address st …
		if len(fields) < 4 || (state != "" && fields[3] != state) {
			continue
		}
		at := strings.LastIndexByte(fields[1], ':')
		if at < 0 {
			continue
		}
		port, err := strconv.ParseUint(fields[1][at+1:], 16, 16)
		if err == nil && port > 0 {
			ports = append(ports, int(port))
		}
	}
	return ports
}

// Timezone returns the time zone of the NAS as an IANA name.
func Timezone() string {
	if zone := readTrimmed("/etc/timezone"); zone != "" {
		return zone
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, found := strings.Cut(target, "zoneinfo/"); found && zone != "" {
			return zone
		}
	}
	return "Etc/UTC"
}
