package main

import (
	"context"
	"log/slog"
	"net"
	"net/url"

	"github.com/ra5on/TitanOS/backend/internal/speech"
)

// wireSpeech wires speech to text and text to speech (PLAN 92,
// docs/SPEECH.md): faster-whisper and Piper in two containers of their own
// that speech manages through Docker, off by default. The models live on
// a data volume (.titanos/speech); a volume that goes away takes the
// containers with it at the next look. In development mode there is no
// Docker: the settings and the downloads work, nothing is started.
func (w *titand) wireSpeech() error {
	ctx := w.ctx
	w.voices = speech.New(w.store)
	w.voices.Mounted = w.volumes.Mounted
	if !w.opts.dev {
		w.voices.Engine = w.containers
		w.voices.Devices = w.devices.ContainerDevices
	}
	// Who may use the Wyoming ports with "only Home Assistant": the
	// addresses of the connections set up under Settings › Home Assistant.
	w.voices.HomeAssistant = func(ctx context.Context) []net.IP {
		var out []net.IP
		for _, m := range w.homes.All() {
			ha, _ := m.Settings()
			u, err := url.Parse(ha.URL)
			if err != nil || u.Hostname() == "" {
				continue
			}
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
			if err != nil {
				continue
			}
			for _, a := range addrs {
				out = append(out, a.IP)
			}
		}
		return out
	}
	if err := w.voices.Load(); err != nil {
		slog.Warn("speech settings not readable from /config; speech stays off until a start reads them", "err", err)
	}
	w.background(func() { w.voices.Run(ctx) })
	return nil
}
