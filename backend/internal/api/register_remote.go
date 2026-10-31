package api

// registerRemoteAccess registers the routes of SSH, the certificate, Let's Encrypt and dynamic DNS.
func (s *Server) registerRemoteAccess(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/system/ssh", s.requireAdmin(s.handleSSH))
	mux.HandleFunc("PUT /api/v1/system/ssh", s.requireAdmin(s.handleSSHChange))
	mux.HandleFunc("PUT /api/v1/system/ssh/networks", s.requireAdmin(s.handleSSHNetworks))
	mux.HandleFunc("GET /api/v1/system/certificate", s.requireAdmin(s.handleCertificate))
	mux.HandleFunc("PUT /api/v1/system/certificate", s.requireAdmin(s.handleCertificateSet))
	mux.HandleFunc("DELETE /api/v1/system/certificate", s.requireAdmin(s.handleCertificateRemove))
	mux.HandleFunc("GET /api/v1/system/letsencrypt", s.requireAdmin(s.handleLetsEncrypt))
	mux.HandleFunc("PUT /api/v1/system/letsencrypt", s.requireAdmin(s.handleLetsEncryptChange))
	mux.HandleFunc("POST /api/v1/system/letsencrypt/renew", s.requireAdmin(s.handleLetsEncryptRenew))
	mux.HandleFunc("GET /api/v1/network/ddns", s.requireAdmin(s.handleDDNS))
	mux.HandleFunc("PUT /api/v1/network/ddns", s.requireAdmin(s.handleDDNSChange))
	mux.HandleFunc("POST /api/v1/network/ddns/update", s.requireAdmin(s.handleDDNSUpdate))
}

// registerIPv64 registers the routes of the IPv64.net account.
func (s *Server) registerIPv64(mux *routeMux) {
	// IPv64.net: POST everywhere, so that a typed-in API key stays out of addresses.
	mux.HandleFunc("POST /api/v1/network/ipv64/account", s.requireAdmin(s.handleIPv64Account))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains", s.requireAdmin(s.handleIPv64Domains))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/create", s.requireAdmin(s.handleIPv64DomainCreate))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/change", s.requireAdmin(s.handleIPv64DomainChange))
	mux.HandleFunc("POST /api/v1/network/ipv64/records", s.requireAdmin(s.handleIPv64Records))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/add", s.requireAdmin(s.handleIPv64RecordAdd))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/delete", s.requireAdmin(s.handleIPv64RecordDelete))
	mux.HandleFunc("POST /api/v1/network/ipv64/healthchecks", s.requireAdmin(s.handleIPv64Healthchecks))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/on", s.requireAdmin(s.handleIPv64HeartbeatOn))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/off", s.requireAdmin(s.handleIPv64HeartbeatOff))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/status", s.requireAdmin(s.handleIPv64HeartbeatStatus))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/test", s.requireAdmin(s.handleIPv64HeartbeatTest))
	s.ipv64Routes(mux)
}
