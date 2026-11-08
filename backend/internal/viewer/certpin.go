package viewer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"net"
	"time"
)

// appKeyPin reads the certificate an app presents on its own address and
// returns the base64 SHA-256 of its public key (SubjectPublicKeyInfo), the
// form Chromium's --ignore-certificate-errors-spki-list takes.
//
// PLAN 74 (owner 8.11.2026): apps such as Bambu Studio (linuxserver, Selkies)
// answer HTTPS with a certificate they signed themselves, and the viewer's
// browser showed its warning page every time. Trusting exactly this key –
// read by titand from the app's address on the viewer network, never from
// what the browser is shown – removes the warning for this app only; any
// other site, or the app with a different key, is checked as before.
func appKeyPin(ctx context.Context, address string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{},
		// Only to read the certificate; nothing is sent over this connection.
		Config: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return ""
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return ""
	}
	sum := sha256.Sum256(certs[0].RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}
