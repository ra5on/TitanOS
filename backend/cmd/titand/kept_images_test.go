package main

import (
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

// N-102-29: the Docker cleanup learns from the NVR and speech which images
// they keep while they are set up (a volume chosen), switched on or off,
// and nothing while they were never set up.
func TestKeptImagesFollowTheFeatures(t *testing.T) {
	store := config.Open(filepath.Join(t.TempDir(), "config"))
	recorder, voices := nvr.New(store), speech.New(store)
	keep := keptImages(recorder, voices)
	if got := keep(); len(got) != 0 {
		t.Fatalf("kept while off: %v", got)
	}
	// Set up and switched off again: the containers are gone, the images stay.
	if err := store.WriteJSON("nvr/settings.json", map[string]any{"enabled": false, "volume": "tank"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteJSON("system/speech.json", map[string]any{"enabled": false, "volume": "tank", "device": speech.DeviceCPU, "language": "auto", "speed": 1, "reach": "ha"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Load(); err != nil {
		t.Fatal(err)
	}
	if err := voices.Load(); err != nil {
		t.Fatal(err)
	}
	got := keep()
	want := map[string]string{nvr.FrigateDigest: "nvr", speech.WhisperDigest: "speech", speech.PiperDigest: "speech"}
	if len(got) != len(want) {
		t.Fatalf("kept: %v", got)
	}
	for digest, reason := range want {
		if got[digest] != reason {
			t.Errorf("%s: %q, want %q", digest, got[digest], reason)
		}
	}
}
