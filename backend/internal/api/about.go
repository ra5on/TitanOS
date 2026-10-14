package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleAbout tells what is running and under which licenses: TitanOS
// itself and what it is built from. The texts lie in the image.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(s.opts.ShareDir, name))
		if err != nil {
			return ""
		}
		return string(data)
	}
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.opts.Version,
		"kernel":     strings.TrimSpace(string(kernel)),
		"license":    read("LICENSE.md"),
		"thirdParty": read("THIRD_PARTY_LICENSES"),
	})
}
