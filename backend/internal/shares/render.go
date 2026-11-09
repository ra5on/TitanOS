package shares

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The files Samba and NFS read, written from the shares (split from
// shares.go, PLAN 75-12).

// renderSMBPasswd writes the accounts in the format of Samba's smbpasswd
// file, which pdbedit can import. Accounts without an SMB hash are left out;
// they get one at their next login.
func renderSMBPasswd(users []auth.User) string {
	var b strings.Builder
	for _, user := range users {
		if user.SMBHash == "" {
			continue
		}
		// The time the password was last changed must not be zero: Samba
		// reads that as "must change the password before logging in".
		changed := max(user.Created.Unix(), 1)
		flags := "[U          ]"
		if user.Disabled {
			flags = "[DU         ]" // account disabled: Samba refuses the login
		}
		fmt.Fprintf(&b, "%s:%d:%s:%s:%s:LCT-%08X:\n", user.Name, user.UID, strings.Repeat("X", 32), user.SMBHash, flags, changed)
	}
	return b.String()
}

// accessLists returns who may use a share at all and who may write.
func accessLists(share Share) (all, writers []string) {
	for user, level := range share.Access {
		all = append(all, user)
		if level == ReadWrite {
			writers = append(writers, user)
		}
	}
	sort.Strings(all)
	sort.Strings(writers)
	return all, writers
}

// offeredOverSMB reports whether anyone could open the share. Samba treats
// an empty list of valid users as "everyone", so a share without any
// access must not be written to smb.conf at all.
func offeredOverSMB(share Share) bool {
	return share.Guest || len(share.Access) > 0
}

// renderSMBConf writes smb.conf. shadow returns the folder with the
// @GMT snapshots of a share's volume, or "" if it has none.
func renderSMBConf(shares []Share, path func(Share) string, shadow func(Share) string) string {
	guests := slices.ContainsFunc(shares, func(s Share) bool { return s.SMB && s.Guest })
	var b strings.Builder
	b.WriteString("# Written by titand from /config/shares/shares.json. Do not edit: it is\n# replaced whenever shares or accounts change.\n\n[global]\n")
	global := [][2]string{
		{"server role", "standalone server"},
		{"workgroup", "WORKGROUP"},
		{"server string", "TitanOS"},
		{"security", "user"},
		{"passdb backend", "tdbsam"},
		{"server min protocol", "SMB2"},
		{"disable netbios", "yes"},
		{"smb ports", "445"},
		{"logging", "systemd"},
		// auth_audit:2 logs failed logins (with the client's address) for
		// the firewall's automatic blocking; successes stay unlogged.
		{"log level", "1 auth_audit:2"},
		{"load printers", "no"},
		{"printing", "bsd"},
		{"printcap name", "/dev/null"},
		{"disable spoolss", "yes"},
	}
	if guests {
		global = append(global, [2]string{"map to guest", "bad user"}, [2]string{"guest account", "nobody"})
	} else {
		global = append(global, [2]string{"map to guest", "never"})
	}
	for _, option := range global {
		fmt.Fprintf(&b, "   %s = %s\n", option[0], option[1])
	}

	for _, share := range shares {
		if !share.SMB || !offeredOverSMB(share) {
			continue
		}
		all, writers := accessLists(share)
		dir := path(share)
		if share.PerUser {
			// Each account lands in its own folder and sees no other.
			dir += "/%u"
		}
		options := [][2]string{
			{"path", dir},
			{"browseable", yesNo(share.Browseable)},
			// Read-only for everyone except the write list.
			{"read only", "yes"},
			{"guest ok", yesNo(share.Guest)},
			{"force group", "users"},
			{"create mask", "0660"},
			{"force create mode", "0660"},
			{"directory mask", "2770"},
			{"force directory mode", "2770"},
		}
		if !share.Guest {
			options = append(options, [2]string{"valid users", strings.Join(all, " ")})
		}
		// A share locked by the ransomware guard has no writers at all.
		if len(writers) > 0 && share.GuardLocked == nil {
			options = append(options, [2]string{"write list", strings.Join(writers, " ")})
		}
		var vfs []string
		if dir := shadowOf(share, shadow); dir != "" {
			// The snapshots hold the whole volume; basedir maps the share
			// path below the volume to the same path in each snapshot.
			vfs = append(vfs, "shadow_copy2")
			options = append(options,
				[2]string{"shadow:snapdir", dir},
				[2]string{"shadow:basedir", filepath.Dir(dir)},
				[2]string{"shadow:format", shadowFormat},
				[2]string{"shadow:localtime", "no"},
				[2]string{"shadow:sort", "desc"},
			)
		}
		if share.Guard {
			vfs = append(vfs, "full_audit")
			options = append(options,
				[2]string{"full_audit:prefix", "%u|%S"},
				[2]string{"full_audit:success", "renameat unlinkat"},
				[2]string{"full_audit:failure", "none"},
				[2]string{"full_audit:facility", "LOCAL5"},
				[2]string{"full_audit:priority", "NOTICE"},
				[2]string{"full_audit:syslog", "true"},
			)
		}
		// Mac support on every share (PLAN 69, owner decision 7.11.2026):
		// Finder data goes into extended attributes instead of "._name"
		// companion files, which macOS writes on shares without streams;
		// existing ones are converted when a Mac opens them.
		vfs = append(vfs, "catia", "fruit", "streams_xattr")
		options = append(options,
			[2]string{"fruit:metadata", "stream"},
			[2]string{"fruit:delete_empty_adfiles", "yes"},
			[2]string{"fruit:wipe_intentionally_left_blank_rfork", "yes"},
		)
		if share.TimeMachine.Enabled {
			options = append(options, [2]string{"fruit:time machine", "yes"})
			if share.TimeMachine.SizeGB > 0 {
				options = append(options, [2]string{"fruit:time machine max size", fmt.Sprintf("%dG", share.TimeMachine.SizeGB)})
			}
		}
		if share.Recycle.Enabled {
			vfs = append(vfs, "recycle")
			options = append(options,
				[2]string{"recycle:repository", recycleDir + "/%U"},
				[2]string{"recycle:keeptree", "yes"},
				[2]string{"recycle:versions", "yes"},
				// The time of deletion decides when a file is removed for good.
				[2]string{"recycle:touch_mtime", "yes"},
				[2]string{"recycle:directory_mode", "0770"},
			)
		}
		if len(vfs) > 0 {
			options = append(options, [2]string{"vfs objects", strings.Join(vfs, " ")})
		}
		fmt.Fprintf(&b, "\n[%s]\n", share.Name)
		for _, option := range options {
			fmt.Fprintf(&b, "   %s = %s\n", option[0], option[1])
		}
	}
	return b.String()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// renderExports writes /etc/exports. Every NFS share is bind-mounted into
// root (see syncBinds), which becomes the root of the NFSv4 tree: a client
// mounts "nas:/<share>". Without it NFSv4 would start at "/", an overlay in
// RAM that cannot be exported, and it could not step from one filesystem
// into a folder of another.
func renderExports(shares []Share, root string) string {
	var b strings.Builder
	b.WriteString("# Written by titand from /config/shares/shares.json. Do not edit.\n")
	var clients []string
	for _, share := range shares {
		if share.NFS.Enabled {
			for _, client := range share.NFS.Clients {
				if !slices.Contains(clients, client) {
					clients = append(clients, client)
				}
			}
		}
	}
	if len(clients) == 0 {
		return b.String()
	}
	sort.Strings(clients)
	// Read-only, and only a way in: it holds nothing but the mount points.
	b.WriteString(exportPath(root))
	for _, client := range clients {
		fmt.Fprintf(&b, " %s(ro,fsid=0,sync,no_subtree_check,all_squash)", client)
	}
	b.WriteString("\n")
	for _, share := range shares {
		if !share.NFS.Enabled {
			continue
		}
		b.WriteString(exportPath(filepath.Join(root, share.Name)))
		for _, client := range share.NFS.Clients {
			// Every NFS client writes as nobody:users, so that files fit the
			// share's group whatever user numbers the client has.
			mode := ReadWrite
			if share.NFS.Mode == ReadOnly || share.GuardLocked != nil {
				mode = ReadOnly
			}
			fmt.Fprintf(&b, " %s(%s,nohide,sync,no_subtree_check,all_squash,anonuid=65534,anongid=%d)", client, mode, UsersGID)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// exportPath writes a path for /etc/exports, where a blank would end it:
// exportfs reads blanks and backslashes as octal escapes, like fstab.
func exportPath(path string) string {
	return strings.NewReplacer("\\", `\134`, " ", `\040`, "\t", `\011`).Replace(path)
}
