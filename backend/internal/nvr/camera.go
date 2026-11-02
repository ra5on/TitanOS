package nvr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
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
	// Record switches recording of the main stream on (0.46.0); RecordMode
	// replaces it when set.
	Record bool `json:"record"`
	// RecordMode is "off", "continuous" or "events" (only what lies around
	// an event is kept; needs the motion check, which then runs).
	RecordMode string `json:"recordMode,omitempty"`
	// EventKeepDays overrides how long events and their pictures stay.
	EventKeepDays int `json:"eventKeepDays,omitempty"`
	// Audio is off unless the person asks for it.
	Audio bool `json:"audio"`
	// ONVIFPort is the port ONVIF answered on at the search, 0 if none did.
	ONVIFPort int `json:"onvifPort,omitempty"`
	// Detect switches the motion check on (and with it events).
	Detect bool `json:"detect"`
	// Zones limit the motion check to these areas (empty: the whole picture);
	// Masks are left out of it (trees, a street, the neighbour's garden).
	Zones []Area `json:"zones,omitempty"`
	Masks []Area `json:"masks,omitempty"`
	// Viewers are accounts without admin rights that may watch this camera
	// (live, recordings, events). Administrators always may.
	Viewers []string `json:"viewers,omitempty"`
	// Sensitivity of the motion check, 1 (hard) to 100 (easy); 0 means 50.
	Sensitivity int `json:"sensitivity,omitempty"`
	// QualityOriginal keeps the video setting a stream had before TitanOS
	// first changed it, so the change can be undone. Set by the server only.
	QualityOriginal map[string]QualityChange `json:"qualityOriginal,omitempty"`
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
	// FormerVolumes held recordings before; their retention still runs.
	FormerVolumes []string `json:"formerVolumes,omitempty"`
	// KeepDays is the retention for cameras without their own value.
	KeepDays int `json:"keepDays"`
	// Armed is the switch of 0.46.0; it is read once into Arming.
	Armed bool `json:"armed,omitempty"`
	// Arming decides when movement is reported. Off by default.
	Arming Arming `json:"arming"`
	// EventKeepDays is how long events and their pictures stay; 0 means 30.
	EventKeepDays int `json:"eventKeepDays,omitempty"`
	// ReserveGB is the space kept free; the oldest recordings go first when
	// it runs short.
	ReserveGB int `json:"reserveGB"`
}

type state struct {
	Settings Settings `json:"settings"`
	Cameras  []Camera `json:"cameras"`
	Views    []View   `json:"views,omitempty"`
}

// Store keeps the cameras and their passwords.
type Store struct {
	// Allow decides which camera addresses ffmpeg may reach; nil means
	// AllowedAddr. For tests only.
	Allow func(netip.Addr) bool

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
	// Both files are read before anything is used: cameras without their
	// passwords would be tried with empty ones (and lock the account).
	st := state{Settings: Settings{KeepDays: DefaultKeep, ReserveGB: DefaultFree}}
	if err := s.cfg.ReadJSON(camerasFile, &st); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return // unreadable: stay unloaded, show nothing, overwrite nothing
	}
	secrets := map[string]string{}
	if err := s.cfg.ReadJSON(secretsFile, &secrets); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	s.st, s.secrets = st, secrets
	if s.st.Settings.KeepDays <= 0 {
		s.st.Settings.KeepDays = DefaultKeep
	}
	if s.st.Settings.Arming.Mode == "" {
		s.st.Settings.Arming.Mode = "off"
		if s.st.Settings.Armed {
			s.st.Settings.Arming.Mode = "on"
		}
	}
	s.st.Settings.Armed = false
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
	if v.EventKeepDays < 0 || v.EventKeepDays > maxKeepDays {
		return fmt.Errorf("%w: retention out of range", ErrInvalid)
	}
	if v.KeepDays < 1 || v.KeepDays > maxKeepDays || v.ReserveGB < 0 || v.ReserveGB > maxReserveGB {
		return fmt.Errorf("%w: retention or reserve out of range", ErrInvalid)
	}
	if err := v.Arming.Validate(); err != nil {
		return err
	}
	if v.Arming.Mode == "" {
		v.Arming.Mode = "off"
	}
	v.Armed = false
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	if old := s.st.Settings.Volume; old != "" && old != v.Volume && !slices.Contains(v.FormerVolumes, old) {
		v.FormerVolumes = append(v.FormerVolumes, old)
	}
	v.FormerVolumes = slices.DeleteFunc(v.FormerVolumes, func(x string) bool { return x == v.Volume })
	if len(v.FormerVolumes) > 8 {
		v.FormerVolumes = v.FormerVolumes[len(v.FormerVolumes)-8:]
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
	if c.Sensitivity < 0 || c.Sensitivity > 100 {
		return fmt.Errorf("%w: sensitivity", ErrInvalid)
	}
	if len(c.Zones) > maxAreas || len(c.Masks) > maxAreas || len(c.Viewers) > 64 {
		return fmt.Errorf("%w: too many areas or viewers", ErrInvalid)
	}
	for _, a := range append(slices.Clone(c.Zones), c.Masks...) {
		if !a.valid() {
			return fmt.Errorf("%w: area", ErrInvalid)
		}
	}
	switch c.RecordMode {
	case "", "off", "continuous", "events":
	default:
		return fmt.Errorf("%w: recording mode", ErrInvalid)
	}
	if c.EventKeepDays < 0 || c.EventKeepDays > maxKeepDays {
		return fmt.Errorf("%w: retention", ErrInvalid)
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
			// Fields the server owns. The time setting belongs to the device:
			// a new address ("replace camera") starts without it.
			c.QualityOriginal = old.QualityOriginal
			sameDevice := hostOf(old.MainURL) == hostOf(c.MainURL)
			c.TimeSync = old.TimeSync && sameDevice
			if !sameDevice {
				c.QualityOriginal = nil
			}
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
			for vi := range s.st.Views {
				// Cloned first: copies handed out by Views share the array.
				s.st.Views[vi].Tiles = slices.DeleteFunc(slices.Clone(s.st.Views[vi].Tiles), func(t Tile) bool { return t.Camera == id })
			}
			s.st.Settings.Arming.Cameras = slices.DeleteFunc(slices.Clone(s.st.Settings.Arming.Cameras), func(x string) bool { return x == id })
			return s.save()
		}
	}
	return ErrNotFound
}

// Views are arrangements of live pictures. The grid has a fixed number of
// columns; a cell that no camera covers stays empty.
const (
	ViewColumns = 6
	maxViews    = 12
	maxTiles    = 24
	maxRows     = 12
)

// Tile places one camera in a view: columns X..X+W-1 of the grid, rows
// Y..Y+H-1 (all counted from 0).
type Tile struct {
	Camera string `json:"camera"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
}

// View is a named arrangement.
type View struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tiles []Tile `json:"tiles"`
}

// Views returns the saved views.
func (s *Store) Views() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return slices.Clone(s.st.Views)
}

// SetViews replaces all views after checking them: known cameras, tiles
// inside the grid and not overlapping.
func (s *Store) SetViews(views []View) error {
	if len(views) > maxViews {
		return fmt.Errorf("%w: too many views", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	known := map[string]bool{}
	for _, c := range s.st.Cameras {
		known[c.ID] = true
	}
	ids := map[string]bool{}
	out := make([]View, 0, len(views))
	for _, v := range views {
		v.Name = strings.TrimSpace(v.Name)
		if v.Name == "" || len([]rune(v.Name)) > maxNameLen || strings.ContainsAny(v.Name, "\x00\r\n") {
			return fmt.Errorf("%w: view name", ErrInvalid)
		}
		if v.ID == "" {
			b := make([]byte, 5)
			_, _ = rand.Read(b)
			v.ID = "view-" + hex.EncodeToString(b)
		}
		if ids[v.ID] {
			return fmt.Errorf("%w: view id", ErrInvalid)
		}
		ids[v.ID] = true
		if len(v.Tiles) > maxTiles {
			return fmt.Errorf("%w: too many tiles", ErrInvalid)
		}
		for i, t := range v.Tiles {
			if !known[t.Camera] || t.W < 1 || t.H < 1 || t.W > ViewColumns || t.H > maxRows || t.X < 0 || t.Y < 0 || t.X >= ViewColumns || t.Y >= maxRows || t.X+t.W > ViewColumns || t.Y+t.H > maxRows {
				return fmt.Errorf("%w: tile", ErrInvalid)
			}
			for _, o := range v.Tiles[:i] {
				if t.X < o.X+o.W && o.X < t.X+t.W && t.Y < o.Y+o.H && o.Y < t.Y+t.H {
					return fmt.Errorf("%w: tiles overlap", ErrInvalid)
				}
			}
		}
		if v.Tiles == nil {
			v.Tiles = []Tile{}
		}
		out = append(out, v)
	}
	old := s.st.Views
	s.st.Views = out
	if err := s.save(); err != nil {
		s.st.Views = old
		return err
	}
	return nil
}

// SetQualityOriginal records or clears (nil) the original setting of a stream.
func (s *Store) SetQualityOriginal(id, token string, orig *QualityChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	for i := range s.st.Cameras {
		c := &s.st.Cameras[i]
		if c.ID != id {
			continue
		}
		old := c.QualityOriginal
		next := map[string]QualityChange{}
		for k, v := range old {
			next[k] = v
		}
		if orig == nil {
			delete(next, token)
		} else if _, kept := next[token]; !kept {
			next[token] = *orig
		}
		if len(next) == 0 {
			next = nil
		}
		c.QualityOriginal = next
		if err := s.save(); err != nil {
			c.QualityOriginal = old
			return err
		}
		return nil
	}
	return ErrNotFound
}

// DefaultEventKeep is how long events stay without a setting, in days.
const DefaultEventKeep = 30

// Mode is the effective recording mode.
func (c Camera) Mode() string {
	switch c.RecordMode {
	case "off", "continuous", "events":
		return c.RecordMode
	}
	if c.Record {
		return "continuous"
	}
	return "off"
}

// Watched reports whether the motion check runs for the camera.
func (c Camera) Watched() bool { return c.Detect || c.Mode() == "events" }

// EventKeep is the retention of events in days.
func (c Camera) EventKeep(st Settings) int {
	switch {
	case c.EventKeepDays > 0:
		return c.EventKeepDays
	case st.EventKeepDays > 0:
		return st.EventKeepDays
	}
	return DefaultEventKeep
}

// Keep is the retention of recordings in days.
func (c Camera) Keep(st Settings) int {
	if c.KeepDays > 0 {
		return c.KeepDays
	}
	return st.KeepDays
}

// Pinned is the stream address for ffmpeg: credentials added, the host
// resolved now and checked against the home network, and for plain RTSP the
// name replaced by the address, so a name that changes its address later
// (DNS rebinding) cannot send ffmpeg elsewhere.
func (s *Store) Pinned(ctx context.Context, c Camera, sub bool) (string, error) {
	raw := c.MainURL
	if sub && c.SubURL != "" {
		raw = c.SubURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("%w: stream address", ErrInvalid)
	}
	host := u.Hostname()
	addr, err := netip.ParseAddr(host)
	if err != nil {
		addrs, err := Resolve(ctx, nil, host)
		if err != nil {
			return "", err
		}
		addr = addrs[0]
	}
	allow := AllowedAddr
	if s.Allow != nil {
		allow = s.Allow
	}
	if !allow(addr.Unmap()) {
		return "", fmt.Errorf("%w: %s", ErrTargetNotAllowed, addr)
	}
	if u.Scheme == "rtsp" {
		u.Host = hostPort(addr.Unmap().String(), portOf(u))
	}
	if c.User != "" {
		u.User = url.UserPassword(c.User, s.Password(c.ID))
	}
	return u.String(), nil
}

func portOf(u *url.URL) int {
	p, _ := strconv.Atoi(u.Port())
	return p
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// SetTimeSync marks a camera as taking its time from the NAS, changing
// nothing else (the request that set it ran for a while).
func (s *Store) SetTimeSync(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if !s.loaded {
		return errors.New("nvr: settings not loaded from /config")
	}
	for i := range s.st.Cameras {
		if s.st.Cameras[i].ID == id {
			old := s.st.Cameras[i].TimeSync
			s.st.Cameras[i].TimeSync = on
			if err := s.save(); err != nil {
				s.st.Cameras[i].TimeSync = old
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

// Area is a rectangle in the picture, in shares of width and height (0..1),
// so it fits main and sub stream alike.
type Area struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

const maxAreas = 16

func (a Area) valid() bool {
	return a.X >= 0 && a.Y >= 0 && a.W > 0 && a.H > 0 && a.X+a.W <= 1.0001 && a.Y+a.H <= 1.0001
}

// MayView reports whether an account may watch the camera; administrators
// are checked by the caller.
func (c Camera) MayView(user string) bool { return slices.Contains(c.Viewers, user) }
