package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ServerName is the name a node asks for when it connects to another one.
// titand then presents the cluster certificate of the node instead of the
// certificate of the web interface, which may change (Let's Encrypt,
// Tailscale, one's own) and is not what the cluster pins.
const ServerName = "node.titanos-cluster.invalid"

type certificate struct {
	tls         tls.Certificate
	fingerprint string
}

// newCertificate makes the certificate of a node: self-signed, valid for
// ServerName only, for 30 years. Nobody but the cluster trusts it.
func newCertificate() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "TitanOS cluster node"},
		DNSNames:     []string{ServerName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(30, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) +
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), nil
}

func parseCertificate(data string) (certificate, error) {
	cert, err := tls.X509KeyPair([]byte(data), []byte(data))
	if err != nil {
		return certificate{}, fmt.Errorf("cluster certificate: %w", err)
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return certificate{}, err
		}
	}
	return certificate{tls: cert, fingerprint: fingerprint(cert.Certificate[0])}, nil
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// Certificate returns the cluster certificate of this node for a TLS
// connection that asked for ServerName, nil for every other.
func (m *Manager) Certificate(hello *tls.ClientHelloInfo) *tls.Certificate {
	if hello == nil || hello.ServerName != ServerName {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return nil
	}
	cert := m.cert.tls
	return &cert
}

// ServerConfig returns the TLS configuration for a connection of another
// node (one that asked for ServerName), nil for every other: the cluster
// certificate of this node, and the other node is asked for its own.
func (m *Manager) ServerConfig(hello *tls.ClientHelloInfo) *tls.Config {
	cert := m.Certificate(hello)
	if cert == nil {
		return nil
	}
	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		// Not checked against an authority: Verify compares the
		// fingerprint with the node the request is signed for.
		ClientAuth: tls.RequestClientCert,
	}
}

// pinned returns a TLS configuration that accepts exactly the certificate
// with the fingerprint want, whoever signed it, and shows own: the other
// node checks it against the node the request is signed for (Verify).
func pinned(want string, own tls.Certificate) *tls.Config {
	return &tls.Config{
		ServerName:   ServerName,
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{own},
		// The chain is not checked against any authority: the fingerprint
		// below is the whole check.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || subtle.ConstantTimeCompare([]byte(fingerprint(raw[0])), []byte(want)) != 1 {
				return errors.New("cluster: certificate of the node does not match")
			}
			return nil
		},
	}
}
