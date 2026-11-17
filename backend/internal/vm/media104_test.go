package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A large first volume does not hide the media and images of the others:
// each volume has its own budget of entries to look at.
func TestEachVolumeHasItsOwnWalkBudget(t *testing.T) {
	limit := walkLimit
	walkLimit = 20
	t.Cleanup(func() { walkLimit = limit })
	m, _ := newTestManager(t)
	big := filepath.Join(m.mountRoot, "data", "lots")
	os.MkdirAll(big, 0o755)
	for i := 0; i < 40; i++ {
		os.WriteFile(filepath.Join(big, fmt.Sprintf("file%02d.txt", i)), nil, 0o644)
	}
	iso := filepath.Join(m.mountRoot, "fast", "debian.iso")
	os.WriteFile(iso, []byte("iso"), 0o644)
	image := filepath.Join(m.mountRoot, "fast", "disk.qcow2")
	os.WriteFile(image, []byte("disk"), 0o644)
	media := m.Media([]string{"data", "fast"})
	if len(media) != 1 || media[0].Path != iso {
		t.Errorf("the ISO on the second volume is missing: %v", media)
	}
	images := m.Images(context.Background(), []string{"data", "fast"})
	if len(images) != 1 || images[0].Path != image {
		t.Errorf("the image on the second volume is missing: %v", images)
	}
}
