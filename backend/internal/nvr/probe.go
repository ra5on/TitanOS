package nvr

import (
	"context"
	"slices"
	"time"
)

// probedStream is what a check found about one stream: the sound's codec
// ("" is no sound) and the picture's size (0 while the camera did not
// say).
type probedStream struct {
	audio         string
	width, height int
	at            time.Time
}

// probedFor is how long a check counts for the camera saved after it.
const probedFor = time.Hour

// maxProbed bounds the remembered streams (a check of an ONVIF camera
// notes up to maxProbeStreams).
const maxProbed = 256

// noteProbe keeps what a check found about a stream that answered until
// the camera is saved (AddCamera, UpdateCamera take it from there), by the
// stream's address without credentials.
func (m *Manager) noteProbe(address string, info StreamInfo) {
	if address == "" || info.Outcome != OutcomeOK {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.probed == nil {
		m.probed = map[string]probedStream{}
	}
	for each, entry := range m.probed {
		if now.Sub(entry.at) > probedFor {
			delete(m.probed, each)
		}
	}
	if _, known := m.probed[address]; known || len(m.probed) < maxProbed {
		m.probed[address] = probedStream{audio: clip(info.Audio, 32), width: info.Width, height: info.Height, at: now}
	}
}

// probedLocked is what a recent check found for a stream.
func (m *Manager) probedLocked(address string) (probedStream, bool) {
	entry, ok := m.probed[address]
	if !ok || address == "" || m.now().Sub(entry.at) > probedFor {
		return probedStream{}, false
	}
	return entry, true
}

// clearStreamFacts drops what a request said about codecs and sizes: they
// are titand's (streamFactsLocked).
func clearStreamFacts(c *Camera) {
	c.Audio.Codec, c.Audio.SubCodec = "", ""
	c.Source.MainWidth, c.Source.MainHeight, c.Source.SubWidth, c.Source.SubHeight = 0, 0, 0, 0
}

// streamFactsLocked sets the sounds' codecs (Audio.Codec, Audio.SubCodec)
// and the pictures' sizes (Source.Main…, Source.Sub…) of a camera being
// saved, per stream: from a recent check of that address, else as the
// camera had them for that address (was, nil for a new camera; the main
// and the sub stream may have swapped places), else unknown. A check that
// did not learn the size leaves a stored one of the same address.
func (m *Manager) streamFactsLocked(c *Camera, was *Camera) {
	facts := func(address string) (audio string, width, height int) {
		if address == "" {
			return "", 0, 0
		}
		if was != nil {
			switch address {
			case was.Source.MainURL:
				audio, width, height = was.Audio.Codec, was.Source.MainWidth, was.Source.MainHeight
			case was.Source.SubURL:
				audio, width, height = was.Audio.SubCodec, was.Source.SubWidth, was.Source.SubHeight
			}
		}
		if probed, ok := m.probedLocked(address); ok {
			audio = probed.audio
			if probed.width > 0 && probed.height > 0 {
				width, height = probed.width, probed.height
			}
		}
		return audio, width, height
	}
	c.Audio.Codec, c.Source.MainWidth, c.Source.MainHeight = facts(c.Source.MainURL)
	c.Audio.SubCodec, c.Source.SubWidth, c.Source.SubHeight = facts(c.Source.SubURL)
}

// ProbeRequest asks one camera before it is added or changed.
type ProbeRequest struct {
	// Kind is "onvif" (Host and ONVIFPort) or "rtsp" (MainURL, SubURL).
	Kind      string `json:"kind"`
	Host      string `json:"host"`
	ONVIFPort int    `json:"onvifPort"`
	MainURL   string `json:"mainURL"`
	SubURL    string `json:"subURL"`
	User      string `json:"user"`
	Password  string `json:"password"`
	// Camera is the ID of a stored camera: without a password in the
	// request its stored login is used, for its own addresses only.
	Camera string `json:"camera"`
	// AllowPublic: the administrator confirmed an address outside the
	// home network.
	AllowPublic bool `json:"allowPublic"`
}

// ProbeStream is one stream that was asked.
type ProbeStream struct {
	// Role is "main" or "sub" (the streams TitanOS would take: MainURL,
	// SubURL), or "other" (a further profile of an ONVIF camera); URL has
	// no credentials.
	Role string `json:"role"`
	URL  string `json:"url"`
	// Profile is the name of the ONVIF profile ("" for kind rtsp). Width,
	// Height and Codec come from the profile where the stream's
	// description does not name them.
	Profile string `json:"profile,omitempty"`
	StreamInfo
}

// maxProbeStreams bounds the streams one check asks: each costs a login
// at the camera.
const maxProbeStreams = 6

// ProbeDevice is who the camera says it is (ONVIF).
type ProbeDevice struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware"`
}

// ProbeResult is what the camera answered.
type ProbeResult struct {
	// Outcome is ok, unauthorized, locked, not_found, unreachable or
	// other: of the ONVIF login for kind onvif, of the main stream for
	// kind rtsp.
	Outcome string `json:"outcome"`
	// Device and Profiles are set for kind onvif.
	Device   *ProbeDevice `json:"device"`
	Profiles []Profile    `json:"profiles"`
	// MainURL and SubURL are the streams TitanOS would take.
	MainURL string `json:"mainURL"`
	SubURL  string `json:"subURL"`
	// Streams are MainURL and SubURL as they answered, for kind onvif
	// followed by the streams of the other profiles (each address once, at
	// most maxProbeStreams), so that the page can offer each.
	Streams   []ProbeStream `json:"streams"`
	PTZ       bool          `json:"ptz"`
	ONVIFPort int           `json:"onvifPort"`
	// ClockOffsetSeconds is camera time minus ours (ONVIF).
	ClockOffsetSeconds float64 `json:"clockOffsetSeconds"`
}

// Discover searches the home network for ONVIF cameras (discover.go).
func (m *Manager) Discover(ctx context.Context) ([]Found, error) {
	if m.Dev {
		return []Found{
			{Address: "192.168.1.65", ONVIFPort: 80, Name: "Demo Kamera Carport", Hardware: "DEMO-4K", ID: "demo-0001"},
			{Address: "192.168.1.63", ONVIFPort: 8000, Name: "Demo Kamera Garten", Hardware: "DEMO-PTZ", ID: "demo-0002"},
		}, nil
	}
	return Discover(ctx)
}

// Probe asks a camera who it is and whether its streams answer. It never
// uses Frigate for this: Frigate's own check takes the address with the
// password in a query, which its web server writes to its log.
func (m *Manager) Probe(ctx context.Context, req ProbeRequest) (ProbeResult, error) {
	result := ProbeResult{Profiles: []Profile{}, Streams: []ProbeStream{}}
	if req.MainURL != "" {
		clean, user, password := SplitCredentials(req.MainURL)
		req.MainURL = clean
		if req.User == "" {
			req.User, req.Password = user, password
		}
	}
	if req.SubURL != "" {
		req.SubURL, _, _ = SplitCredentials(req.SubURL)
	}
	if req.Camera != "" && req.Password == "" {
		c, ok := m.Camera(req.Camera)
		if !ok {
			return result, ErrNotFound
		}
		// The stored login only goes to the camera it belongs to.
		known := []string{c.Source.Host, streamHost(c.Source.MainURL), streamHost(c.Source.SubURL)}
		for _, host := range []string{req.Host, streamHost(req.MainURL), streamHost(req.SubURL)} {
			if host != "" && !slices.Contains(known, host) {
				return result, invalid("password: needed for another address")
			}
		}
		if req.User == "" || req.User == c.Source.User {
			req.User, req.Password = c.Source.User, c.Source.Password
		}
		req.AllowPublic = req.AllowPublic || c.Source.AllowPublic
	}
	if m.Dev {
		result := devProbe(req)
		for _, s := range result.Streams {
			m.noteProbe(s.URL, s.StreamInfo)
		}
		return result, nil
	}
	prober := &Prober{AllowPublic: req.AllowPublic}
	switch req.Kind {
	case "onvif":
		if req.ONVIFPort == 0 {
			req.ONVIFPort = 80
		}
		info, err := prober.ProbeONVIF(ctx, req.Host, req.ONVIFPort, req.User, req.Password)
		if err != nil {
			return result, err
		}
		result.Outcome, result.ONVIFPort, result.ClockOffsetSeconds = info.Outcome, req.ONVIFPort, info.ClockOffsetSeconds
		if info.Outcome != OutcomeOK {
			return result, nil
		}
		result.Device = &ProbeDevice{Manufacturer: info.Manufacturer, Model: info.Model, Firmware: info.Firmware}
		result.Profiles, result.PTZ = info.Profiles, info.PTZ
		result.MainURL, result.SubURL = PickStreams(info.Profiles)
	case "rtsp":
		if req.MainURL == "" {
			return result, invalid("mainURL")
		}
		result.MainURL, result.SubURL = req.MainURL, req.SubURL
	default:
		return result, invalid("kind")
	}
	for _, target := range probeTargets(result.MainURL, result.SubURL, result.Profiles) {
		info, err := prober.ProbeRTSP(ctx, target.URL, req.User, req.Password)
		if err != nil {
			return result, err
		}
		target.StreamInfo = withProfile(info, target.profile)
		result.Streams = append(result.Streams, target.ProbeStream)
		if target.Role == "main" && req.Kind == "rtsp" {
			result.Outcome = info.Outcome
		}
		m.noteProbe(target.URL, target.StreamInfo)
		// A refused login is not tried a second time on another stream:
		// cameras lock the account after a few.
		if info.Outcome == OutcomeUnauthorized || info.Outcome == OutcomeLocked {
			break
		}
	}
	return result, nil
}

// probeTarget is a stream a check asks, with the ONVIF profile it is of.
type probeTarget struct {
	ProbeStream
	profile *Profile
}

// probeTargets are the streams a check asks: the main and the sub stream
// TitanOS would take, then the streams of the other profiles in their
// order; each address once, at most maxProbeStreams.
func probeTargets(mainURL, subURL string, profiles []Profile) []probeTarget {
	out := []probeTarget{}
	add := func(role, address string) {
		if address == "" || len(out) >= maxProbeStreams || slices.ContainsFunc(out, func(t probeTarget) bool { return t.URL == address }) {
			return
		}
		target := probeTarget{ProbeStream: ProbeStream{Role: role, URL: address}}
		if at := slices.IndexFunc(profiles, func(p Profile) bool { return p.URL == address }); at >= 0 {
			target.profile = &profiles[at]
			target.Profile = profiles[at].Name
		}
		out = append(out, target)
	}
	add("main", mainURL)
	add("sub", subURL)
	for _, p := range profiles {
		add("other", p.URL)
	}
	return out
}

// withProfile fills in what a stream's description did not name from its
// ONVIF profile: many cameras leave the size out of the description.
func withProfile(info StreamInfo, profile *Profile) StreamInfo {
	if profile == nil || info.Outcome != OutcomeOK {
		return info
	}
	if (info.Width == 0 || info.Height == 0) && profile.Width > 0 && profile.Height > 0 {
		info.Width, info.Height = profile.Width, profile.Height
	}
	if info.Codec == "" {
		info.Codec = profile.Codec
	}
	return info
}

// devProbe is the answer of development mode: a camera that is there
// unless the password is "wrong".
func devProbe(req ProbeRequest) ProbeResult {
	result := ProbeResult{Outcome: OutcomeOK, Profiles: []Profile{}, Streams: []ProbeStream{}}
	if req.Password == "wrong" {
		result.Outcome = OutcomeUnauthorized
		return result
	}
	if req.Kind == "rtsp" {
		result.MainURL, result.SubURL = req.MainURL, req.SubURL
		if result.MainURL != "" {
			result.Streams = append(result.Streams, ProbeStream{Role: "main", URL: result.MainURL, StreamInfo: StreamInfo{Outcome: OutcomeOK, Codec: "H265", Audio: "MPEG4-GENERIC", Width: 3840, Height: 2160}})
		}
		if result.SubURL != "" {
			result.Streams = append(result.Streams, ProbeStream{Role: "sub", URL: result.SubURL, StreamInfo: StreamInfo{Outcome: OutcomeOK, Codec: "H264", Width: 640, Height: 360}})
		}
		return result
	}
	// Three streams in four profiles (two share the main stream, as on
	// many cameras), so that the page offers a choice.
	host := req.Host
	result.ONVIFPort = max(req.ONVIFPort, 80)
	result.Device = &ProbeDevice{Manufacturer: "Demo", Model: "DEMO-4K", Firmware: "1.0"}
	base := "rtsp://" + host + ":554/"
	result.Profiles = []Profile{
		{Token: "main", Name: "mainStream", Width: 3840, Height: 2160, Codec: "H265", URL: base + "main", PTZ: true},
		{Token: "sub", Name: "subStream", Width: 640, Height: 360, Codec: "H264", URL: base + "sub"},
		{Token: "third", Name: "thirdStream", Width: 1280, Height: 720, Codec: "H264", URL: base + "third"},
		{Token: "main2", Name: "mainStream (PTZ)", Width: 3840, Height: 2160, Codec: "H265", URL: base + "main", PTZ: true},
	}
	result.PTZ = true
	result.MainURL, result.SubURL = PickStreams(result.Profiles)
	audio := map[string]string{base + "main": "MPEG4-GENERIC", base + "third": "PCMA"}
	for _, target := range probeTargets(result.MainURL, result.SubURL, result.Profiles) {
		target.StreamInfo = withProfile(StreamInfo{Outcome: OutcomeOK, Audio: audio[target.URL]}, target.profile)
		result.Streams = append(result.Streams, target.ProbeStream)
	}
	return result
}
