package lxc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config returns the LXC configuration of a container. forCreate leaves
// out what lxc-create sets itself or what needs the root filesystem
// (root filesystem, folders, network); it only carries the ID mapping and
// the common settings the download template unpacks with.
func (m *Manager) Config(c Container, forCreate bool) string {
	var b strings.Builder
	line := func(key, value string) { fmt.Fprintf(&b, "%s = %s\n", key, value) }
	b.WriteString("# Written by TitanOS from /config/lxc/" + c.Name + ".json before every start.\n")
	b.WriteString("# Changes made here are overwritten; change the container in the web UI.\n")
	line("lxc.include", m.include(c.Distribution, "common"))
	if !c.Privileged {
		line("lxc.include", m.include(c.Distribution, "userns"))
		// Root of the container is an unprivileged ID of the NAS. Its group
		// 100 ("users") is the group of the NAS all accounts and shares
		// belong to, so that folders of shares are writable inside.
		gid := m.usersGID
		line("lxc.idmap", fmt.Sprintf("u 0 %d %d", idmapBase, idmapSize))
		line("lxc.idmap", fmt.Sprintf("g 0 %d 100", idmapBase))
		line("lxc.idmap", fmt.Sprintf("g 100 %d 1", gid))
		line("lxc.idmap", fmt.Sprintf("g 101 %d %d", idmapBase+101, idmapSize-101))
	}
	line("lxc.arch", archKey(c.Arch))
	if _, err := os.Stat(m.apparmorParser); err != nil {
		// Without the AppArmor tools LXC cannot generate a profile and
		// would refuse to start on a kernel with AppArmor switched on.
		line("lxc.apparmor.profile", "unconfined")
	} else {
		line("lxc.apparmor.profile", "generated")
		line("lxc.apparmor.allow_nesting", "0")
	}
	if forCreate {
		return b.String()
	}
	line("lxc.uts.name", c.Name)
	line("lxc.rootfs.path", "dir:"+m.rootfs(c))
	if c.MemoryMiB > 0 {
		line("lxc.cgroup2.memory.max", fmt.Sprint(int64(c.MemoryMiB)<<20))
	}
	if c.CPUs > 0 {
		line("lxc.cgroup2.cpu.max", fmt.Sprintf("%d 100000", c.CPUs*100000))
	}
	if device, ok := m.bridgeOf(c); ok && c.Network != "none" {
		line("lxc.net.0.type", "veth")
		line("lxc.net.0.link", device)
		line("lxc.net.0.flags", "up")
		line("lxc.net.0.name", "eth0")
		line("lxc.net.0.hwaddr", c.MAC)
	} else {
		line("lxc.net.0.type", "empty")
	}
	for _, mount := range c.Mounts {
		options := "bind,create=dir"
		if mount.ReadOnly {
			options += ",ro"
		}
		// The place in the container is relative to its root filesystem.
		// LXC reads the entry like a line of fstab (getmntent), so blanks
		// and backslashes in a folder name ("My Photos") are escaped.
		line("lxc.mount.entry", fmt.Sprintf("%s %s none %s 0 0", mountEscaper.Replace(mount.Path), mountEscaper.Replace(strings.TrimPrefix(mount.Target, "/")), options))
	}
	return b.String()
}

// mountEscaper escapes the characters getmntent splits fields at the
// way fstab does: as octal escapes it decodes again.
var mountEscaper = strings.NewReplacer("\\", `\134`, " ", `\040`, "\t", `\011`, "\n", `\012`)

// include picks the snippet of the LXC package for a distribution, or the
// generic one.
func (m *Manager) include(distribution, kind string) string {
	specific := filepath.Join(m.templateDir, distribution+"."+kind+".conf")
	if _, err := os.Stat(specific); err == nil && imageRE.MatchString(distribution) {
		return specific
	}
	return filepath.Join(m.templateDir, kind+".conf")
}

func archKey(arch string) string {
	switch arch {
	case "amd64":
		return "linux64"
	case "i386":
		return "linux32"
	}
	return arch
}
