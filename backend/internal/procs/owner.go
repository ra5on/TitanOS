package procs

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	dockerScope = regexp.MustCompile(`(?:^|/)docker-([0-9a-f]{64})\.scope(?:/|$)`)
	dockerPlain = regexp.MustCompile(`(?:^|/)docker/([0-9a-f]{64})(?:/|$)`)
	machine     = regexp.MustCompile(`(?:^|/)machine-qemu\\x2d(\d+)\\x2d([^/]+)\.scope(?:/|$)`)
	lxcPayload  = regexp.MustCompile(`(?:^|/)lxc\.(?:payload|monitor)\.([^/]+)(?:/|$)`)
	lxcUnit     = regexp.MustCompile(`(?:^|/)titanos-lxc@([^/]+)\.service(?:/|$)`)
	service     = regexp.MustCompile(`^/system\.slice/(?:[^/]+\.slice/)*([^/]+)\.service(?:/|$)`)
)

// OwnerOf names what a cgroup path (unified hierarchy) belongs to.
func OwnerOf(cgroup string) Owner {
	if m := dockerScope.FindStringSubmatch(cgroup); m != nil {
		return Owner{Kind: "docker", ID: m[1]}
	}
	if m := dockerPlain.FindStringSubmatch(cgroup); m != nil {
		return Owner{Kind: "docker", ID: m[1]}
	}
	if m := machine.FindStringSubmatch(cgroup); m != nil {
		// libvirt: machine-qemu\x2d<id>\x2d<name>.scope; systemd escapes
		// "-" in the name as \x2d too (and may shorten long names).
		return Owner{Kind: "vm", ID: m[1], Name: unescapeUnit(m[2])}
	}
	if m := lxcPayload.FindStringSubmatch(cgroup); m != nil {
		return Owner{Kind: "lxc", Name: m[1]}
	}
	if m := lxcUnit.FindStringSubmatch(cgroup); m != nil {
		return Owner{Kind: "lxc", Name: unescapeUnit(m[1])}
	}
	if m := service.FindStringSubmatch(cgroup); m != nil {
		return Owner{Kind: "service", Name: unescapeUnit(m[1])}
	}
	return Owner{}
}

// unescapeUnit reverses systemd's \xNN escapes in unit names.
func unescapeUnit(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
