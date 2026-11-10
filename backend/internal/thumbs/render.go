package thumbs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/sandbox"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	// ErrUnsupported is a file no thumbnail is made of (type, size or a
	// missing helper program such as ffmpeg).
	ErrUnsupported = errors.New("thumbs: no thumbnail for this file")
	// ErrBusy means all generation slots stayed taken.
	ErrBusy = errors.New("thumbs: too many thumbnails at once")
)

const (
	// maxPixels refuses decompression bombs before any pixel is decoded.
	maxPixels = 100_000_000
	// maxDecoded bounds the memory a decoded picture takes in titand: the
	// pixels alone say too little, a 16-bit picture with transparency
	// takes eight bytes each (100 megapixels: 800 MiB, and two pictures
	// are decoded at a time on a system that runs from RAM).
	maxDecoded = 400 << 20
	maxImage   = 64 << 20
	// maxRendered bounds what pdftoppm or ffmpeg may hand back.
	maxRendered = 32 << 20
	toolTimeout = 20 * time.Second
)

// Kind returns the way a thumbnail of a file is made: "image", "pdf",
// "video" or "" for none. Only the name counts, like the file icons.
func Kind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return "image"
	case ".pdf":
		return "pdf"
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".ogv", ".mts", ".m2ts", ".wmv":
		return "video"
	case ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp":
		return "office"
	}
	return ""
}

// render turns an open file into a thumbnail that fits into box×box.
func (m *Manager) render(ctx context.Context, f *os.File, name string, size int64, box int) ([]byte, error) {
	var (
		img         image.Image
		orientation int
		err         error
	)
	switch Kind(name) {
	case "image":
		if size > maxImage {
			return nil, ErrUnsupported
		}
		img, orientation, err = decodeImage(f)
	case "pdf":
		img, err = m.runTool(ctx, f, "pdftoppm", "-f", "1", "-l", "1", "-scale-to", strconv.Itoa(box), "-singlefile", "-png", "-")
	case "video":
		// A frame one second in avoids the black first frame of many clips;
		// a shorter clip falls back to its very first frame.
		args := func(seek string) []string {
			return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", "1", "-ss", seek, "-i", "fd:0", "-frames:v", "1",
				"-vf", fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease", box, box), "-f", "image2pipe", "-vcodec", "png", "pipe:1"}
		}
		img, err = m.runTool(ctx, f, "ffmpeg", args("1")...)
		if errors.Is(err, errEmpty) {
			if _, err = f.Seek(0, io.SeekStart); err == nil {
				img, err = m.runTool(ctx, f, "ffmpeg", args("0")...)
			}
		}
	case "office":
		img, err = officeThumbnail(f, size)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	return encode(orient(scale(img, box), orientation))
}

// Office documents (PLAN 70, quick look): OOXML and OpenDocument files are
// zip archives that often carry a small picture of their first page, made
// by the program that saved them. Only that picture is read: nothing is
// converted, no helper program runs. Without one (or as EMF/WMF) the file
// manager shows the icon.
const (
	maxOffice      = 512 << 20
	maxOfficeThumb = 8 << 20
)

var officeThumbs = []string{"docProps/thumbnail.jpeg", "docProps/thumbnail.jpg", "docProps/thumbnail.png", "Thumbnails/thumbnail.png"}

func officeThumbnail(f *os.File, size int64) (image.Image, error) {
	if size <= 0 || size > maxOffice {
		return nil, ErrUnsupported
	}
	z, err := zip.NewReader(f, size)
	if err != nil {
		return nil, ErrUnsupported
	}
	for _, want := range officeThumbs {
		for _, entry := range z.File {
			if !strings.EqualFold(entry.Name, want) || entry.UncompressedSize64 > maxOfficeThumb {
				continue
			}
			r, err := entry.Open()
			if err != nil {
				return nil, ErrUnsupported
			}
			data, err := io.ReadAll(io.LimitReader(r, maxOfficeThumb+1))
			_ = r.Close()
			if err != nil || len(data) > maxOfficeThumb {
				return nil, ErrUnsupported
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPixels {
				return nil, ErrUnsupported
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, ErrUnsupported
			}
			return img, nil
		}
	}
	return nil, ErrUnsupported
}

func decodeImage(f *os.File) (image.Image, int, error) {
	head := make([]byte, exifLimit)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	config, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPixels ||
		int64(config.Width)*int64(config.Height)*bytesPerPixel(config.ColorModel) > maxDecoded {
		return nil, 0, ErrUnsupported
	}
	orientation := 0
	if format == "jpeg" {
		orientation = parseJPEG(head).Orientation
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	img, _, err := image.Decode(io.LimitReader(f, maxImage))
	if err != nil {
		return nil, 0, ErrUnsupported
	}
	return img, orientation, nil
}

// bytesPerPixel is what a decoded picture of this color model takes per
// pixel in memory.
func bytesPerPixel(model color.Model) int64 {
	switch model {
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	case color.Gray16Model:
		return 2
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.YCbCrModel:
		return 3
	case color.CMYKModel, color.RGBAModel, color.NRGBAModel, color.NYCbCrAModel:
		return 4
	}
	if _, paletted := model.(color.Palette); paletted {
		return 1
	}
	return 4
}

var errEmpty = errors.New("thumbs: helper produced no picture")

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("thumbs: helper output too large")
	}
	return b.Buffer.Write(p)
}

// runTool runs a fixed helper on the already opened file, passed as its
// stdin (still seekable, for videos with the index at the end): no path
// from the request reaches the command line and no temporary file is
// written. The helper runs in a sandbox (package sandbox); the picture
// comes back through a RAM pipe.
func (m *Manager) runTool(ctx context.Context, f *os.File, tool string, args ...string) (image.Image, error) {
	program, err := m.lookPath(tool)
	if err != nil {
		return nil, ErrUnsupported // e.g. no ffmpeg in this image: an icon instead
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := sandbox.Command(ctx, sandbox.Limits{MemoryMax: 768 << 20, Runtime: toolTimeout + 10*time.Second}, program, args...)
	cmd.Stdin = f
	out := &boundedBuffer{limit: maxRendered}
	cmd.Stdout, cmd.Stderr = out, &boundedBuffer{limit: 4096}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if out.Len() == 0 {
			return nil, errEmpty
		}
		return nil, ErrUnsupported
	}
	if out.Len() == 0 {
		return nil, errEmpty
	}
	config, err := png.DecodeConfig(bytes.NewReader(out.Bytes()))
	if err != nil || int64(config.Width)*int64(config.Height) > maxPixels {
		return nil, ErrUnsupported
	}
	img, err := png.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		return nil, ErrUnsupported
	}
	return img, nil
}

// scale fits an image into box×box without enlarging it. Large reductions
// take a cheap first step, so that a camera picture does not cost seconds.
func scale(src image.Image, box int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= box && h <= box {
		return src
	}
	fit := func(limit int) (int, int) {
		if w >= h {
			return limit, max(1, h*limit/w)
		}
		return max(1, w*limit/h), limit
	}
	if w > 4*box || h > 4*box {
		tw, th := fit(2 * box)
		step := image.NewRGBA(image.Rect(0, 0, tw, th))
		xdraw.ApproxBiLinear.Scale(step, step.Bounds(), src, b, draw.Src, nil)
		src, b = step, step.Bounds()
	}
	tw, th := fit(box)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// orient applies an EXIF orientation (2–8) so that the picture stands upright.
func orient(src image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	transposed := orientation >= 5
	dw, dh := w, h
	if transposed {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// encode writes JPEG, or PNG where transparency would otherwise turn black.
func encode(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if opaque, ok := img.(interface{ Opaque() bool }); ok && !opaque.Opaque() {
		if err := png.Encode(&out, img); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
