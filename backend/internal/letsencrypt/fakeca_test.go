package letsencrypt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

// fakeCA is a small ACME server (RFC 8555) for tests: enough of the
// protocol for golang.org/x/crypto/acme to register, order, prove and
// finalize. Signatures of the requests are not checked; challenges are
// judged by the validate function of the test.
type fakeCA struct {
	t        *testing.T
	server   *httptest.Server
	caKey    *ecdsa.PrivateKey
	caCert   *x509.Certificate
	lifetime time.Duration

	mu       sync.Mutex
	nonce    int
	accounts map[string]bool
	thumb    string
	orders   map[string]*fakeOrder
	authzs   map[string]*fakeAuthz
	// validate decides on a challenge: its type, the name, the token and
	// the key authorization the client must show.
	validate func(kind, name, token, keyAuth string) bool
	issued   int
}

type fakeOrder struct {
	id        string
	ids       []string
	authzs    []string
	finalized bool
	cert      []byte
}

type fakeAuthz struct {
	id       string
	name     string
	wildcard bool
	token    string
	status   string
}

func newFakeCA(t *testing.T) *fakeCA {
	ca := &fakeCA{t: t, accounts: map[string]bool{}, orders: map[string]*fakeOrder{}, authzs: map[string]*fakeAuthz{}, lifetime: 90 * 24 * time.Hour}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fake ACME CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	ca.caKey, ca.caCert = key, mustParse(der)
	ca.server = httptest.NewServer(http.HandlerFunc(ca.serve))
	t.Cleanup(ca.server.Close)
	return ca
}

func mustParse(der []byte) *x509.Certificate {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return c
}

func (ca *fakeCA) url(path string) string { return ca.server.URL + path }

type jws struct {
	Protected string `json:"protected"`
	Payload   string `json:"payload"`
}

func (ca *fakeCA) reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (ca *fakeCA) serve(w http.ResponseWriter, r *http.Request) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.nonce++
	w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", ca.nonce))
	if r.URL.Path == "/dir" {
		ca.reply(w, http.StatusOK, map[string]any{
			"newNonce": ca.url("/nonce"), "newAccount": ca.url("/acct"), "newOrder": ca.url("/new-order"),
			"revokeCert": ca.url("/revoke"), "keyChange": ca.url("/key-change"),
			"meta": map[string]any{"termsOfService": "https://ca.example/terms"},
		})
		return
	}
	if r.URL.Path == "/nonce" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var body jws
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		ca.reply(w, http.StatusBadRequest, map[string]string{"type": "urn:ietf:params:acme:error:malformed"})
		return
	}
	protectedJSON, _ := base64.RawURLEncoding.DecodeString(body.Protected)
	var protected struct {
		JWK json.RawMessage `json:"jwk"`
		KID string          `json:"kid"`
	}
	json.Unmarshal(protectedJSON, &protected)
	payload, _ := base64.RawURLEncoding.DecodeString(body.Payload)

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch parts[0] {
	case "acct":
		thumb := ca.thumbprint(protected.JWK)
		w.Header().Set("Location", ca.url("/acct/1"))
		status := http.StatusCreated
		if ca.accounts[thumb] {
			status = http.StatusOK
		}
		ca.accounts[thumb], ca.thumb = true, thumb
		ca.reply(w, status, map[string]any{"status": "valid"})
	case "new-order":
		var req struct {
			Identifiers []struct{ Type, Value string } `json:"identifiers"`
		}
		json.Unmarshal(payload, &req)
		o := &fakeOrder{id: fmt.Sprint(len(ca.orders) + 1)}
		for _, id := range req.Identifiers {
			o.ids = append(o.ids, id.Value)
			a := &fakeAuthz{id: fmt.Sprint(len(ca.authzs) + 1), status: "pending", token: fmt.Sprintf("token%d", len(ca.authzs)+1)}
			a.name, a.wildcard = strings.CutPrefix(id.Value, "*.")
			ca.authzs[a.id] = a
			o.authzs = append(o.authzs, a.id)
		}
		ca.orders[o.id] = o
		w.Header().Set("Location", ca.url("/order/"+o.id))
		ca.reply(w, http.StatusCreated, ca.orderJSON(o))
	case "authz":
		a := ca.authzs[parts[1]]
		ca.reply(w, http.StatusOK, ca.authzJSON(a))
	case "chal":
		a := ca.authzs[parts[1]]
		kind := parts[2]
		if a.status == "pending" {
			if ca.validate(kind, a.name, a.token, a.token+"."+ca.thumb) {
				a.status = "valid"
			} else {
				a.status = "invalid"
			}
		}
		ca.reply(w, http.StatusOK, map[string]any{"type": kind, "url": ca.url("/chal/" + a.id + "/" + kind), "token": a.token, "status": a.status})
	case "order":
		o := ca.orders[parts[1]]
		ca.reply(w, http.StatusOK, ca.orderJSON(o))
	case "finalize":
		o := ca.orders[parts[1]]
		var req struct {
			CSR string `json:"csr"`
		}
		json.Unmarshal(payload, &req)
		der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil || ca.status(o) != "ready" {
			ca.reply(w, http.StatusForbidden, map[string]string{"type": "urn:ietf:params:acme:error:orderNotReady"})
			return
		}
		ca.issued++
		template := &x509.Certificate{
			SerialNumber: big.NewInt(int64(100 + ca.issued)), Subject: pkix.Name{CommonName: csr.Subject.CommonName},
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(ca.lifetime),
			DNSNames: csr.DNSNames, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		leaf, _ := x509.CreateCertificate(rand.Reader, template, ca.caCert, csr.PublicKey, ca.caKey)
		o.cert = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.caCert.Raw})...)
		o.finalized = true
		w.Header().Set("Location", ca.url("/order/"+o.id))
		ca.reply(w, http.StatusOK, ca.orderJSON(o))
	case "cert":
		o := ca.orders[parts[1]]
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		w.Write(o.cert)
	default:
		ca.t.Errorf("fake CA: unexpected %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (ca *fakeCA) thumbprint(raw json.RawMessage) string {
	var jwk struct{ X, Y string }
	if err := json.Unmarshal(raw, &jwk); err != nil {
		ca.t.Errorf("jwk: %v", err)
		return ""
	}
	x, _ := base64.RawURLEncoding.DecodeString(jwk.X)
	y, _ := base64.RawURLEncoding.DecodeString(jwk.Y)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
	if err != nil {
		ca.t.Errorf("jwk key: %v", err)
		return ""
	}
	thumb, err := acme.JWKThumbprint(pub)
	if err != nil {
		ca.t.Errorf("thumbprint: %v", err)
	}
	return thumb
}

func (ca *fakeCA) status(o *fakeOrder) string {
	if o.finalized {
		return "valid"
	}
	ready := true
	for _, id := range o.authzs {
		switch ca.authzs[id].status {
		case "invalid":
			return "invalid"
		case "pending":
			ready = false
		}
	}
	if ready {
		return "ready"
	}
	return "pending"
}

func (ca *fakeCA) orderJSON(o *fakeOrder) map[string]any {
	var ids []map[string]string
	for _, v := range o.ids {
		ids = append(ids, map[string]string{"type": "dns", "value": v})
	}
	var authz []string
	for _, id := range o.authzs {
		authz = append(authz, ca.url("/authz/"+id))
	}
	out := map[string]any{"status": ca.status(o), "identifiers": ids, "authorizations": authz, "finalize": ca.url("/finalize/" + o.id)}
	if o.finalized {
		out["certificate"] = ca.url("/cert/" + o.id)
	}
	if ca.status(o) == "invalid" {
		out["error"] = map[string]any{"type": "urn:ietf:params:acme:error:unauthorized", "detail": "challenge failed"}
	}
	return out
}

func (ca *fakeCA) authzJSON(a *fakeAuthz) map[string]any {
	var chals []map[string]any
	for _, kind := range []string{"http-01", "dns-01"} {
		if kind == "http-01" && a.wildcard {
			continue
		}
		chals = append(chals, map[string]any{"type": kind, "url": ca.url("/chal/" + a.id + "/" + kind), "token": a.token, "status": a.status})
	}
	out := map[string]any{"identifier": map[string]string{"type": "dns", "value": a.name}, "status": a.status, "wildcard": a.wildcard, "challenges": chals}
	if a.status == "invalid" {
		chals[len(chals)-1]["error"] = map[string]any{"type": "urn:ietf:params:acme:error:unauthorized", "detail": "no TXT record found"}
	}
	return out
}
