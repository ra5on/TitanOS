package nvr

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
)

// go2rtc keeps the streams of config.yml's preload open (PLAN 82-1): it
// connects at its start and connects again by itself after a drop. But a
// camera that did not answer when go2rtc started is never tried again
// (measured with go2rtc 1.9.14). The watch therefore asks go2rtc which
// streams it keeps open and asks it again for each missing one. go2rtc's
// API listens on the container's loopback only, and Frigate's web server
// has no route to /api/preload: the questions go through docker exec with
// the curl of the image.

// The pace of the watch: go2rtc is asked every preloadEvery (not in the
// first preloadEvery after a start, while it still connects), one missing
// stream at most every rearmEvery.
const (
	preloadEvery = 2 * time.Minute
	rearmEvery   = 5 * time.Minute
)

// preloadOutputLimit bounds what is read of go2rtc's answer: a map of the
// open streams, a few hundred bytes per stream.
const preloadOutputLimit = 64 << 10

// go2rtcAPI is go2rtc's API inside the container.
const go2rtcAPI = "http://127.0.0.1:1984/api/preload"

// preloadListCmd asks go2rtc for the streams it keeps open.
var preloadListCmd = []string{"curl", "-s", "-m", "5", go2rtcAPI}

// preloadArmCmd asks go2rtc to keep a stream open again. Its answer is
// never read: an error of go2rtc may name the camera's address with the
// password in it.
func preloadArmCmd(stream string) []string {
	return []string{"curl", "-s", "-m", "10", "-o", "/dev/null", "-X", "PUT", go2rtcAPI + "?src=" + url.QueryEscape(stream)}
}

// preloadState is what the watch knows of the streams go2rtc keeps open.
type preloadState struct {
	// busy: a round runs (it never holds up the loop); checkedAt is when
	// the last one began.
	busy      bool
	checkedAt time.Time
	// tried is when each missing stream was last asked for again;
	// missing are the streams found missing (each logged once).
	tried   map[string]time.Time
	missing map[string]bool
}

// watchPreload starts a round when one is due: Frigate runs and answered,
// the container runs for preloadEvery, and the last round is that long
// ago. The round runs beside the loop: with several cameras away, asking
// go2rtc again takes up to ten seconds per stream.
func (m *Manager) watchPreload(ctx context.Context, now time.Time) {
	m.mu.Lock()
	p := &m.rt.preload
	due := m.rt.healthy && !p.busy && now.Sub(m.rt.startedAt) >= preloadEvery && now.Sub(p.checkedAt) >= preloadEvery &&
		(len(m.rt.configPreload) > 0 || len(p.tried) > 0 || len(p.missing) > 0)
	if !due {
		m.mu.Unlock()
		return
	}
	p.busy, p.checkedAt = true, now
	want := slices.Clone(m.rt.configPreload)
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			m.rt.preload.busy = false
			m.mu.Unlock()
		}()
		rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		m.preloadRound(rctx, want)
	}()
}

// preloadRound asks go2rtc which of want it keeps open and asks it again
// for each missing stream not asked for within rearmEvery. Nothing go2rtc
// answers is logged; only the names of the streams (camera IDs).
func (m *Manager) preloadRound(ctx context.Context, want []string) {
	out, code, err := m.Engine.NVRExec(ctx, preloadListCmd, preloadOutputLimit)
	if err != nil || code != 0 {
		// Frigate restarting, the container going: the next round asks again.
		slog.Debug("NVR: go2rtc not asked for its open streams", "exit", code, "err", err != nil)
		return
	}
	var open map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &open); err != nil {
		slog.Debug("NVR: go2rtc's open streams not readable")
		return
	}
	now := m.now()
	var arm, lost, back []string
	m.mu.Lock()
	p := &m.rt.preload
	if p.tried == nil {
		p.tried, p.missing = map[string]time.Time{}, map[string]bool{}
	}
	// Streams that are no longer to be kept open are forgotten.
	for name := range p.tried {
		if !slices.Contains(want, name) {
			delete(p.tried, name)
		}
	}
	for name := range p.missing {
		if !slices.Contains(want, name) {
			delete(p.missing, name)
		}
	}
	for _, name := range want {
		if _, ok := open[name]; ok {
			if p.missing[name] {
				back = append(back, name)
			}
			delete(p.missing, name)
			delete(p.tried, name)
			continue
		}
		if !p.missing[name] {
			p.missing[name] = true
			lost = append(lost, name)
		}
		if last, ok := p.tried[name]; ok && now.Sub(last) < rearmEvery {
			continue
		}
		p.tried[name] = now
		arm = append(arm, name)
	}
	m.mu.Unlock()
	if len(lost) > 0 {
		slog.Info("NVR: go2rtc does not keep these streams open (camera away when it connected); asking it again every 5 minutes", "streams", strings.Join(lost, " "))
	}
	if len(back) > 0 {
		slog.Info("NVR: go2rtc keeps these streams open again", "streams", strings.Join(back, " "))
	}
	for _, name := range arm {
		if ctx.Err() != nil {
			return
		}
		// What it answers tells nothing the next round does not: go2rtc
		// lists the stream then, or it does not.
		if _, _, err := m.Engine.NVRExec(ctx, preloadArmCmd(name), 4<<10); err != nil {
			slog.Debug("NVR: go2rtc not asked again for a stream", "stream", name)
		}
	}
}
