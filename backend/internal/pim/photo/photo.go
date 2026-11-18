// Package photo re-encodes pictures people hand to TitanOS: the account
// pictures of the login page and the pictures of contacts (TECHNIK 5.5).
// Only pixels are kept (no metadata, no location); the output is always a
// JPEG. The dimensions in the header are checked before anything is
// decoded, so a small file that claims a huge picture (a decompression
// bomb) costs nothing.
package photo

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"slices"

	// The formats accepted besides JPEG.
	_ "image/png"

	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

var (
	// ErrFormat: not a JPEG, PNG or WebP picture.
	ErrFormat = errors.New("the picture must be JPEG, PNG or WebP")
	// ErrSize: the header cannot be read, or the picture is smaller than
	// Options.MinEdge or has more pixels than Options.MaxPixels.
	ErrSize = errors.New("the picture could not be read or is too large")
	// ErrDecode: the pixels cannot be read.
	ErrDecode = errors.New("the picture could not be read")
	// ErrEncode: no JPEG within Options.MaxBytes.
	ErrEncode = errors.New("the picture could not be saved")
)

// Types are the content types accepted (http.DetectContentType).
var Types = []string{"image/jpeg", "image/png", "image/webp"}

// Options say what comes out.
type Options struct {
	// Edge bounds width and height of the result (never enlarged).
	Edge int
	// Square crops the middle square first (account pictures); else the
	// aspect ratio stays (contacts: the crop chosen on the iPhone stays).
	Square bool
	// MaxPixels bounds width×height in the header (0 = DefaultMaxPixels).
	MaxPixels int
	// MinEdge is the least width and height accepted (0 = 1).
	MinEdge int
	// MaxBytes bounds the JPEG (0 = no bound): the quality is lowered
	// step by step, then the edge.
	MaxBytes int
	// Quality of the first try (0 = 86).
	Quality int
}

// DefaultMaxPixels is the bound for pictures of contacts: 50 million pixels
// is a photo of a 48-megapixel phone camera with room. Decoding it takes
// about 1.5 bytes per pixel for a JPEG (YCbCr 4:2:0), 4 for PNG and WebP;
// decodes run one at a time (see sem), so the peak stays near 200 MB.
const DefaultMaxPixels = 50_000_000

// sem lets one decode run at a time: a burst of uploads (or devices) must
// not hold several large pictures in RAM together.
var sem = make(chan struct{}, 1)

// Config reads only the header: format and dimensions, checked against
// the options. It decodes no pixels.
func Config(data []byte, opt Options) (image.Config, error) {
	if !slices.Contains(Types, http.DetectContentType(data)) {
		return image.Config{}, ErrFormat
	}
	maxPixels := opt.MaxPixels
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	minEdge := max(opt.MinEdge, 1)
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < minEdge || config.Height < minEdge ||
		int64(config.Width)*int64(config.Height) > int64(maxPixels) {
		return image.Config{}, ErrSize
	}
	return config, nil
}

// DecodeBytes estimates the RAM a decode of the picture takes (for a
// budget such as system.MemoryPolicy.TryReserve).
func DecodeBytes(c image.Config) int64 {
	return int64(c.Width) * int64(c.Height) * 4
}

// Reencode checks the header (Config), decodes and writes a JPEG of at
// most Edge pixels per side.
func Reencode(data []byte, opt Options) ([]byte, error) {
	if _, err := Config(data, opt); err != nil {
		return nil, err
	}
	sem <- struct{}{}
	img, _, err := image.Decode(bytes.NewReader(data))
	<-sem
	if err != nil {
		return nil, ErrDecode
	}
	b := img.Bounds()
	src := b
	if opt.Square {
		side := min(b.Dx(), b.Dy())
		src = image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
		src.Max = src.Min.Add(image.Pt(side, side))
	}
	quality := opt.Quality
	if quality <= 0 {
		quality = 86
	}
	edge := opt.Edge
	if edge <= 0 {
		edge = max(src.Dx(), src.Dy())
	}
	for {
		w, h := fit(src.Dx(), src.Dy(), edge)
		canvas := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, xdraw.Src)
		xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), img, src, xdraw.Over, nil)
		for q := quality; ; q -= 12 {
			var out bytes.Buffer
			if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: max(q, 30)}); err != nil {
				return nil, ErrEncode
			}
			if opt.MaxBytes <= 0 || out.Len() <= opt.MaxBytes {
				return out.Bytes(), nil
			}
			if q <= 30 {
				break
			}
		}
		if edge <= 64 {
			return nil, ErrEncode
		}
		edge = edge * 3 / 4
	}
}

// fit scales w×h into a box of edge×edge, keeping the ratio, never larger.
func fit(w, h, edge int) (int, int) {
	if w <= edge && h <= edge {
		return w, h
	}
	if w >= h {
		return edge, max(1, h*edge/w)
	}
	return max(1, w*edge/h), edge
}

// Contact is how the picture of a contact is kept (TECHNIK 5.5): at most
// 512 pixels per side, the ratio kept, a JPEG of at most 256 KiB.
func Contact(maxPixels int) Options {
	return Options{Edge: ContactEdge, MaxBytes: ContactBytes, MaxPixels: maxPixels}
}

// Bounds of the picture of a contact.
const (
	ContactEdge  = 512
	ContactBytes = 256 << 10
	// DevicePixels bounds a picture in a card of a device: it comes in at
	// most 2 MiB without a RAM budget of its own, so the bound is lower
	// than for a picture chosen in TitanOS (iOS keeps contact pictures
	// far smaller).
	DevicePixels = 24_000_000
)
