// Package viewer runs the remote view of Docker apps (Fernansicht, PLAN 63,
// docs/FERNANSICHT.md): a browser per account and app in a container of
// its own, whose picture and sound titand relays to the app window.
//
// This package owns the titand side: settings, the signed image of the
// release, the containers and their network, and the checks of messages
// on their way to the agent in the container. The routes are in
// api/viewer.go; the agent and its image are in backend/cmd/titanos-viewer
// and viewer/.
package viewer

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// settingsFile is the optional file of the settings; older titand ignore it.
const settingsFile = "viewer/settings.json"

// Settings are the owner's choices for the remote view.
type Settings struct {
	// GPU is "auto", "off" or the id of a detected GPU (GPU.ID).
	GPU string `json:"gpu"`
	// Quality is "auto", "high", "balanced" or "low".
	Quality string `json:"quality"`
	// FPS is 30 or 60.
	FPS   int  `json:"fps"`
	Audio bool `json:"audio"`
	// Volume holds the image and the browser profiles; "" is Docker's volume.
	Volume string `json:"volume"`
}

// ErrInvalid is a setting outside the allowed values.
var ErrInvalid = errors.New("viewer: invalid setting")

// Defaults are the settings without a file.
func Defaults() Settings {
	return Settings{GPU: "auto", Quality: "auto", FPS: 30, Audio: true}
}

var volumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Validate checks settings against the GPUs that were found and the
// volumes that are mounted. A GPU that is not found is refused: the
// setting never names an arbitrary device path.
func Validate(s Settings, gpus []GPU, volumes []string) error {
	switch s.GPU {
	case "auto", "off":
	default:
		if !slices.ContainsFunc(gpus, func(g GPU) bool { return g.ID == s.GPU }) {
			return fmt.Errorf("%w: unknown GPU %q", ErrInvalid, s.GPU)
		}
	}
	if !slices.Contains([]string{"auto", "high", "balanced", "low"}, s.Quality) {
		return fmt.Errorf("%w: quality", ErrInvalid)
	}
	if s.FPS != 30 && s.FPS != 60 {
		return fmt.Errorf("%w: fps must be 30 or 60", ErrInvalid)
	}
	if s.Volume != "" && (!volumeName.MatchString(s.Volume) || !slices.Contains(volumes, s.Volume)) {
		return fmt.Errorf("%w: volume %q is not mounted", ErrInvalid, s.Volume)
	}
	return nil
}

// normalize fills fields an older or hand-edited file left empty.
func normalize(s Settings) Settings {
	d := Defaults()
	if s.GPU == "" {
		s.GPU = d.GPU
	}
	if s.Quality == "" {
		s.Quality = d.Quality
	}
	if s.FPS == 0 {
		s.FPS = d.FPS
	}
	return s
}
