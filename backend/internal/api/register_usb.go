package api

// registerUSB registers the routes of USB drives.
func (s *Server) registerUSB(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/usb", s.requireAdmin(s.handleUSB))
	mux.HandleFunc("POST /api/v1/usb/{id}/mount", s.requireAdmin(s.handleUSBMount))
	mux.HandleFunc("POST /api/v1/usb/{id}/eject", s.requireAdmin(s.handleUSBEject))
	mux.HandleFunc("GET /api/v1/usb/disks", s.requireAdmin(s.handleUSBDisks))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/format", s.requireAdmin(s.handleUSBFormat))
	mux.HandleFunc("GET /api/v1/usb/disks/{id}/partitions", s.requireAdmin(s.handleUSBPartitions))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/table", s.requireAdmin(s.handleUSBTable))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions", s.requireAdmin(s.handleUSBPartitionCreate))
	mux.HandleFunc("DELETE /api/v1/usb/disks/{id}/partitions/{number}", s.requireAdmin(s.handleUSBPartitionDelete))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/format", s.requireAdmin(s.handleUSBPartitionFormat))
	mux.HandleFunc("PUT /api/v1/usb/disks/{id}/partitions/{number}/label", s.requireAdmin(s.handleUSBPartitionLabel))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/grow", s.requireAdmin(s.handleUSBPartitionGrow))
}
