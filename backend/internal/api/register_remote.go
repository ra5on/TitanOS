package api

// registerRemoteAccess registers the routes of SSH, the certificate, Let's Encrypt and dynamic DNS.
func (s *Server) registerRemoteAccess(mux *routeMux) {
	sshArea{ssh: s.opts.SSH}.register(mux, s.requireAdmin)
	certificateArea{cert: s.opts.Certificate}.register(mux, s.requireAdmin)
	s.appAccessArea().registerCA(mux, s.requireUser)
	letsEncryptArea{acme: s.opts.LetsEncrypt, firewall: s.opts.Firewall}.register(mux, s.requireAdmin)
	ddnsArea{ddns: s.opts.DDNS}.register(mux, s.requireAdmin)
}

// registerIPv64 registers the routes of the IPv64.net account.
func (s *Server) registerIPv64(mux *routeMux) {
	s.ipv64Area().register(mux, s.requireAdmin, s.browserOnly)
}
