package diagnose

import (
	"strings"
	"testing"
)

// The private keys in /config/tls (the TitanOS certificate authority, the
// key of app windows, the web UI's certificate), the accounts and T-Mail
// (settings with the mail passwords and their key) never go into a bundle.
func TestBundleLeavesOutKeys(t *testing.T) {
	for _, name := range configFiles {
		for _, secret := range []string{"tls/", "users/", "cluster/", "docker/app-access", "tmail/"} {
			if strings.HasPrefix(name, secret) {
				t.Errorf("%s is in the diagnostic bundle", name)
			}
		}
	}
}
