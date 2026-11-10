package api

// registerHardware registers the routes of hardware, memory, power button, energy and power schedule.
func (s *Server) registerHardware(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/hardware", s.requireAdmin(s.handleHardware))
	mux.HandleFunc("GET /api/v1/system/memory", s.requireAdmin(s.handleMemoryPolicy))
	mux.HandleFunc("PUT /api/v1/system/memory", s.requireAdmin(s.handleMemoryPolicyChange))
	mux.HandleFunc("GET /api/v1/system/power-button", s.requireAdmin(s.handlePowerButton))
	mux.HandleFunc("PUT /api/v1/system/power-button", s.requireAdmin(s.handlePowerButtonChange))
	mux.HandleFunc("GET /api/v1/system/energy", s.requireAdmin(s.handleEnergy))
	mux.HandleFunc("PUT /api/v1/system/energy", s.requireAdmin(s.handleEnergyChange))
	mux.HandleFunc("GET /api/v1/system/power-schedule", s.requireAdmin(s.handlePowerSchedule))
	mux.HandleFunc("PUT /api/v1/system/power-schedule", s.requireAdmin(s.handlePowerScheduleChange))
	mux.HandleFunc("GET /api/v1/hardware/container-devices", s.requireAdmin(s.handleContainerDevices))
	nvidiaArea{nvidia: s.opts.Nvidia}.register(mux, s.requireAdmin)
	intelGucArea{guc: s.opts.IntelGuc}.register(mux, s.requireAdmin)
	uasQuirksArea{quirks: s.opts.UASQuirks}.register(mux, s.requireAdmin)
	hwcheckArea{check: s.opts.HWCheck, version: s.opts.Version}.register(mux, s.requireAdmin)
}

// registerFans registers the routes of the fans.
func (s *Server) registerFans(mux *routeMux) {
	fansArea{fans: s.opts.Fans}.register(mux, s.requireAdmin)
}
