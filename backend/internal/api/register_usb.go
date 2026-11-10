package api

// registerUSB registers the routes of USB drives.
func (s *Server) registerUSB(mux *routeMux) {
	usbArea{usb: s.opts.USB}.register(mux, s.requireAdmin)
}
