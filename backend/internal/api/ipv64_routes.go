package api

import "net/http"

// ipv64Routes registers the tab IPv64 of Settings › Dynamic DNS. Reads are GET (nothing in them is
// secret: the key never comes with a request); every change is owner or
// administrator and from a web session only.
func (s *Server) ipv64Routes(mux *routeMux) {
	read := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, s.requireAdmin(h)) }
	change := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, s.requireAdmin(s.browserOnly(h))) }

	read("GET /api/v1/ipv64", s.handleIPv64Status)
	change("PUT /api/v1/ipv64/key", s.handleIPv64Key)
	change("PUT /api/v1/ipv64/notify", s.handleIPv64Notify)
	change("PUT /api/v1/ipv64/blocklist", s.handleIPv64Blocklist)
	change("POST /api/v1/ipv64/blocklist/refresh", s.handleIPv64BlocklistRefresh)

	read("GET /api/v1/ipv64/me", s.handleIPv64Me)
	read("GET /api/v1/ipv64/logs", s.handleIPv64Logs)
	read("GET /api/v1/ipv64/api-keys", s.handleIPv64Keys)
	change("POST /api/v1/ipv64/api-keys", s.handleIPv64KeyCreate)
	change("DELETE /api/v1/ipv64/api-keys/{id}", s.handleIPv64KeyDelete)

	read("GET /api/v1/ipv64/domains", s.handleIPv64AreaDomains)
	change("POST /api/v1/ipv64/domains", s.handleIPv64AreaDomainCreate)
	read("GET /api/v1/ipv64/domains/{domain}", s.handleIPv64AreaDomain)
	change("PATCH /api/v1/ipv64/domains/{domain}", s.handleIPv64AreaDomainChange)
	change("DELETE /api/v1/ipv64/domains/{domain}", s.handleIPv64AreaDomainDelete)
	change("POST /api/v1/ipv64/domains/{domain}/rotate-token", s.handleIPv64AreaDomainRotate)
	change("POST /api/v1/ipv64/domains/{domain}/reset", s.handleIPv64AreaDomainReset)
	change("POST /api/v1/ipv64/domains/{domain}/records", s.handleIPv64AreaRecordAdd)
	change("PATCH /api/v1/ipv64/records/{record}", s.handleIPv64AreaRecordChange)
	change("DELETE /api/v1/ipv64/records/{record}", s.handleIPv64AreaRecordDelete)

	read("GET /api/v1/ipv64/healthchecks", s.handleIPv64Checks)
	change("POST /api/v1/ipv64/healthchecks", s.handleIPv64CheckCreate)
	read("GET /api/v1/ipv64/healthchecks/{id}", s.handleIPv64Check)
	change("PATCH /api/v1/ipv64/healthchecks/{id}", s.handleIPv64CheckChange)
	change("DELETE /api/v1/ipv64/healthchecks/{id}", s.handleIPv64CheckDelete)
	change("POST /api/v1/ipv64/healthchecks/{id}/{action}", s.handleIPv64CheckAction)
	read("GET /api/v1/ipv64/healthchecks/{id}/events", s.handleIPv64CheckEvents)
	read("GET /api/v1/ipv64/healthchecks/{id}/incidents", s.handleIPv64CheckIncidents)
	read("GET /api/v1/ipv64/healthchecks/{id}/stats", s.handleIPv64CheckStats)

	read("GET /api/v1/ipv64/integrations", s.handleIPv64Integrations)
	change("POST /api/v1/ipv64/integrations", s.handleIPv64IntegrationCreate)
	change("PATCH /api/v1/ipv64/integrations/{id}", s.handleIPv64IntegrationChange)
	change("DELETE /api/v1/ipv64/integrations/{id}", s.handleIPv64IntegrationDelete)
	change("POST /api/v1/ipv64/integrations/{id}/test", s.handleIPv64IntegrationTest)

	read("GET /api/v1/ipv64/status-pages", s.handleIPv64StatusPages)
	change("POST /api/v1/ipv64/status-pages", s.handleIPv64StatusPageCreate)
	change("PATCH /api/v1/ipv64/status-pages/{id}", s.handleIPv64StatusPageChange)
	change("DELETE /api/v1/ipv64/status-pages/{id}", s.handleIPv64StatusPageDelete)

	read("GET /api/v1/ipv64/offline-pages", s.handleIPv64OfflinePages)
	change("POST /api/v1/ipv64/offline-pages", s.handleIPv64OfflinePageCreate)
	change("PATCH /api/v1/ipv64/offline-pages/{id}", s.handleIPv64OfflinePageChange)
	change("DELETE /api/v1/ipv64/offline-pages/{id}", s.handleIPv64OfflinePageDelete)
	change("PUT /api/v1/ipv64/offline-pages/{id}/icon", s.handleIPv64OfflineIcon)
	change("DELETE /api/v1/ipv64/offline-pages/{id}/icon", s.handleIPv64OfflineIconDelete)

	read("GET /api/v1/ipv64/failover", s.handleIPv64Policies)
	change("POST /api/v1/ipv64/failover", s.handleIPv64PolicyCreate)
	change("PATCH /api/v1/ipv64/failover/{id}", s.handleIPv64PolicyChange)
	change("DELETE /api/v1/ipv64/failover/{id}", s.handleIPv64PolicyDelete)
	change("POST /api/v1/ipv64/failover/{id}/do/{action}", s.handleIPv64PolicyAction)
	change("POST /api/v1/ipv64/failover/{id}/monitors", s.handleIPv64MonitorAdd)
	change("DELETE /api/v1/ipv64/failover/{id}/monitors/{check}", s.handleIPv64MonitorRemove)
	change("POST /api/v1/ipv64/failover/{id}/rules", s.handleIPv64RuleAdd)
	change("DELETE /api/v1/ipv64/failover/{id}/rules/{record}", s.handleIPv64RuleRemove)
	change("POST /api/v1/ipv64/maintenance", s.handleIPv64MaintenanceCreate)
	change("DELETE /api/v1/ipv64/maintenance/{id}", s.handleIPv64MaintenanceDelete)

	read("GET /api/v1/ipv64/cdn", s.handleIPv64CDNs)
	change("POST /api/v1/ipv64/cdn", s.handleIPv64CDNCreate)
	read("GET /api/v1/ipv64/cdn/{token}", s.handleIPv64CDN)
	change("PATCH /api/v1/ipv64/cdn/{token}", s.handleIPv64CDNChange)
	change("DELETE /api/v1/ipv64/cdn/{token}", s.handleIPv64CDNDelete)
	change("POST /api/v1/ipv64/cdn/{token}/action", s.handleIPv64CDNAction)
	change("POST /api/v1/ipv64/cdn/{token}/records/{record}", s.handleIPv64CDNRecord)
	change("POST /api/v1/ipv64/cdn/{token}/mappings", s.handleIPv64MappingCreate)
	change("DELETE /api/v1/ipv64/cdn/{token}/mappings/{mapping}", s.handleIPv64MappingDelete)
	change("POST /api/v1/ipv64/cdn/{token}/access-list", s.handleIPv64AccessAdd)
	change("DELETE /api/v1/ipv64/cdn/{token}/access-list/{id}", s.handleIPv64AccessRemove)

	read("GET /api/v1/ipv64/assignments", s.handleIPv64Assignments)
	change("PUT /api/v1/ipv64/assignments/{domain}", s.handleIPv64Assign)
	change("DELETE /api/v1/ipv64/assignments/{domain}", s.handleIPv64Unassign)
	change("PUT /api/v1/ipv64/ddns", s.handleIPv64DDNS)

	read("GET /api/v1/ipv64/apps/{app}", s.handleIPv64App)
	change("PUT /api/v1/ipv64/apps/{app}", s.handleIPv64Publish)
	change("DELETE /api/v1/ipv64/apps/{app}", s.handleIPv64Unpublish)

	read("GET /api/v1/ipv64/routers", s.handleIPv64Routers)
	change("POST /api/v1/ipv64/routers", s.handleIPv64RouterCreate)
	read("GET /api/v1/ipv64/routers/{token}", s.handleIPv64Router)
	change("PATCH /api/v1/ipv64/routers/{token}", s.handleIPv64RouterRename)
	change("POST /api/v1/ipv64/routers/{token}/{kind}", s.handleIPv64RouterAdd)
	change("PATCH /api/v1/ipv64/routers/{token}/{kind}/{id}", s.handleIPv64RouterItem)
	change("DELETE /api/v1/ipv64/routers/{token}/{kind}/{id}", s.handleIPv64RouterItem)
	change("PATCH /api/v1/ipv64/gateways/{token}", s.handleIPv64GatewayChange)
	change("POST /api/v1/ipv64/gateways/{token}/do/{action}", s.handleIPv64GatewayAction)
	change("DELETE /api/v1/ipv64/gateways/{token}", s.handleIPv64GatewayAction)
	change("POST /api/v1/ipv64/gateways/{token}/peers", s.handleIPv64PeerCreate)
	change("DELETE /api/v1/ipv64/peers/{token}", s.handleIPv64PeerDelete)

	read("GET /api/v1/ipv64/tunnel", s.handleIPv64Tunnel)
	change("POST /api/v1/ipv64/tunnel", s.handleIPv64TunnelConnect)
	change("DELETE /api/v1/ipv64/tunnel", s.handleIPv64TunnelDisconnect)
}
