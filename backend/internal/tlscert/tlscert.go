// Package tlscert provides the self-signed certificate for the web UI.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	certFile = "tls/cert.pem"
	keyFile  = "tls/key.pem"

	// Browsers reject certificates that are valid for longer than 825 days.
	validity = 820 * 24 * time.Hour
	// A stored certificate that expires within this time is replaced.
	renewBefore = 30 * 24 * time.Hour
)

// LoadOrCreate returns the certificate stored in /config, or creates and
// stores a new one. Keeping it across boots means the browser's security
// exception only has to be confirmed once.
//
// If the new certificate cannot be stored it is returned anyway, together
// with the error: the web UI must come up even without the stick.
func LoadOrCreate(store *config.Store, hostname string) (tls.Certificate, error) {
	if cert, err := load(store); err == nil {
		return cert, nil
	}

	certPEM, keyPEM, err := generate(hostname, time.Now())
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := store.Write(keyFile, keyPEM); err != nil {
		return cert, fmt.Errorf("certificate not saved: %w", err)
	}
	if err := store.Write(certFile, certPEM); err != nil {
		return cert, fmt.Errorf("certificate not saved: %w", err)
	}
	return cert, nil
}

func load(store *config.Store) (tls.Certificate, error) {
	certPEM, err := store.Read(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := store.Read(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	if time.Until(leaf.NotAfter) < renewBefore {
		return tls.Certificate{}, errors.New("certificate expires soon")
	}
	if leaf.Subject.SerialNumber == "" {
		// Made before names were unique: Firefox and Chrome may take an
		// earlier TitanOS certificate of the same name for its issuer and
		// refuse it without a way to proceed. Safari does not.
		return tls.Certificate{}, errors.New("certificate name not unique")
	}
	return cert, nil
}

func generate(hostname string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		// Every certificate gets a name of its own. A browser that saw a
		// certificate of another TitanOS (or of this NAS before a new stick)
		// would otherwise look for the issuer under the same name, find the
		// old certificate and reject the new one as wrongly signed.
		Subject:               pkix.Name{CommonName: hostname, Organization: []string{"TitanOS"}, SerialNumber: fmt.Sprintf("%032x", serial)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname, hostname + ".local", "localhost"},
		IPAddresses:           append([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, localAddresses()...),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// localAddresses returns the addresses the NAS has right now, so that the
// certificate also matches when it is opened by IP address.
func localAddresses() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
			ips = append(ips, ipnet.IP)
		}
	}
	return ips
}
