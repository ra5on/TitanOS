package api

import "net/http"

// Admit a backend job before it is published. Automatic jobs use the same
// maintenance manager's Enter hook. Stop/review routes remain available.
func (s *Server) admitOperation(w http.ResponseWriter) (func(), bool) {
	if s.opts.Maintenance == nil {
		return func() {}, true
	}
	leave, err := s.opts.Maintenance.Enter()
	if err != nil {
		writeError(w, http.StatusLocked, "maintenance_running", err.Error())
		return nil, false
	}
	return leave, true
}

func (s *Server) requireHeavyAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(s.admitted(next))
}

// admitted runs next as an admitted heavy operation.
func (s *Server) admitted(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		leave, ok := s.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
		next(w, r)
	}
}
