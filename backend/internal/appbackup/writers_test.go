package appbackup

import (
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"testing"
)

func TestWriterOverlapUsesComponentBoundariesAndWritableGuestFolders(t *testing.T) {
	paths := vmWriterPaths("/mnt", vm.Status{VM: vm.VM{Name: "guest", Volume: "data", Folders: []vm.Folder{{Path: "/mnt/data/shared", ReadOnly: false}, {Path: "/mnt/data/read-only", ReadOnly: true}}}})
	if !writerOverlaps("/mnt/data/shared/app", paths) {
		t.Fatal("missed writable guest share", paths)
	}
	if writerOverlaps("/mnt/data/read-only", paths) || writerOverlaps("/mnt/data/shared-other", paths) {
		t.Fatal("matched read-only or similarly named sibling", paths)
	}
}
