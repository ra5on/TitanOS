package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// encoderParams describe one run of the video pipeline.
type encoderParams struct {
	GPU    string // cpu | vaapi | nvenc
	Device string // /dev/dri/renderD* for vaapi (optional)
	W, H   int
	// OutW, OutH: the size of the video when it is smaller than the screen
	// (the video mode of the transport "auto", PLAN 72); 0 = W, H. The
	// browser draws it over the whole screen size.
	OutW, OutH int
	FPS        int
	Kbps       int
}

// videoBox is the largest video of the video mode (KasmVNC: at most
// 1920×1080, scaled up in the browser).
const videoBoxW, videoBoxH = 1920, 1080

// fitVideo is the size of the video for a screen: the screen itself when it
// fits into 1920×1080, else scaled down with its aspect ratio, even sizes.
func fitVideo(w, h int) (int, int) {
	if w <= videoBoxW && h <= videoBoxH {
		return w, h
	}
	scale := min(float64(videoBoxW)/float64(w), float64(videoBoxH)/float64(h))
	ow, oh := int(float64(w)*scale)&^1, int(float64(h)*scale)&^1
	return max(ow, 2), max(oh, 2)
}

// out is the encoded size.
func (p encoderParams) out() (int, int) {
	if p.OutW > 0 && p.OutH > 0 && (p.OutW != p.W || p.OutH != p.H) {
		return p.OutW, p.OutH
	}
	return p.W, p.H
}

// gstExists reports whether a GStreamer element is available. GPU
// elements (va*, nv*) only register when a device is present.
func gstExists(element string) bool {
	cmd := exec.Command("/usr/bin/gst-inspect-1.0", "--exists", element)
	cmd.Env = baseEnv()
	return cmd.Run() == nil
}

// warmGstRegistry builds the GStreamer registry once at start so the
// first pipeline does not pay for the plugin scan.
func warmGstRegistry() {
	cmd := exec.Command("/usr/bin/gst-inspect-1.0", "x264enc")
	cmd.Env = baseEnv()
	cmd.Stdout = io.Discard
	_ = cmd.Run()
}

// vaElement returns the name of a va element for a render node:
// renderD128 (or the default device) uses "va<kind>", others
// "varenderD129<kind>".
func vaElement(device, kind string) string {
	base := filepath.Base(device)
	if device == "" || base == "renderD128" {
		return "va" + kind
	}
	return "va" + base + kind
}

// pipelineArgs builds the gst-launch-1.0 command line. The H.264 byte
// stream (Annex B, AU aligned, with access unit delimiters and SPS/PPS
// before every IDR) is written to fd 3.
func pipelineArgs(p encoderParams) ([]string, error) {
	if p.W%2 != 0 || p.H%2 != 0 || p.W <= 0 || p.H <= 0 {
		return nil, errors.New("size must be even")
	}
	gop := p.FPS * 3
	src := fmt.Sprintf("ximagesrc display-name=%s show-pointer=false use-damage=false startx=0 starty=0 endx=%d endy=%d"+
		" ! video/x-raw,framerate=%d/1 ! queue max-size-buffers=1 leaky=downstream ! videoconvert n-threads=2",
		display, p.W-1, p.H-1, p.FPS)
	ow, oh := p.out()
	scaled := ow != p.W || oh != p.H
	if scaled && (ow%2 != 0 || oh%2 != 0 || ow <= 0 || oh <= 0) {
		return nil, errors.New("video size must be even")
	}
	// size adds the smaller size to a caps filter; scaler scales on the CPU.
	size, scaler := "", ""
	if scaled {
		size = fmt.Sprintf(",width=%d,height=%d", ow, oh)
		scaler = "videoscale method=bilinear n-threads=2 ! "
	}
	var enc string
	switch p.GPU {
	case "cpu":
		enc = fmt.Sprintf("%svideo/x-raw,format=I420%s ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=%d vbv-buf-capacity=300"+
			" key-int-max=%d aud=true byte-stream=true threads=2 ! video/x-h264,profile=constrained-baseline", scaler, size, p.Kbps, gop)
	case "vaapi":
		h264 := ""
		for _, e := range []string{vaElement(p.Device, "h264enc"), vaElement(p.Device, "h264lpenc")} {
			if gstExists(e) {
				h264 = e
				break
			}
		}
		if h264 == "" {
			return nil, errors.New("no VA-API H.264 encoder (no /dev/dri access?)")
		}
		// The VA post-processor scales on the GPU.
		post := vaElement(p.Device, "postproc")
		enc = fmt.Sprintf("video/x-raw,format=NV12 ! %s ! video/x-raw(memory:VAMemory),format=NV12%s ! %s rate-control=cbr bitrate=%d"+
			" key-int-max=%d b-frames=0 aud=true target-usage=7", post, size, h264, p.Kbps, gop)
	case "nvenc":
		if !gstExists("nvh264enc") {
			return nil, errors.New("nvh264enc not available")
		}
		enc = fmt.Sprintf("%svideo/x-raw,format=NV12%s ! nvh264enc rc-mode=cbr bitrate=%d gop-size=%d bframes=0 zerolatency=true aud=true preset=low-latency-hq", scaler, size, p.Kbps, gop)
	default:
		return nil, fmt.Errorf("unknown encoder %q", p.GPU)
	}
	line := src + " ! " + enc + " ! h264parse config-interval=-1 ! video/x-h264,stream-format=byte-stream,alignment=au ! fdsink fd=3 sync=false"
	return append([]string{"/usr/bin/gst-launch-1.0", "-q"}, strings.Fields(line)...), nil
}

// videoEncoder runs one pipeline and delivers access units.
type videoEncoder struct {
	params encoderParams
	p      *proc
	r      *os.File
	once   sync.Once
	done   chan struct{}
}

// startVideo starts the pipeline; onAU is called from a single goroutine.
func startVideo(params encoderParams, onAU func(accessUnit)) (*videoEncoder, error) {
	args, err := pipelineArgs(params)
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	setPipeSize(w, 1<<20)
	p, err := startProc("gst-video", args, baseEnv(), nil, true, w)
	w.Close()
	if err != nil {
		r.Close()
		return nil, err
	}
	e := &videoEncoder{params: params, p: p, r: r, done: make(chan struct{})}
	go func() {
		defer close(e.done)
		sp := newAUSplitter()
		buf := make([]byte, 256<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				for _, au := range sp.Write(buf[:n]) {
					onAU(au)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return e, nil
}

// stop ends the pipeline and waits for the reader.
func (e *videoEncoder) stop() {
	if e == nil {
		return
	}
	e.once.Do(func() {
		e.p.stop(2 * time.Second)
		e.r.Close()
		<-e.done
	})
}

// exited is closed when the encoder output ended.
func (e *videoEncoder) exited() <-chan struct{} { return e.done }

// bitrate returns the target bitrate in kbit/s for a screen size, frame
// rate and quality mode, with an extra factor for automatic adaption.
func bitrate(w, h, fps int, mode string, factor float64) int {
	bpp := 0.06 // bits per pixel per frame
	switch mode {
	case "high":
		bpp = 0.12
	case "low":
		bpp = 0.03
	}
	k := int(float64(w*h*fps) * bpp * factor / 1000)
	if k < 300 {
		k = 300
	}
	if k > 25000 {
		k = 25000
	}
	return k
}

func logEncoder(p encoderParams) {
	log.Printf("video: %s %dx%d@%d %d kbit/s", p.GPU, p.W, p.H, p.FPS, p.Kbps)
}
