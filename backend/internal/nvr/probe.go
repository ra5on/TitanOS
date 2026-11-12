package nvr

import (
	"context"
	"slices"
)

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
	// Role is "main" or "sub"; URL has no credentials.
	Role string `json:"role"`
	URL  string `json:"url"`
	StreamInfo
}

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
	// Streams are MainURL and SubURL as they answered.
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
		return devProbe(req), nil
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
	for _, stream := range [][2]string{{"main", result.MainURL}, {"sub", result.SubURL}} {
		role, address := stream[0], stream[1]
		if address == "" {
			continue
		}
		info, err := prober.ProbeRTSP(ctx, address, req.User, req.Password)
		if err != nil {
			return result, err
		}
		result.Streams = append(result.Streams, ProbeStream{Role: role, URL: address, StreamInfo: info})
		if role == "main" && req.Kind == "rtsp" {
			result.Outcome = info.Outcome
		}
		// A refused login is not tried a second time on the other stream.
		if info.Outcome == OutcomeUnauthorized || info.Outcome == OutcomeLocked {
			break
		}
	}
	return result, nil
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
	} else {
		host := req.Host
		result.ONVIFPort = max(req.ONVIFPort, 80)
		result.Device = &ProbeDevice{Manufacturer: "Demo", Model: "DEMO-4K", Firmware: "1.0"}
		result.MainURL, result.SubURL = "rtsp://"+host+":554/main", "rtsp://"+host+":554/sub"
		result.Profiles = []Profile{
			{Token: "main", Name: "mainStream", Width: 3840, Height: 2160, Codec: "H265", URL: result.MainURL, PTZ: true},
			{Token: "sub", Name: "subStream", Width: 640, Height: 360, Codec: "H264", URL: result.SubURL},
		}
		result.PTZ = true
	}
	if result.MainURL != "" {
		result.Streams = append(result.Streams, ProbeStream{Role: "main", URL: result.MainURL, StreamInfo: StreamInfo{Outcome: OutcomeOK, Codec: "H265", Audio: "MPEG4-GENERIC", Width: 3840, Height: 2160}})
	}
	if result.SubURL != "" {
		result.Streams = append(result.Streams, ProbeStream{Role: "sub", URL: result.SubURL, StreamInfo: StreamInfo{Outcome: OutcomeOK, Codec: "H264", Width: 640, Height: 360}})
	}
	return result
}
