package api

import (
	"context"
	"errors"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// One native guard state covers the share, its volume path and aliases. Only
// the explicitly selected output directory is checked; reading stays available.
func (s *Server) personalGuardedDirectory(directory string) error {
	if s.opts.Shares == nil {
		return errPersonalDenied
	}
	guarded, err := s.opts.Shares.GuardedDirectory(directory)
	switch {
	case err != nil:
		return errPersonalSource
	case guarded:
		return errors.New("die Freigabe ist durch den Schutz nur lesbar")
	}
	return nil
}

func (s *Server) personalOfficePublish(ctx context.Context) func(string) error {
	return func(directory string) error {
		if err := pa.Reauthorize(ctx); err != nil {
			return err
		}
		if err := s.personalGuardedDirectory(directory); err != nil {
			return errors.Join(pa.ErrNoEffect, err)
		}
		return nil
	}
}
