package api

import "net/http"

// ipv64Routes registers the tab IPv64 of Settings › Dynamic DNS. Reads are GET (nothing in them is
// secret: the key never comes with a request); every change is owner or
// administrator and from a web session only.
func (a ipv64Area) ipv64Routes(mux *routeMux, admin, browserOnly func(http.HandlerFunc) http.HandlerFunc) {
	read := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, admin(h)) }
	change := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, admin(browserOnly(h))) }

	read("GET /api/v1/ipv64", a.handleIPv64Status)
	change("PUT /api/v1/ipv64/key", a.handleIPv64Key)
	change("PUT /api/v1/ipv64/notify", a.handleIPv64Notify)
	change("PUT /api/v1/ipv64/blocklist", a.handleIPv64Blocklist)
	change("POST /api/v1/ipv64/blocklist/refresh", a.handleIPv64BlocklistRefresh)

	read("GET /api/v1/ipv64/me", a.handleIPv64Me)
	read("GET /api/v1/ipv64/logs", a.handleIPv64Logs)
	read("GET /api/v1/ipv64/api-keys", a.handleIPv64Keys)
	change("POST /api/v1/ipv64/api-keys", a.handleIPv64KeyCreate)
	change("DELETE /api/v1/ipv64/api-keys/{id}", a.handleIPv64KeyDelete)

	read("GET /api/v1/ipv64/domains", a.handleIPv64AreaDomains)
	change("POST /api/v1/ipv64/domains", a.handleIPv64AreaDomainCreate)
	read("GET /api/v1/ipv64/domains/{domain}", a.handleIPv64AreaDomain)
	change("PATCH /api/v1/ipv64/domains/{domain}", a.handleIPv64AreaDomainChange)
	change("DELETE /api/v1/ipv64/domains/{domain}", a.handleIPv64AreaDomainDelete)
	change("POST /api/v1/ipv64/domains/{domain}/rotate-token", a.handleIPv64AreaDomainRotate)
	change("POST /api/v1/ipv64/domains/{domain}/reset", a.handleIPv64AreaDomainReset)
	change("POST /api/v1/ipv64/domains/{domain}/records", a.handleIPv64AreaRecordAdd)
	change("PATCH /api/v1/ipv64/records/{record}", a.handleIPv64AreaRecordChange)
	change("DELETE /api/v1/ipv64/records/{record}", a.handleIPv64AreaRecordDelete)

	read("GET /api/v1/ipv64/healthchecks", a.handleIPv64Checks)
	change("POST /api/v1/ipv64/healthchecks", a.handleIPv64CheckCreate)
	read("GET /api/v1/ipv64/healthchecks/{id}", a.handleIPv64Check)
	change("PATCH /api/v1/ipv64/healthchecks/{id}", a.handleIPv64CheckChange)
	change("DELETE /api/v1/ipv64/healthchecks/{id}", a.handleIPv64CheckDelete)
	change("POST /api/v1/ipv64/healthchecks/{id}/{action}", a.handleIPv64CheckAction)
	read("GET /api/v1/ipv64/healthchecks/{id}/events", a.handleIPv64CheckEvents)
	read("GET /api/v1/ipv64/healthchecks/{id}/incidents", a.handleIPv64CheckIncidents)
	read("GET /api/v1/ipv64/healthchecks/{id}/stats", a.handleIPv64CheckStats)

	read("GET /api/v1/ipv64/integrations", a.handleIPv64Integrations)
	change("POST /api/v1/ipv64/integrations", a.handleIPv64IntegrationCreate)
	change("PATCH /api/v1/ipv64/integrations/{id}", a.handleIPv64IntegrationChange)
	change("DELETE /api/v1/ipv64/integrations/{id}", a.handleIPv64IntegrationDelete)
	change("POST /api/v1/ipv64/integrations/{id}/test", a.handleIPv64IntegrationTest)

	read("GET /api/v1/ipv64/status-pages", a.handleIPv64StatusPages)
	change("POST /api/v1/ipv64/status-pages", a.handleIPv64StatusPageCreate)
	change("PATCH /api/v1/ipv64/status-pages/{id}", a.handleIPv64StatusPageChange)
	change("DELETE /api/v1/ipv64/status-pages/{id}", a.handleIPv64StatusPageDelete)

	read("GET /api/v1/ipv64/offline-pages", a.handleIPv64OfflinePages)
	change("POST /api/v1/ipv64/offline-pages", a.handleIPv64OfflinePageCreate)
	change("PATCH /api/v1/ipv64/offline-pages/{id}", a.handleIPv64OfflinePageChange)
	change("DELETE /api/v1/ipv64/offline-pages/{id}", a.handleIPv64OfflinePageDelete)
	change("PUT /api/v1/ipv64/offline-pages/{id}/icon", a.handleIPv64OfflineIcon)
	change("DELETE /api/v1/ipv64/offline-pages/{id}/icon", a.handleIPv64OfflineIconDelete)

	read("GET /api/v1/ipv64/failover", a.handleIPv64Policies)
	change("POST /api/v1/ipv64/failover", a.handleIPv64PolicyCreate)
	change("PATCH /api/v1/ipv64/failover/{id}", a.handleIPv64PolicyChange)
	change("DELETE /api/v1/ipv64/failover/{id}", a.handleIPv64PolicyDelete)
	change("POST /api/v1/ipv64/failover/{id}/do/{action}", a.handleIPv64PolicyAction)
	change("POST /api/v1/ipv64/failover/{id}/monitors", a.handleIPv64MonitorAdd)
	change("DELETE /api/v1/ipv64/failover/{id}/monitors/{check}", a.handleIPv64MonitorRemove)
	change("POST /api/v1/ipv64/failover/{id}/rules", a.handleIPv64RuleAdd)
	change("DELETE /api/v1/ipv64/failover/{id}/rules/{record}", a.handleIPv64RuleRemove)
	change("POST /api/v1/ipv64/maintenance", a.handleIPv64MaintenanceCreate)
	change("DELETE /api/v1/ipv64/maintenance/{id}", a.handleIPv64MaintenanceDelete)

	read("GET /api/v1/ipv64/cdn", a.handleIPv64CDNs)
	change("POST /api/v1/ipv64/cdn", a.handleIPv64CDNCreate)
	read("GET /api/v1/ipv64/cdn/{token}", a.handleIPv64CDN)
	change("PATCH /api/v1/ipv64/cdn/{token}", a.handleIPv64CDNChange)
	change("DELETE /api/v1/ipv64/cdn/{token}", a.handleIPv64CDNDelete)
	change("POST /api/v1/ipv64/cdn/{token}/action", a.handleIPv64CDNAction)
	change("POST /api/v1/ipv64/cdn/{token}/records/{record}", a.handleIPv64CDNRecord)
	change("POST /api/v1/ipv64/cdn/{token}/mappings", a.handleIPv64MappingCreate)
	change("DELETE /api/v1/ipv64/cdn/{token}/mappings/{mapping}", a.handleIPv64MappingDelete)
	change("POST /api/v1/ipv64/cdn/{token}/access-list", a.handleIPv64AccessAdd)
	change("DELETE /api/v1/ipv64/cdn/{token}/access-list/{id}", a.handleIPv64AccessRemove)

	read("GET /api/v1/ipv64/assignments", a.handleIPv64Assignments)
	change("PUT /api/v1/ipv64/assignments/{domain}", a.handleIPv64Assign)
	change("DELETE /api/v1/ipv64/assignments/{domain}", a.handleIPv64Unassign)
	change("PUT /api/v1/ipv64/ddns", a.handleIPv64DDNS)

	read("GET /api/v1/ipv64/apps/{app}", a.handleIPv64App)
	change("PUT /api/v1/ipv64/apps/{app}", a.handleIPv64Publish)
	change("DELETE /api/v1/ipv64/apps/{app}", a.handleIPv64Unpublish)

	read("GET /api/v1/ipv64/routers", a.handleIPv64Routers)
	change("POST /api/v1/ipv64/routers", a.handleIPv64RouterCreate)
	read("GET /api/v1/ipv64/routers/{token}", a.handleIPv64Router)
	change("PATCH /api/v1/ipv64/routers/{token}", a.handleIPv64RouterRename)
	change("POST /api/v1/ipv64/routers/{token}/{kind}", a.handleIPv64RouterAdd)
	change("PATCH /api/v1/ipv64/routers/{token}/{kind}/{id}", a.handleIPv64RouterItem)
	change("DELETE /api/v1/ipv64/routers/{token}/{kind}/{id}", a.handleIPv64RouterItem)
	change("PATCH /api/v1/ipv64/gateways/{token}", a.handleIPv64GatewayChange)
	change("POST /api/v1/ipv64/gateways/{token}/do/{action}", a.handleIPv64GatewayAction)
	change("DELETE /api/v1/ipv64/gateways/{token}", a.handleIPv64GatewayAction)
	change("POST /api/v1/ipv64/gateways/{token}/peers", a.handleIPv64PeerCreate)
	change("DELETE /api/v1/ipv64/peers/{token}", a.handleIPv64PeerDelete)

	read("GET /api/v1/ipv64/tunnel", a.handleIPv64Tunnel)
	change("POST /api/v1/ipv64/tunnel", a.handleIPv64TunnelConnect)
	change("DELETE /api/v1/ipv64/tunnel", a.handleIPv64TunnelDisconnect)
}
