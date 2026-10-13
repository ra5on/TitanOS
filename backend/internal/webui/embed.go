//go:build embedui

package webui

import (
	"embed"
	"io/fs"
)

// dist is the output of the web build (see web/svelte.config.js).
//
//go:embed all:dist
var dist embed.FS

// Embedded reports whether the real web UI is part of this binary.
const Embedded = true

// FS returns the files of the web UI.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
