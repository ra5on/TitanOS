package api

// registerRemoteAccess registers the routes of SSH, the certificate, Let's Encrypt and dynamic DNS.
func (s *Server) registerRemoteAccess(mux *routeMux) {
	sshArea{ssh: s.opts.SSH}.register(mux, s.requireAdmin)
	certificateArea{cert: s.opts.Certificate}.register(mux, s.requireAdmin)
	// The certificate (never the key) of the TitanOS authority, for devices.
	mux.HandleFunc("GET /api/v1/certs/ca", s.requireUser(s.handleCAInfo))
	mux.HandleFunc("GET /api/v1/certs/{file}", s.requireUser(s.handleCADownload))
	letsEncryptArea{acme: s.opts.LetsEncrypt, firewall: s.opts.Firewall}.register(mux, s.requireAdmin)
	ddnsArea{ddns: s.opts.DDNS}.register(mux, s.requireAdmin)
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
