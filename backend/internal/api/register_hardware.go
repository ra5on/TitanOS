package api

// registerHardware registers the routes of hardware, memory, power button, energy and power schedule.
func (s *Server) registerHardware(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/hardware", s.requireAdmin(s.vmsArea().handleHardware))
	memoryArea{memory: s.opts.MemoryPolicy}.register(mux, s.requireAdmin)
	powerButtonArea{button: s.opts.PowerButton}.register(mux, s.requireAdmin)
	energyArea{energy: s.opts.Energy}.register(mux, s.requireAdmin)
	powerScheduleArea{schedule: s.opts.PowerSchedule}.register(mux, s.requireAdmin)
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
