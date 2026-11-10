package api

// registerHomeAssistant registers the routes of Home Assistant and the storage for it.
func (s *Server) registerHomeAssistant(mux *routeMux) {
	ha := s.haArea()
	// Several connections (PLAN §45); the other routes take ?connection=.
	mux.HandleFunc("GET /api/v1/homeassistant/connections", s.requireAdmin(ha.handleHAConnections))
	mux.HandleFunc("POST /api/v1/homeassistant/connections", s.requireAdmin(ha.handleHAConnectionAdd))
	mux.HandleFunc("DELETE /api/v1/homeassistant/connections/{id}", s.requireAdmin(ha.handleHAConnectionRemove))
	mux.HandleFunc("GET /api/v1/homeassistant/connection-names", s.requireUser(ha.handleHAConnectionNames))
	mux.HandleFunc("GET /api/v1/homeassistant/settings", s.requireAdmin(ha.handleHASettings))
	mux.HandleFunc("PUT /api/v1/homeassistant/settings", s.requireAdmin(ha.handleHASettingsChange))
	mux.HandleFunc("POST /api/v1/homeassistant/check", s.requireAdmin(ha.handleHACheck))
	mux.HandleFunc("GET /api/v1/homeassistant/entities", s.requireAdmin(ha.handleHAEntities))
	mux.HandleFunc("POST /api/v1/homeassistant/undo", s.requireAdmin(ha.handleHAUndo))
	mux.HandleFunc("POST /api/v1/homeassistant/call", s.requireAdmin(ha.handleHACall))
	mux.HandleFunc("GET /api/v1/homeassistant/notify-services", s.requireUser(ha.handleHANotifyServices))
	// Storage for Home Assistant: a share, an account and the entry there.
	mux.HandleFunc("GET /api/v1/homeassistant/storage", s.requireAdmin(ha.handleHAStorage))
	mux.HandleFunc("POST /api/v1/homeassistant/storage", s.requireAdmin(ha.handleHAStorageAdd))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/register", s.requireAdmin(ha.handleHAStorageRegister))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/check", s.requireAdmin(ha.handleHAStorageCheck))
	mux.HandleFunc("PUT /api/v1/homeassistant/storage/{mount}", s.requireAdmin(ha.handleHAStorageChange))
	mux.HandleFunc("DELETE /api/v1/homeassistant/storage/{mount}", s.requireAdmin(ha.handleHAStorageRemove))
}
