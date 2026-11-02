package nvr

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

// Snapshot returns one JPEG picture of the camera's sub stream (or main,
// if there is no sub stream).
func (r *Recorder) Snapshot(ctx context.Context, c Camera) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.program(),
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-i", r.Store.Full(c, true),
		"-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "4", "-f", "image2pipe", "-c:v", "mjpeg", "pipe:1")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		return nil, errors.New("nvr: no picture from the camera")
	}
	return out.Bytes(), nil
}

// LiveBoundary is the part separator of the live stream.
const LiveBoundary = "ffmpeg"

// maxLive limits simultaneous live views; each one decodes a stream.
const maxLive = 6

var liveSlots = make(chan struct{}, maxLive)

// ErrTooManyLive is returned when all live slots are in use.
var ErrTooManyLive = errors.New("nvr: too many live views")

// Live writes a motion-JPEG stream of the sub stream to w until ctx ends.
func (r *Recorder) Live(ctx context.Context, c Camera, w io.Writer) error {
	select {
	case liveSlots <- struct{}{}:
		defer func() { <-liveSlots }()
	default:
		return ErrTooManyLive
	}
	cmd := exec.CommandContext(ctx, r.program(),
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-i", r.Store.Full(c, true),
		"-an", "-r", "5", "-vf", "scale='min(960,iw)':-2", "-q:v", "6", "-f", "mpjpeg", "-boundary_tag", LiveBoundary, "pipe:1")
	cmd.Stdout = w
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = 3 * time.Second
	return cmd.Run()
}

// fileExists is used by the API to serve one recording.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
