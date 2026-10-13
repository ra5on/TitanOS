//go:build !embedui

// Package webui provides the files of the web UI.
//
// Built with the tag "embedui" it contains the output of the web build.
// Without the tag (go test, go run during development) it only contains a
// placeholder page, so the backend builds without Node.js.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed placeholder
var placeholder embed.FS

// Embedded reports whether the real web UI is part of this binary.
const Embedded = false

// FS returns the files of the web UI.
func FS() fs.FS {
	sub, err := fs.Sub(placeholder, "placeholder")
	if err != nil {
		panic(err)
	}
	return sub
}
