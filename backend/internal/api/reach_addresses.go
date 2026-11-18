package api

import (
	"cmp"
	"crypto/tls"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

// reachAddress is one way a device can reach this NAS: the home network,
// Tailscale, NetBird, dynamic DNS or an own domain. Only what is set up
// appears; nothing is looked up on the internet. Shared by the agent app
// (agentAddresses, PLAN 53) and the setup page of the iPhone
// (deviceSetup, TECHNIK 4.2); each takes its own order and scheme.
type reachAddress struct {
	Kind string // lan, tailscale, netbird, ddns, domain
	Host string
	// LetsEncrypt: the host is one of the domains of Let's Encrypt (its
	// settings; whether the certificate covers it is another question).
	LetsEncrypt bool
	// TailscaleCert: the name in the tailnet has its certificate.
	TailscaleCert bool
}

// reachSources are the facts the addresses come from.
type reachSources struct {
	lan      string
	tsName   string
	tsIPs    []string
	tsCertOK bool
	netbird  string
	ddns     []string
	domains  []string
}

func (s *Server) reachSources() reachSources {
	env := s.agentEnv()
	src := reachSources{lan: appproxy.PrimaryIPv4(), ddns: s.ipv64Area().ddnsHosts()}
	if env.tailscale != nil {
		ts := env.tailscale.Status()
		src.tsName, src.tsIPs, src.tsCertOK = ts.Name, ts.IPs, ts.HTTPS.State == "ok"
	}
	if env.netbird != nil {
		src.netbird = env.netbird.Status().FQDN
	}
	if env.letsEncrypt != nil {
		src.domains = env.letsEncrypt.Status().Settings.Domains
	}
	return src
}

// reachAddresses lists the addresses of this NAS: LAN, Tailscale, NetBird,
// DDNS, domains; a host appears once, under its first kind; wildcard
// domains are left out.
func (s *Server) reachAddresses() []reachAddress {
	return reachList(s.reachSources())
}

func reachList(src reachSources) []reachAddress {
	out := []reachAddress{}
	seen := map[string]bool{}
	add := func(a reachAddress) {
		a.Host = strings.TrimSuffix(strings.TrimSpace(a.Host), ".")
		if a.Host == "" || seen[a.Host] {
			return
		}
		seen[a.Host] = true
		out = append(out, a)
	}
	add(reachAddress{Kind: "lan", Host: src.lan})
	if src.tsName != "" {
		add(reachAddress{Kind: "tailscale", Host: src.tsName, TailscaleCert: src.tsCertOK})
	} else if len(src.tsIPs) > 0 {
		add(reachAddress{Kind: "tailscale", Host: src.tsIPs[0]})
	}
	if src.netbird != "" {
		add(reachAddress{Kind: "netbird", Host: src.netbird})
	}
	for _, h := range src.ddns {
		add(reachAddress{Kind: "ddns", Host: h, LetsEncrypt: slices.Contains(src.domains, h)})
	}
	for _, d := range src.domains {
		if !strings.HasPrefix(d, "*.") {
			add(reachAddress{Kind: "domain", Host: d, LetsEncrypt: true})
		}
	}
	return out
}

// ---------- the setup page of the iPhone (TECHNIK 4.2) ----------

// Certificates of an address on the setup page.
const (
	certValid      = "valid"      // Let's Encrypt or Tailscale: iOS trusts it
	certSelfSigned = "selfSigned" // made by TitanOS: iOS asks once
	certOwn        = "own"        // put in by an administrator
)

// Hints of an address on the setup page; the web UI words them.
const (
	hintTailscaleCert = "tailscaleCertificate" // switch on the certificate of Tailscale (settings)
	hintTailscaleApp  = "tailscaleApp"         // the iPhone needs the Tailscale app
	hintNetbirdApp    = "netbirdApp"           // the iPhone needs the NetBird app
	hintSelfSigned    = "selfSignedOnce"       // iOS warns once about the certificate
	hintHomeOnly      = "homeOnly"             // only in the home network
)

// setupAddress is a server for the accounts of CalDAV and CardDAV.
type setupAddress struct {
	Kind        string   `json:"kind"`
	Server      string   `json:"server"`
	CalDAV      string   `json:"caldav"`
	CardDAV     string   `json:"carddav"`
	Certificate string   `json:"certificate"`
	Recommended bool     `json:"recommended,omitempty"`
	Hints       []string `json:"hints"`
}

// setupCerts says which certificate a host gets: letsEncrypt reports a
// valid certificate of Let's Encrypt (not staging) for it, fallback is the
// certificate of every other name (selfSigned or own).
type setupCerts struct {
	letsEncrypt func(host string) bool
	fallback    string
}

// setupRank is the order of the owner (TECHNIK 4.2): a name of Let's
// Encrypt, Tailscale (recommended), the home network, then DDNS and
// NetBird.
var setupRank = map[string]int{"domain": 0, "tailscale": 1, "lan": 2, "ddns": 3, "netbird": 4}

// setupAddresses turns the addresses into servers for the iPhone: a
// domain only with a valid certificate, a DDNS name with one counts as a
// domain; Host:Port when the web UI is not on 443.
func setupAddresses(list []reachAddress, account string, port int, certs setupCerts) []setupAddress {
	out := []setupAddress{}
	for _, a := range list {
		kind, cert := a.Kind, certs.fallback
		var hints []string
		switch a.Kind {
		case "domain":
			if !certs.letsEncrypt(a.Host) {
				continue
			}
			cert = certValid
		case "ddns":
			if a.LetsEncrypt && certs.letsEncrypt(a.Host) {
				kind, cert = "domain", certValid
			}
		case "tailscale":
			hints = append(hints, hintTailscaleApp)
			if a.TailscaleCert {
				cert = certValid
			} else if net.ParseIP(a.Host) == nil {
				// Only a name in the tailnet gets a certificate.
				hints = append(hints, hintTailscaleCert)
			}
		case "lan":
			hints = append(hints, hintHomeOnly)
		case "netbird":
			hints = append(hints, hintNetbirdApp)
		default:
			continue
		}
		if cert == certSelfSigned {
			hints = append(hints, hintSelfSigned)
		}
		server := a.Host
		if strings.Contains(server, ":") {
			server = "[" + server + "]" // IPv6
		}
		if port != 443 {
			server = net.JoinHostPort(a.Host, strconv.Itoa(port))
		}
		base := "https://" + server + "/"
		acc := url.PathEscape(account)
		out = append(out, setupAddress{Kind: kind, Server: server, CalDAV: base + "caldav/" + acc + "/",
			CardDAV: base + "carddav/" + acc + "/", Certificate: cert, Hints: append([]string{}, hints...)})
	}
	slices.SortStableFunc(out, func(a, b setupAddress) int { return cmp.Compare(setupRank[a.Kind], setupRank[b.Kind]) })
	recommend := func(kind string) bool {
		for i := range out {
			if out[i].Kind == kind {
				out[i].Recommended = true
				return true
			}
		}
		return false
	}
	if !recommend("tailscale") {
		recommend("domain")
	}
	return out
}

// setupCerts reads the certificates of the web UI.
func (s *Server) setupCerts() setupCerts {
	certs := setupCerts{letsEncrypt: func(string) bool { return false }, fallback: certSelfSigned}
	manager := s.core().certificate
	if manager == nil {
		return certs
	}
	info := manager.Info()
	if info.Kind == tlscert.KindCustom {
		certs.fallback = certOwn
	}
	// Staging and another ACME CA (Directory, e.g. step-ca at home) are not
	// trusted by iOS: such a domain is left out like one without a
	// certificate.
	untrusted := false
	if le := s.opts.LetsEncrypt; le != nil {
		st := le.Status()
		untrusted = st.CertificateStaging || st.Settings.Directory != ""
	}
	if info.LetsEncrypt == nil || untrusted {
		return certs
	}
	fingerprint := info.LetsEncrypt.Fingerprint
	certs.letsEncrypt = func(host string) bool {
		// What a client asking for this name gets (expired ones are not served).
		served, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: host})
		return err == nil && tlscert.Describe(served).Fingerprint == fingerprint
	}
	return certs
}

// deviceSetup is GET /account/devices/setup: what the iPhone needs, for
// every account – host names and addresses, no secret.
type deviceSetup struct {
	User      string         `json:"user"`
	Port      int            `json:"port"`
	Addresses []setupAddress `json:"addresses"`
}

func (s *Server) deviceSetup(account string) deviceSetup {
	port := s.opts.HTTPSPort
	if port <= 0 {
		port = 443
	}
	return deviceSetup{User: account, Port: port, Addresses: setupAddresses(s.reachAddresses(), account, port, s.setupCerts())}
}
