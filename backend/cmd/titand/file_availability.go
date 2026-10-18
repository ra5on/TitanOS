package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/usb"
)

// USB roots have no internal Volume name. Match their current mount as well as
// their root ID; a leftover directory after ejection is never an upload target.
func fileRootAvailable(root files.Root, volumes []string, drives []usb.Mounted) bool {
	if root.Kind == "usb" {
		return slices.ContainsFunc(drives, func(d usb.Mounted) bool {
			return root.ID == "usb/"+d.Mount && filepath.Clean(root.Dir) == filepath.Clean(d.Dir)
		})
	}
	return (root.Kind == "volume" || root.Kind == "share") && root.Volume != "" && strings.HasPrefix(root.ID, root.Kind+"/") && slices.Contains(volumes, root.Volume)
}
