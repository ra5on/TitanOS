package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// aboutArea is the about page; an area with narrow dependencies (PLAN 8a,
// see fansArea). shareDir holds the texts of the licenses.
type aboutArea struct {
	version  string
	shareDir string
}

func (a aboutArea) register(mux *routeMux, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/about", user(a.handleAbout))
}

// handleAbout tells what is running and under which licenses: TitanOS
// itself and what it is built from. The texts lie in the image.
func (a aboutArea) handleAbout(w http.ResponseWriter, r *http.Request) {
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(a.shareDir, name))
		if err != nil {
			return ""
		}
		return string(data)
	}
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    a.version,
		"kernel":     strings.TrimSpace(string(kernel)),
		"license":    read("LICENSE.md"),
		"thirdParty": read("THIRD_PARTY_LICENSES"),
	})
}
