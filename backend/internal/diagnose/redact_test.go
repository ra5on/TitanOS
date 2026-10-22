package diagnose

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	r := NewRedactor("keller", []string{"alex", "bo", "maria"})
	cases := []struct{ in, want string }{
		{"login of alex from 192.168.1.20", "login of <user-1> from <ip-1>"},
		{"again 192.168.1.20, then 10.0.0.7 and 192.168.1.20", "again <ip-1>, then <ip-2> and <ip-1>"},
		{"listening on 127.0.0.1:8080 and 0.0.0.0:443 mask 255.255.255.0", "listening on 127.0.0.1:8080 and 0.0.0.0:443 mask 255.255.255.0"},
		{"TitanOS 1.2.3 kernel 6.12.111+deb13", "TitanOS 1.2.3 kernel 6.12.111+deb13"},
		{"at 12:30:05 link fe80::5054:ff:fe12:3456/64 up", "at 12:30:05 link <ip6-1>/64 up"},
		{"2001:db8:0:0:0:0:0:1 and ::1", "<ip6-2> and ::1"},
		{"eth0 52:54:00:12:34:56 brd ff:ff:ff:ff:ff:ff", "eth0 <mac-1> brd ff:ff:ff:ff:ff:ff"},
		{"host keller.local, not kellerassel", "host <nas>.local, not kellerassel"},
		{"bo stays, maria does not, mariadb stays", "bo stays, <user-2> does not, mariadb stays"},
		{"POSTGRES_PASSWORD=hunter2 TZ=Europe/Berlin", "POSTGRES_PASSWORD=<removed> TZ=Europe/Berlin"},
		{`"token": "abc123", "name": "x"`, `"token": <removed>, "name": "x"`},
		{"api_key: sk-123456 secret=\"a b\"", "api_key: <removed> secret=<removed>"},
		{"pull https://bob:s3cret@registry.example/v2/", "pull https://<removed>@registry.example/v2/"},
		// The scheme goes too; what matters is that the token does.
		{"Authorization: Bearer abcdefghijklmnop", "Authorization: <removed> <removed>"},
		{"sent Bearer abcdefghijklmnop to the registry", "sent Bearer <removed> to the registry"},
		{"mail to me@example.org failed", "mail to <email-1> failed"},
		{"Started serial-getty@ttyS0.service and user@1000.service", "Started serial-getty@ttyS0.service and user@1000.service"},
		{"sha256:" + strings.Repeat("ab", 32), "sha256:<removed>"},
		{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuv alex@pc", "ssh-ed25519 <key removed> <user-1>@pc"},
		{"-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----", "<key removed>"},
	}
	for _, tc := range cases {
		if got := r.Text(tc.in); got != tc.want {
			t.Errorf("%q\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
}

func TestSecretsKeepsJSONShape(t *testing.T) {
	in := `{"secretSet":{"email":false},"passwordSet":true,"password":"hunter2","token": "abc","apiKey":12345}`
	out := Secrets(in)
	if !strings.Contains(out, `"secretSet":{"email":false}`) || !strings.Contains(out, `"passwordSet":true`) || strings.Contains(out, "hunter2") || strings.Contains(out, "abc") || strings.Contains(out, "12345") {
		t.Fatalf("%s", out)
	}
	if got := Secrets("password=geheim user=alex"); strings.Contains(got, "geheim") || !strings.Contains(got, "user=alex") {
		t.Fatalf("%s", got)
	}
}
