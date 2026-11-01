package cluster

import "strings"

// Protocol is the level of the cluster protocol: how nodes sign, join and
// pass on requests. It goes up only when that changes in a way older nodes
// cannot follow, not with every version. Nodes of the same level manage
// each other fully, whatever their versions; versions before the level was
// sent speak level 1.
const Protocol = 1

// Compatible reports whether a node announcing level p (0: none) speaks
// the protocol of this one.
func Compatible(p int) bool {
	if p == 0 {
		p = 1
	}
	return p == Protocol
}

// limited reports whether a request (path after /api/v1/) may go to a
// node of another protocol level: updating it, looking at it and its
// logs, and switching it off or on again. These have stayed the same.
func limited(method, path string) bool {
	path = strings.Trim(path, "/")
	read := method == "GET" || method == "HEAD"
	switch {
	case path == "update" || strings.HasPrefix(path, "update/"):
		return true
	case path == "system/power" || path == "system/power/now":
		return true
	case path == "logs" || strings.HasPrefix(path, "logs/"):
		return read
	case path == "system" || path == "notifications":
		return read
	}
	return false
}
