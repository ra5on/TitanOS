package config

import "path"

// NotBackedUp are the files of /config (slash separated, globs as in
// path.Match) that never leave the NAS in a backup: the TitanOS
// certificate authority (its key and its certificate, which belong
// together), the leaf certificates it issued with their keys, and the key
// that signs the passes of app windows. Anyone holding the authority's
// key could vouch for the NAS's names on every device that trusts it, and
// backups can be downloaded or lie on other machines. After a restore a
// new authority is made on first need and devices install it again.
var NotBackedUp = []string{"tls/ca-key.pem", "tls/ca-cert.pem", "tls/ca-leaf-*.pem", "tls/app-gate.key"}

// LeftOutOfBackups tells whether a file of /config is one of NotBackedUp.
func LeftOutOfBackups(name string) bool {
	for _, pattern := range NotBackedUp {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}
