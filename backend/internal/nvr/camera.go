package nvr

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	camerasFile  = "nvr/cameras.json"
	secretsFile  = "nvr/secrets.json"
	maxCameras   = 32
	maxNameLen   = 60
	DefaultKeep  = 7  // days
	DefaultFree  = 20 // GiB kept free
	maxKeepDays  = 3650
	maxReserveGB = 100000
)

var (
	ErrInvalid  = errors.New("nvr: invalid camera")
	ErrNotFound = errors.New("nvr: no such camera")
)

// Camera is one configured camera. The password is not part of it: it lives
// in its own file and never leaves the server.
type Camera struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	User    string `json:"user"`
	Vendor  string `json:"vendor,omitempty"`
	MainURL string `json:"mainUrl"`
	SubURL  string `json:"subUrl,omitempty"`
	// Record switches recording of the main stream on.
	Record bool `json:"record"`
	// Audio is off unless the person asks for it.
	Audio bool `json:"audio"`
	// ONVIFPort is the port ONVIF answered on at the search, 0 if none did.
	ONVIFPort int `json:"onvifPort,omitempty"`
	// TimeSync is set once the camera was pointed at this NAS as its time server.
	TimeSync bool `json:"timeSync"`
	// KeepDays overrides the general retention; 0 means the general value.
	KeepDays int `json:"keepDays,omitempty"`
}

// Settings are the general NVR settings.
type Settings struct {
	// Enabled is the service switch (Settings › Services).
	Enabled bool `json:"enabled"`
	// Volume holds the recordings, below its "nvr" folder. Empty: not chosen.
	Volume string `json:"volume,omitempty"`
	// KeepDays is the retention for cameras without their own value.
	KeepDays int `json:"keepDays"`
	// ReserveGB is the space kept free; the oldest recordings go first when
	// it runs short.
	ReserveGB int `json:"reserveGB"`
}

type state struct {
	Settings Settings `json:"settings"`
	Cameras  []Camera `json:"cameras"`
}

// Store keeps the cameras and their passwords.
type Store struct {
	mu      sync.Mutex
	cfg     *config.Store
	loaded  bool
	st      state
	secrets map[string]string
}

func NewStore(cfg *config.Store) *Store { return &Store{cfg: cfg} }

func (s *Store) load() {
	if s.loaded {
		return
	}
	s.st = state{Settings: Settings{KeepDays: DefaultKeep, ReserveGB: DefaultFree}}
	if err := s.cfg.ReadJSON(camerasFile, &s.st); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return // unreadable: stay unloaded so nothing is overwritten
	}
	s.secrets = map[string]string{}
	if err := s.cfg.ReadJSON(secretsFile, &s.secrets); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if s.st.Settings.KeepDays <= 0 {
		s.st.Settings.KeepDays = DefaultKeep
	}
	s.loaded = true
}

func (s *Store) save() error {
	if err := s.cfg.WriteJSON(secretsFile, s.secrets); err != nil {
		return err
	}
	return s.cfg.WriteJSON(camerasFile, s.st)
}

// Settings returns the general settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.st.Settings
}

// SetSettings validates and stores the general settings.
func (s *Store) SetSettings(v Settings) error {
	if v.KeepDays < 1 || v.KeepDays > maxKeepDays || v.ReserveGB < 0 || v.ReserveGB > maxReserveGB {
		return fmt.Errorf("%w: retention or reserve out of range", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	s.st.Settings = v
	return s.save()
}

// Cameras returns all cameras.
func (s *Store) Cameras() []Camera {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return slices.Clone(s.st.Cameras)
}

// Camera returns one camera.
func (s *Store) Camera(id string) (Camera, bool) {
	for _, c := range s.Cameras() {
		if c.ID == id {
			return c, true
		}
	}
	return Camera{}, false
}

// Password returns the camera's password for the server's own use.
func (s *Store) Password(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.secrets[id]
}

// Full returns the stream address with credentials, for ffmpeg only.
func (s *Store) Full(c Camera, sub bool) string {
	raw := c.MainURL
	if sub && c.SubURL != "" {
		raw = c.SubURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if c.User != "" {
		u.User = url.UserPassword(c.User, s.Password(c.ID))
	}
	return u.String()
}

func validateCamera(c Camera) error {
	name := strings.TrimSpace(c.Name)
	if name == "" || len([]rune(name)) > maxNameLen || strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	for _, raw := range []string{c.MainURL, c.SubURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("%w: stream address", ErrInvalid)
		}
	}
	if c.MainURL == "" {
		return fmt.Errorf("%w: stream address", ErrInvalid)
	}
	if c.KeepDays < 0 || c.KeepDays > maxKeepDays {
		return fmt.Errorf("%w: retention", ErrInvalid)
	}
	return nil
}

// Add stores a new camera.
func (s *Store) Add(c Camera, password string) (Camera, error) {
	c.Name = strings.TrimSpace(c.Name)
	if err := validateCamera(c); err != nil {
		return Camera{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return Camera{}, errors.New("nvr: settings not loaded from /config")
	}
	if len(s.st.Cameras) >= maxCameras {
		return Camera{}, fmt.Errorf("%w: too many cameras", ErrInvalid)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	c.ID = "cam-" + hex.EncodeToString(b)
	s.st.Cameras = append(s.st.Cameras, c)
	if s.secrets == nil {
		s.secrets = map[string]string{}
	}
	s.secrets[c.ID] = password
	if err := s.save(); err != nil {
		s.st.Cameras = s.st.Cameras[:len(s.st.Cameras)-1]
		delete(s.secrets, c.ID)
		return Camera{}, err
	}
	return c, nil
}

// Update replaces a camera; a nil password keeps the stored one.
func (s *Store) Update(c Camera, password *string) (Camera, error) {
	c.Name = strings.TrimSpace(c.Name)
	if err := validateCamera(c); err != nil {
		return Camera{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return Camera{}, errors.New("nvr: settings not loaded from /config")
	}
	for i := range s.st.Cameras {
		if s.st.Cameras[i].ID == c.ID {
			old, oldPass := s.st.Cameras[i], s.secrets[c.ID]
			s.st.Cameras[i] = c
			if password != nil {
				s.secrets[c.ID] = *password
			}
			if err := s.save(); err != nil {
				s.st.Cameras[i], s.secrets[c.ID] = old, oldPass
				return Camera{}, err
			}
			return c, nil
		}
	}
	return Camera{}, ErrNotFound
}

// Remove deletes a camera and its password. Recordings stay until retention
// removes them.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	for i := range s.st.Cameras {
		if s.st.Cameras[i].ID == id {
			s.st.Cameras = slices.Delete(s.st.Cameras, i, i+1)
			delete(s.secrets, id)
			return s.save()
		}
	}
	return ErrNotFound
}
