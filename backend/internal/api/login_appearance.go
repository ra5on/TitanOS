package api

// The look of the login page (Settings › Appearance › Login page). It is one
// setting for the whole NAS, read without login: the public answer carries
// only display values, the image route only the one configured image.
// Stored in /config/system/login-appearance.json (older titand ignore it);
// the image lies next to it, named by its digest. Both are written only
// when an owner or administrator saves.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	loginAppearanceFile = "system/login-appearance.json"
	// maxLoginImageBytes limits the uploaded login image.
	maxLoginImageBytes = 5 << 20
	// maxLoginImageEdge is the longest side the stored image keeps; larger
	// uploads are scaled down, so the stick holds at most a few MiB.
	maxLoginImageEdge = 3840
	// maxLoginImagePixels refuses decompression bombs before decoding.
	maxLoginImagePixels = 40_000_000
	maxLoginTitle       = 60
	maxLoginNotice      = 300
)

var (
	loginThemes      = []string{"inherit", "graphit", "schwarz", "mitternacht", "hell", "auto"}
	loginAccents     = []string{"blau", "gruen", "tuerkis", "violett", "kupfer", "rose", "custom"}
	loginStyles      = []string{"flat", "glass", "outline"}
	loginSources     = []string{"theme", "builtin", "custom"}
	loginPositions   = []string{"center", "left", "right"}
	loginWidths      = []string{"normal", "wide"}
	loginLocales     = []string{"de", "en"}
	loginBackgrounds = []string{"nordlicht", "nebelwald", "bergsee", "kueste", "wueste", "weltraum", "graphit", "mitternacht", "wellen", "geometrie"}
	loginHex         = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

type loginBackground struct {
	Source string `json:"source"`
	ID     string `json:"id,omitempty"`
	Fit    string `json:"fit"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Zoom   int    `json:"zoom"`
	Dim    int    `json:"dim"`
}

type loginTexts struct {
	Title  string `json:"title,omitempty"`
	Notice string `json:"notice,omitempty"`
}

// loginImage describes the stored image. Only titand sets it.
type loginImage struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int    `json:"bytes"`
}

// loginAppearance is the stored file and, field for field, the public answer.
type loginAppearance struct {
	Theme        string                `json:"theme"`
	Accent       string                `json:"accent"`
	CustomAccent string                `json:"customAccent"`
	Style        string                `json:"style"`
	Glass        int                   `json:"glass"`
	Blur         int                   `json:"blur"`
	Background   loginBackground       `json:"background"`
	Image        *loginImage           `json:"image"`
	Position     string                `json:"position"`
	Width        string                `json:"width"`
	Texts        map[string]loginTexts `json:"texts"`
	HideHost     bool                  `json:"hideHost"`
	HideLogo     bool                  `json:"hideLogo"`
	Clock        bool                  `json:"clock"`
}

// loginAppearanceRequest is what the editor sends; the image is managed here.
type loginAppearanceRequest struct {
	loginAppearance
	// RemoveImage drops the stored image (only when it is not in use).
	RemoveImage bool `json:"removeImage"`
}

func defaultLoginAppearance() loginAppearance {
	return loginAppearance{
		Theme: "inherit", Accent: "blau", CustomAccent: "#E5B92E", Style: "glass", Glass: 45, Blur: 18,
		Background: loginBackground{Source: "theme", Fit: "cover", X: 50, Y: 50, Zoom: 100, Dim: 30},
		Position:   "center", Width: "normal", Texts: map[string]loginTexts{},
	}
}

// loginLook holds the setting in memory: the stick is read once.
type loginLook struct {
	mu     sync.Mutex
	loaded bool
	exists bool
	value  loginAppearance
	image  []byte
}

func loginImageFile(id string) string { return "system/login-background-" + id }

func cleanText(value string, most int) (string, bool) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > most {
		return "", false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", false
		}
	}
	return value, true
}

// validate checks every value strictly; nothing is silently corrected.
func (v *loginAppearance) validate() error {
	in := func(value string, allowed []string) bool { return slices.Contains(allowed, value) }
	rng := func(n, lo, hi int) bool { return n >= lo && n <= hi }
	b := &v.Background
	switch {
	case !in(v.Theme, loginThemes):
		return errors.New("unknown theme")
	case !in(v.Accent, loginAccents):
		return errors.New("unknown accent")
	case !loginHex.MatchString(v.CustomAccent):
		return errors.New("invalid custom accent")
	case !in(v.Style, loginStyles):
		return errors.New("unknown surface style")
	case !rng(v.Glass, 0, 90) || !rng(v.Blur, 0, 40):
		return errors.New("glass values out of range")
	case !in(b.Source, loginSources):
		return errors.New("unknown background")
	case b.Source == "builtin" && !in(b.ID, loginBackgrounds):
		return errors.New("unknown gallery motif")
	case b.Source != "builtin" && b.ID != "":
		return errors.New("background id without gallery motif")
	case b.Fit != "cover" && b.Fit != "contain":
		return errors.New("unknown fit")
	case !rng(b.X, 0, 100) || !rng(b.Y, 0, 100) || !rng(b.Zoom, 100, 200) || !rng(b.Dim, 0, 80):
		return errors.New("background values out of range")
	case !in(v.Position, loginPositions):
		return errors.New("unknown card position")
	case !in(v.Width, loginWidths):
		return errors.New("unknown card width")
	}
	v.CustomAccent = strings.ToUpper(v.CustomAccent)
	texts := map[string]loginTexts{}
	for locale, text := range v.Texts {
		if !in(locale, loginLocales) {
			return errors.New("unknown language")
		}
		title, ok := cleanText(text.Title, maxLoginTitle)
		if !ok {
			return errors.New("invalid title")
		}
		notice, ok := cleanText(text.Notice, maxLoginNotice)
		if !ok {
			return errors.New("invalid notice")
		}
		if title != "" || notice != "" {
			texts[locale] = loginTexts{Title: title, Notice: notice}
		}
	}
	v.Texts = texts
	return nil
}

// current returns the setting, reading the stick only the first time.
func (s *Server) loginAppearance() (loginAppearance, bool) {
	l := &s.loginLook
	l.mu.Lock()
	defer l.mu.Unlock()
	s.loadLoginLookLocked()
	return l.value, l.exists
}

func (s *Server) loadLoginLookLocked() {
	l := &s.loginLook
	if l.loaded {
		return
	}
	l.value = defaultLoginAppearance()
	data, err := s.opts.Store.Read(loginAppearanceFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// Try again on the next request; until then the default look.
			slog.Warn("read login appearance", "err", err)
			return
		}
		l.loaded = true
		return
	}
	l.loaded = true
	value := defaultLoginAppearance()
	if json.Unmarshal(data, &value) != nil || value.validate() != nil {
		slog.Warn("ignoring damaged login appearance")
		return
	}
	if value.Image != nil {
		image, err := s.opts.Store.Read(loginImageFile(value.Image.ID))
		if err != nil || digest(image) != value.Image.ID {
			slog.Warn("login image missing or damaged")
			value.Image = nil
		} else {
			l.image = image
		}
	}
	if value.Background.Source == "custom" && value.Image == nil {
		value.Background.Source = "theme"
	}
	l.value, l.exists = value, true
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// handleLoginAppearance answers without login: display values only.
func (s *Server) handleLoginAppearance(w http.ResponseWriter, r *http.Request) {
	value, exists := s.loginAppearance()
	writeJSON(w, http.StatusOK, struct {
		Configured bool `json:"configured"`
		loginAppearance
	}{exists, value})
}

// handleLoginBackground serves the configured image and nothing else.
func (s *Server) handleLoginBackground(w http.ResponseWriter, r *http.Request) {
	l := &s.loginLook
	l.mu.Lock()
	s.loadLoginLookLocked()
	image, value := l.image, l.value
	l.mu.Unlock()
	if value.Image == nil || r.PathValue("id") != value.Image.ID || image == nil {
		writeError(w, http.StatusNotFound, "not_found", "no login image")
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(image))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Write(image)
}

// handlePutLoginAppearance saves the setting: JSON, or multipart with the
// fields "settings" and "image".
func (s *Server) handlePutLoginAppearance(w http.ResponseWriter, r *http.Request) {
	var raw, upload []byte
	if isMultipartImageUpload(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginImageBytes+maxJSONBody+(64<<10))
		reader, err := r.MultipartReader()
		if err != nil {
			writeError(w, 400, "bad_request", "invalid upload")
			return
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeError(w, 400, "bad_request", "invalid upload")
				return
			}
			switch part.FormName() {
			case "settings":
				if raw != nil {
					writeError(w, 400, "bad_request", "duplicate settings")
					return
				}
				raw, err = io.ReadAll(io.LimitReader(part, maxJSONBody+1))
				if err != nil || len(raw) > maxJSONBody {
					writeError(w, 413, "too_large", "settings are too large")
					return
				}
			case "image":
				if upload != nil {
					writeError(w, 400, "bad_request", "only one image")
					return
				}
				upload, err = io.ReadAll(io.LimitReader(part, maxLoginImageBytes+1))
				if err != nil || len(upload) > maxLoginImageBytes {
					writeError(w, 413, "too_large", "the image is larger than 5 MiB")
					return
				}
			default:
				writeError(w, 400, "bad_request", "unknown upload field")
				return
			}
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		var err error
		if raw, err = io.ReadAll(r.Body); err != nil {
			writeError(w, 413, "too_large", "request body too large")
			return
		}
	}
	req := loginAppearanceRequest{loginAppearance: defaultLoginAppearance()}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if raw == nil || decoder.Decode(&req) != nil {
		writeError(w, 400, "bad_request", "settings are not valid JSON")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, 400, "invalid_login_appearance", err.Error())
		return
	}
	var image []byte
	var meta *loginImage
	if upload != nil {
		var err error
		if image, meta, err = reencodeLoginImage(upload); err != nil {
			writeError(w, 415, "unsupported_type", err.Error())
			return
		}
	}

	l := &s.loginLook
	l.mu.Lock()
	defer l.mu.Unlock()
	s.loadLoginLookLocked()
	if !l.loaded {
		writeError(w, 503, "config_unavailable", "/config could not be read")
		return
	}
	old := l.value.Image
	next := req.loginAppearance
	switch {
	case meta != nil:
		next.Image = meta
	case req.RemoveImage:
		next.Image = nil
	default:
		next.Image = old
	}
	if next.Background.Source == "custom" && next.Image == nil {
		writeError(w, 400, "invalid_login_appearance", "the own image is missing")
		return
	}
	if meta != nil && (old == nil || old.ID != meta.ID) {
		if err := s.opts.Store.Write(loginImageFile(meta.ID), image); err != nil {
			slog.Error("write login image", "err", err)
			writeError(w, 500, "config_write_failed", "the image could not be saved to /config")
			return
		}
	}
	if err := s.opts.Store.WriteJSON(loginAppearanceFile, next); err != nil {
		slog.Error("write login appearance", "err", err)
		if meta != nil && (old == nil || old.ID != meta.ID) {
			_ = s.opts.Store.Remove(loginImageFile(meta.ID))
		}
		writeError(w, 500, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	if old != nil && (next.Image == nil || next.Image.ID != old.ID) {
		_ = s.opts.Store.Remove(loginImageFile(old.ID))
	}
	if meta != nil {
		l.image = image
	} else if next.Image == nil {
		l.image = nil
	}
	l.value, l.exists = next, true
	noteTarget(r, "login-appearance")
	writeJSON(w, http.StatusOK, struct {
		Configured bool `json:"configured"`
		loginAppearance
	}{true, next})
}

// handleDeleteLoginAppearance goes back to the default look.
func (s *Server) handleDeleteLoginAppearance(w http.ResponseWriter, r *http.Request) {
	l := &s.loginLook
	l.mu.Lock()
	defer l.mu.Unlock()
	s.loadLoginLookLocked()
	if err := s.opts.Store.Remove(loginAppearanceFile); err != nil {
		writeError(w, 500, "config_write_failed", "the settings could not be removed from /config")
		return
	}
	if l.value.Image != nil {
		_ = s.opts.Store.Remove(loginImageFile(l.value.Image.ID))
	}
	l.loaded, l.exists, l.value, l.image = true, false, defaultLoginAppearance(), nil
	noteTarget(r, "login-appearance")
	w.WriteHeader(http.StatusNoContent)
}

// reencodeLoginImage decodes an upload and writes it anew: only pixels are
// kept (no metadata such as location data), large images are scaled down.
func reencodeLoginImage(data []byte) ([]byte, *loginImage, error) {
	kind := http.DetectContentType(data)
	if !slices.Contains(backgroundTypes, kind) {
		return nil, nil, errors.New("the image must be JPEG, PNG or WebP")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width*config.Height > maxLoginImagePixels {
		return nil, nil, errors.New("the image could not be read or is too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, errors.New("the image could not be read")
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if edge := max(width, height); edge > maxLoginImageEdge {
		width, height = max(1, width*maxLoginImageEdge/edge), max(1, height*maxLoginImageEdge/edge)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), img, bounds, xdraw.Src, nil)
	var out bytes.Buffer
	if canvas.Opaque() {
		rgb := image.NewRGBA(canvas.Bounds())
		xdraw.Draw(rgb, rgb.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, xdraw.Src)
		xdraw.Draw(rgb, rgb.Bounds(), canvas, image.Point{}, xdraw.Over)
		err = jpeg.Encode(&out, rgb, &jpeg.Options{Quality: 88})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, canvas)
	}
	if err != nil {
		return nil, nil, errors.New("the image could not be saved")
	}
	encoded := out.Bytes()
	return encoded, &loginImage{ID: digest(encoded), Width: width, Height: height, Bytes: len(encoded)}, nil
}
