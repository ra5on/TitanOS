package api

// registerHomeAssistant registers the routes of Home Assistant and the storage for it.
func (s *Server) registerHomeAssistant(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/homeassistant/settings", s.requireAdmin(s.handleHASettings))
	mux.HandleFunc("PUT /api/v1/homeassistant/settings", s.requireAdmin(s.handleHASettingsChange))
	mux.HandleFunc("POST /api/v1/homeassistant/check", s.requireAdmin(s.handleHACheck))
	mux.HandleFunc("GET /api/v1/homeassistant/entities", s.requireAdmin(s.handleHAEntities))
	mux.HandleFunc("POST /api/v1/homeassistant/undo", s.requireAdmin(s.handleHAUndo))
	mux.HandleFunc("POST /api/v1/homeassistant/call", s.requireAdmin(s.handleHACall))
	mux.HandleFunc("GET /api/v1/homeassistant/notify-services", s.requireUser(s.handleHANotifyServices))
	// Storage for Home Assistant: a share, an account and the entry there.
	mux.HandleFunc("GET /api/v1/homeassistant/storage", s.requireAdmin(s.handleHAStorage))
	mux.HandleFunc("POST /api/v1/homeassistant/storage", s.requireAdmin(s.handleHAStorageAdd))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/register", s.requireAdmin(s.handleHAStorageRegister))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/check", s.requireAdmin(s.handleHAStorageCheck))
	mux.HandleFunc("PUT /api/v1/homeassistant/storage/{mount}", s.requireAdmin(s.handleHAStorageChange))
	mux.HandleFunc("DELETE /api/v1/homeassistant/storage/{mount}", s.requireAdmin(s.handleHAStorageRemove))
}
