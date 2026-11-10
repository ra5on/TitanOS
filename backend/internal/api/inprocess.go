package api

import (
	"path/filepath"
	"slices"
)

// inProcessIdentity marks a request that titand makes to itself in the name
// of an account: an MCP action tool or a button of Home Assistant. Only
// in-process requests can carry this unexported context key. Such a request
// is no person at a web session, so routes that need one refuse it.
type inProcessIdentity struct{}

// notifyDir is where personal notifications held back for quiet hours are
// kept: on the first mounted volume, never on the stick.
func (s *Server) notifyDir() (string, bool) {
	if s.core().storage == nil {
		return "", false
	}
	mounted := s.core().storage.Mounted()
	if len(mounted) == 0 {
		return "", false
	}
	slices.Sort(mounted)
	return filepath.Join(s.core().storage.MountPoint(mounted[0]), ".titanos", "notify"), true
}

// notifyAudience are the accounts that may get the notifications of the NAS
// on their own channels: owners and administrators that are not locked.
func (s *Server) notifyAudience() []string {
	if s.core().users == nil {
		return nil
	}
	var names []string
	for _, user := range s.core().users.List() {
		if user.Role.CanAdminister() && !user.Disabled {
			names = append(names, user.Name)
		}
	}
	return names
}
