// Package naming validates user-chosen storage and backup names without changing
// their spelling. Names are persisted and passed to filesystems exactly as given.
package naming

import "regexp"

var volume = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,11}$`)
var backup = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,30}$`)

// ValidVolume respects the 12-byte XFS label limit and mdadm's safe name syntax.
func ValidVolume(name string) bool { return volume.MatchString(name) }

// ValidBackup validates job, destination and sending-NAS names.
func ValidBackup(name string) bool { return backup.MatchString(name) }
