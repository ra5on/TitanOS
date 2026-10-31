package api

import (
	"context"
	"errors"
)

// Start does what New leaves out because it has effects: it reads the
// settings of the stand-ins New made for managers that were not given,
// and it ties the root context of the server to ctx, so that its own jobs
// end with the daemon. Errors are returned together; the server works
// without the settings that could not be read (as before, when New
// ignored them).
func (s *Server) Start(ctx context.Context) error {
	if ctx != nil {
		stop := context.AfterFunc(ctx, s.cancel)
		// Close ends the root context itself; the hook is then no longer needed.
		context.AfterFunc(s.ctx, func() { stop() })
	}
	var errs []error
	for _, load := range s.pending {
		if err := load(s.ctx); err != nil {
			errs = append(errs, err)
		}
	}
	s.pending = nil
	return errors.Join(errs...)
}

// Close ends the root context of the server and waits for the jobs it
// started on its own (a power action waiting for a rebuild, a restart
// after a restore). Requests in flight are the business of http.Server.
func (s *Server) Close() error {
	s.cancel()
	s.jobs.Wait()
	return nil
}

// goJob runs a job of the server in the background. It gets the root
// context and is counted, so that Close waits for it.
func (s *Server) goJob(job func(ctx context.Context)) {
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		job(s.ctx)
	}()
}
