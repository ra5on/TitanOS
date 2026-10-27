package sshd

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestParseLogin(t *testing.T) {
	for line, want := range map[string]Login{
		"Accepted publickey for alex from 192.168.1.5 port 50122 ssh2: ED25519 SHA256:AbC+/12xyz":                              {User: "alex", IP: "192.168.1.5", KeyType: "ED25519", Fingerprint: "SHA256:AbC+/12xyz"},
		"Accepted publickey for bo from fd7a:115c:a1e0::5 port 22 ssh2: RSA SHA256:q1w2e3":                                     {User: "bo", IP: "fd7a:115c:a1e0::5", KeyType: "RSA", Fingerprint: "SHA256:q1w2e3"},
		"Accepted publickey for alex from 10.0.0.2 port 1 ssh2: ED25519-CERT SHA256:cert ID x (serial 1) CA ED25519 SHA256:ca": {User: "alex", IP: "10.0.0.2", KeyType: "ED25519-CERT", Fingerprint: "SHA256:cert"},
		"User alex from 203.0.113.7 not allowed because not listed in AllowUsers":                                              {User: "alex", IP: "203.0.113.7", Blocked: true},
	} {
		got, ok := parseLogin(line)
		if !ok || got != want {
			t.Errorf("%q: %+v %v", line, got, ok)
		}
	}
	for _, line := range []string{
		"Failed publickey for alex from 1.2.3.4 port 5 ssh2: ED25519 SHA256:x",
		"Accepted password for alex from 1.2.3.4 port 5 ssh2",
		"Connection closed by 1.2.3.4 port 5 [preauth]",
		"pam_unix(sshd:session): session opened for user alex",
	} {
		if _, ok := parseLogin(line); ok {
			t.Errorf("%q counted as a login", line)
		}
	}
}

func TestReadLogins(t *testing.T) {
	line := userKey(t)
	key, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(line))
	fingerprint := ssh.FingerprintSHA256(key)
	users := func() []auth.User {
		return []auth.User{
			{Name: "alex", Role: auth.RoleOwner, SSHKeys: []string{line}},
			{Name: "gast", Role: auth.RoleUser},
		}
	}
	journal := strings.Join([]string{
		`{"MESSAGE":"Accepted publickey for alex from 192.168.1.5 port 50122 ssh2: ED25519 ` + fingerprint + `","__REALTIME_TIMESTAMP":"1761552000000000"}`,
		`{"MESSAGE":"Accepted publickey for alex from 192.168.1.5 port 50123 ssh2: ED25519 SHA256:other","__REALTIME_TIMESTAMP":"1761552001000000"}`,
		`{"MESSAGE":"User alex from 203.0.113.7 not allowed because not listed in AllowUsers"}`,
		// Probes of names without SSH access are left out.
		`{"MESSAGE":"User daemon from 203.0.113.7 not allowed because not listed in AllowUsers"}`,
		`{"MESSAGE":"User gast from 203.0.113.7 not allowed because not listed in AllowUsers"}`,
		`{"MESSAGE":"Connection closed by 203.0.113.7 port 4 [preauth]"}`,
		`not json`,
		// Not valid UTF-8: journald hands out bytes.
		`{"MESSAGE":[85,115,101,114,32,97,108,101,120,32,102,114,111,109,32,49,46,50,46,51,46,52,32,110,111,116,32,97,108,108,111,119,101,100,32,98,101,99,97,117,115,101,32,110,111,116,32,108,105,115,116,101,100,32,105,110,32,65,108,108,111,119,85,115,101,114,115,255]}`,
	}, "\n")
	var got []Login
	if err := readLogins(strings.NewReader(journal), users, func(l Login) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("logins: %+v", got)
	}
	if got[0].Comment != "test" || got[0].Fingerprint != fingerprint || !got[0].At.Equal(time.UnixMicro(1761552000000000)) || got[0].Target() != "ED25519 "+fingerprint+" (test)" {
		t.Errorf("first: %+v %q", got[0], got[0].Target())
	}
	if got[1].Comment != "" || got[1].Target() != "ED25519 SHA256:other" {
		t.Errorf("unknown key: %+v", got[1])
	}
	if !got[2].Blocked || got[2].IP != "203.0.113.7" || got[2].Target() != "" || !got[3].Blocked || got[3].IP != "1.2.3.4" {
		t.Errorf("blocked: %+v %+v", got[2], got[3])
	}
}
