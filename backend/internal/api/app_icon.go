package api

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// The symbol of TitanOS on the home screen and in notifications (Settings ›
// Appearance › App symbol, for the whole NAS): the default, the logo on a
// color of one's own, or an own picture. titand draws the PNG files the
// manifest names (/icons/app-180|192|512.png) itself when one is chosen;
// otherwise the built-in files of the web UI answer. iOS takes the symbol
// when TitanOS is added to the home screen and keeps it.

const (
	appIconFile  = "system/app-icon.json"
	appIconImage = "system/app-icon.png"
	appIconEdge  = 512
)

var (
	appIconSizes = map[string]int{"app-180.png": 180, "app-192.png": 192, "app-512.png": 512}
	hexColor     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

type appIconSettings struct {
	// Kind is "default", "color" or "image".
	Kind  string `json:"kind"`
	Color string `json:"color,omitempty"`
}

// appIcons holds the drawn files in RAM; nil maps mean the built-in ones.
type appIcons struct {
	mu       sync.Mutex
	loaded   bool
	settings appIconSettings
	files    map[string][]byte
	version  int
}

// appIconState reads the choice once from /config.
func (s *Server) appIconState() (*appIcons, error) {
	a := &s.appIcon
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded {
		return a, nil
	}
	settings := appIconSettings{Kind: "default"}
	if err := s.opts.Store.ReadJSON(appIconFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return a, err
	}
	var picture image.Image
	if settings.Kind == "image" {
		data, err := s.opts.Store.Read(appIconImage)
		if err == nil {
			picture, err = png.Decode(bytes.NewReader(data))
		}
		if err != nil {
			slog.Warn("own app symbol not readable, using the default", "err", err)
			settings = appIconSettings{Kind: "default"}
		}
	}
	a.settings, a.loaded = settings, true
	a.files = drawAppIcons(settings, picture)
	return a, nil
}

// drawAppIcons makes the PNG files for a choice; nil for the default.
func drawAppIcons(settings appIconSettings, picture image.Image) map[string][]byte {
	var source image.Image
	switch settings.Kind {
	case "color":
		c, err := parseHex(settings.Color)
		if err != nil {
			return nil
		}
		source = logoOn(c)
	case "image":
		if picture == nil {
			return nil
		}
		source = picture
	default:
		return nil
	}
	files := map[string][]byte{}
	for name, size := range appIconSizes {
		canvas := image.NewRGBA(image.Rect(0, 0, size, size))
		xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), source, source.Bounds(), xdraw.Src, nil)
		var out bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, canvas); err != nil {
			return nil
		}
		files[name] = out.Bytes()
	}
	return files
}

func parseHex(text string) (color.RGBA, error) {
	if !hexColor.MatchString(text) {
		return color.RGBA{}, errors.New("not a color")
	}
	v, _ := strconv.ParseUint(text[1:], 16, 32)
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}

// logoOn draws the "T" of the logo (docs/brand/app-icon.svg) on a color,
// dark or white, whichever reads better.
func logoOn(bg color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, appIconEdge, appIconEdge))
	xdraw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, xdraw.Src)
	ink := color.RGBA{R: 0x0B, G: 0x16, B: 0x22, A: 255}
	if luminance(bg) < 0.35 {
		ink = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	unit := float64(appIconEdge) / 32
	rect := func(x0, y0, x1, y1 float64) image.Rectangle {
		return image.Rect(int(x0*unit+0.5), int(y0*unit+0.5), int(x1*unit+0.5), int(y1*unit+0.5))
	}
	xdraw.Draw(img, rect(10.4, 9.6, 21.6, 13.1), &image.Uniform{C: ink}, image.Point{}, xdraw.Src)
	xdraw.Draw(img, rect(14.25, 14.2, 17.75, 22.4), &image.Uniform{C: ink}, image.Point{}, xdraw.Src)
	return img
}

// luminance is the relative luminance of WCAG.
func luminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// squareIcon crops an upload to its middle square, scales it to the size
// of the files and lays it on black (home screens want no transparency).
func squareIcon(data []byte) (image.Image, error) {
	kind := http.DetectContentType(data)
	if kind != "image/png" && kind != "image/jpeg" && kind != "image/webp" {
		return nil, errors.New("the image must be JPEG, PNG or WebP")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 64 || config.Height < 64 || config.Width*config.Height > maxLoginImagePixels {
		return nil, errors.New("the image could not be read, is smaller than 64 pixels or too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the image could not be read")
	}
	b := img.Bounds()
	edge := min(b.Dx(), b.Dy())
	corner := image.Pt(b.Min.X+(b.Dx()-edge)/2, b.Min.Y+(b.Dy()-edge)/2)
	crop := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(edge, edge))}
	out := image.NewRGBA(image.Rect(0, 0, appIconEdge, appIconEdge))
	xdraw.Draw(out, out.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, xdraw.Src)
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, crop, xdraw.Over, nil)
	return out, nil
}

// handleAppIconFile answers /icons/app-*.png: the drawn file of the choice,
// else the built-in one. No sign-in: home screens fetch it without.
func (s *Server) handleAppIconFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	a, _ := s.appIconState()
	a.mu.Lock()
	data := a.files[name]
	a.mu.Unlock()
	if _, known := appIconSizes[name]; !known || data == nil {
		uiHandler(s.opts.UI).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *Server) appIconAnswer(w http.ResponseWriter) {
	a, err := s.appIconState()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"kind": a.settings.Kind, "color": a.settings.Color, "version": a.version})
}

func (s *Server) handleAppIcon(w http.ResponseWriter, r *http.Request) { s.appIconAnswer(w) }

// handlePutAppIcon takes the default or a color as JSON, or a picture as
// the raw body (Content-Type image/…, at most 5 MiB).
func (s *Server) handlePutAppIcon(w http.ResponseWriter, r *http.Request) {
	a, err := s.appIconState()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	var settings appIconSettings
	var picture image.Image
	var pictureFile []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginImageBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the image is larger than 5 MiB")
			return
		}
		if picture, err = squareIcon(data); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_app_icon", err.Error())
			return
		}
		var out bytes.Buffer
		if err := png.Encode(&out, picture); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		settings, pictureFile = appIconSettings{Kind: "image"}, out.Bytes()
	} else {
		if !readJSON(w, r, &settings) {
			return
		}
		switch settings.Kind {
		case "default":
			settings.Color = ""
		case "color":
			if _, err := parseHex(settings.Color); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_app_icon", "the color must look like #6cb6ff")
				return
			}
			settings.Color = strings.ToLower(settings.Color)
		default:
			writeError(w, http.StatusBadRequest, "invalid_app_icon", "unknown kind")
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if pictureFile != nil {
		if err := s.opts.Store.Write(appIconImage, pictureFile); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
	} else if a.settings.Kind == "image" {
		s.opts.Store.Remove(appIconImage)
	}
	if err := s.opts.Store.WriteJSON(appIconFile, settings); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.settings, a.files = settings, drawAppIcons(settings, picture)
	a.version++
	writeJSON(w, http.StatusOK, map[string]any{"kind": settings.Kind, "color": settings.Color, "version": a.version})
}
