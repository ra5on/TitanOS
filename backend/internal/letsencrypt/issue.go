package letsencrypt

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// accountKey returns the key of the ACME account, made on first use. The
// same key serves every directory; each CA knows it as its own account.
func (m *Manager) accountKey() (crypto.Signer, error) {
	if data, err := m.store.Read(accountFile); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, errors.New("account key not readable")
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("account key not readable: %w", err)
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("account key not usable")
		}
		return signer, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := m.store.Write(accountFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return nil, fmt.Errorf("account key not saved to /config: %w", err)
	}
	return key, nil
}

// challengeFQDN is where the TXT record of an authorization goes; a
// wildcard is proven on the name below it.
func challengeFQDN(identifier string) string {
	return "_acme-challenge." + strings.TrimPrefix(identifier, "*.")
}

type pending struct {
	authzURL string
	chal     *acme.Challenge
	fqdn     string
	value    string
}

// issue runs one ACME order for names and returns the PEM file to store.
func (m *Manager) issue(ctx context.Context, s Settings, names []string) ([]byte, error) {
	key, err := m.accountKey()
	if err != nil {
		return nil, err
	}
	client := &acme.Client{Key: key, DirectoryURL: s.directory(), HTTPClient: m.httpClient, UserAgent: "TitanOS"}
	account := &acme.Account{}
	if s.Email != "" {
		account.Contact = []string{"mailto:" + s.Email}
	}
	if _, err := client.Register(ctx, account, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("account: %w", err)
	}

	m.setPhase("order")
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(names...))
	if err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	var todo []pending
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, fmt.Errorf("authorization: %w", err)
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == s.Challenge {
				chal = c
			}
		}
		if chal == nil {
			return nil, fmt.Errorf("the CA offers no %s challenge for %s", s.Challenge, authz.Identifier.Value)
		}
		p := pending{authzURL: authzURL, chal: chal}
		if s.Challenge == ChallengeDNS {
			p.fqdn = challengeFQDN(authz.Identifier.Value)
			if p.value, err = client.DNS01ChallengeRecord(chal.Token); err != nil {
				return nil, err
			}
		} else {
			if p.value, err = client.HTTP01ChallengeResponse(chal.Token); err != nil {
				return nil, err
			}
		}
		todo = append(todo, p)
	}

	if s.Challenge == ChallengeDNS {
		err = m.proveDNS(ctx, client, s, todo)
	} else {
		err = m.proveHTTP(ctx, client, todo)
	}
	if err != nil {
		return nil, err
	}

	m.setPhase("finalize")
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: strings.TrimPrefix(names[0], "*.")}, DNSNames: names}, certKey)
	if err != nil {
		return nil, err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return nil, err
	}
	var out []byte
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: metaBlock, Headers: map[string]string{"Directory": s.directory()}})...)
	for _, der := range chain {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...)
	return out, nil
}

// accept tells the CA to look and waits for its verdict on each one.
func accept(ctx context.Context, client *acme.Client, round []pending) error {
	for _, p := range round {
		if _, err := client.Accept(ctx, p.chal); err != nil {
			return fmt.Errorf("challenge: %w", err)
		}
	}
	for _, p := range round {
		if _, err := client.WaitAuthorization(ctx, p.authzURL); err != nil {
			return fmt.Errorf("validation: %w", err)
		}
	}
	return nil
}

// proveDNS sets the TXT records, waits until the name servers of the
// provider know them, lets the CA look and removes the records again. A
// provider that holds one TXT record per name (DuckDNS) does one
// authorization at a time.
func (m *Manager) proveDNS(ctx context.Context, client *acme.Client, s Settings, todo []pending) error {
	if len(todo) == 0 {
		return nil
	}
	provider, err := m.newChallenger(s.Provider)
	if err != nil {
		return err
	}
	rounds := [][]pending{todo}
	if !provider.MultipleTXT() {
		rounds = nil
		for _, p := range todo {
			rounds = append(rounds, []pending{p})
		}
	}
	wait := time.Duration(s.Propagation) * time.Second
	if wait == 0 {
		wait = dnsprovider.PropagationDefault(s.Provider.Type)
	}
	for _, round := range rounds {
		values := map[string][]string{}
		var order []string
		for _, p := range round {
			if _, ok := values[p.fqdn]; !ok {
				order = append(order, p.fqdn)
			}
			values[p.fqdn] = append(values[p.fqdn], p.value)
		}
		m.setPhase("dns")
		err := func() error {
			defer func() {
				// Clean up even when the order was cancelled.
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
				defer cancel()
				for _, fqdn := range order {
					if err := provider.ClearTXT(cleanup, fqdn); err != nil {
						slog.Warn("TXT record of the DNS challenge not removed", "name", fqdn, "err", err)
					}
				}
			}()
			for _, fqdn := range order {
				if err := provider.SetTXT(ctx, fqdn, values[fqdn]); err != nil {
					return fmt.Errorf("DNS: %w", err)
				}
			}
			m.setPhase("propagation")
			if err := m.sleep(ctx, wait); err != nil {
				return err
			}
			m.checkPublic(ctx, values)
			m.setPhase("validation")
			return accept(ctx, client, round)
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// proveHTTP answers the challenges on port 80 while the CA looks.
func (m *Manager) proveHTTP(ctx context.Context, client *acme.Client, todo []pending) error {
	m.mu.Lock()
	for _, p := range todo {
		m.http01[p.chal.Token] = p.value
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		for _, p := range todo {
			delete(m.http01, p.chal.Token)
		}
		m.mu.Unlock()
	}()
	m.setPhase("validation")
	return accept(ctx, client, todo)
}

// PublicResolvers are asked whether the TXT records can be seen yet.
var PublicResolvers = []string{"1.1.1.1:53", "9.9.9.9:53"}

// lookupTXT asks one resolver for the TXT records of a name.
func lookupTXT(ctx context.Context, server, name string) ([]string, error) {
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, server)
	}}
	return resolver.LookupTXT(ctx, name)
}

// checkPublic waits up to two minutes until public resolvers see every
// value. Let's Encrypt asks the authoritative name servers itself, so this
// only helps with slow providers; it never fails the order.
func (m *Manager) checkPublic(ctx context.Context, values map[string][]string) {
	if m.lookupTXT == nil {
		return
	}
	deadline := m.now().Add(2 * time.Minute)
	for {
		visible := true
		for fqdn, want := range values {
			seen := false
			for _, server := range PublicResolvers {
				lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
				got, err := m.lookupTXT(lookup, server, fqdn)
				cancel()
				if err == nil && containsAll(got, want) {
					seen = true
					break
				}
			}
			visible = visible && seen
		}
		if visible || !m.now().Before(deadline) {
			if !visible {
				slog.Info("TXT records not yet visible to public resolvers; asking the CA anyway")
			}
			return
		}
		if m.sleep(ctx, 10*time.Second) != nil {
			return
		}
	}
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}
