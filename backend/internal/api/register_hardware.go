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
	mux.HandleFunc("GET /api/v1/hardware/nvidia", s.requireAdmin(s.handleNvidia))
	mux.HandleFunc("PUT /api/v1/hardware/nvidia/{address}", s.requireAdmin(s.handleNvidiaSet))
	mux.HandleFunc("GET /api/v1/hardware/intel-gpu", s.requireAdmin(s.handleIntelGuc))
	mux.HandleFunc("PUT /api/v1/hardware/intel-gpu", s.requireAdmin(s.handleIntelGucSet))
}

// registerFans registers the routes of the fans.
func (s *Server) registerFans(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/system/fans", s.requireAdmin(s.handleFans))
	mux.HandleFunc("PUT /api/v1/system/fans", s.requireAdmin(s.handleFansSet))
}
