package nvr

import "slices"

// Candidate is a stream address to try on a camera: the port and path of a
// maker, for the main stream or the substream.
type Candidate struct {
	Vendor string
	// Scheme is rtsp or rtsps.
	Scheme string
	Port   int
	Path   string
	// Role is "main" or "sub".
	Role string
}

// Vendor describes how a maker's cameras are usually reached. The values are
// written from the makers' documentation and common knowledge and are NOT
// yet checked on a device (Verified); an entry that proves wrong is
// corrected here together with its test, not worked around in the search.
type Vendor struct {
	Name string
	// ONVIFPorts are HTTP ports where the camera usually answers ONVIF.
	ONVIFPorts []int
	RTSPPort   int
	Main, Sub  string
	// Note is what to tell the person when the search fails.
	Note     string
	Verified bool
}

// Vendors is the table of the makers of group 1 (plan 26).
var Vendors = []Vendor{
	{Name: "hikvision", ONVIFPorts: []int{80, 8000}, RTSPPort: 554, Main: "/Streaming/Channels/101", Sub: "/Streaming/Channels/102", Note: "lockout"},
	{Name: "dahua", ONVIFPorts: []int{80}, RTSPPort: 554, Main: "/cam/realmonitor?channel=1&subtype=0", Sub: "/cam/realmonitor?channel=1&subtype=1", Note: "lockout"},
	{Name: "reolink", ONVIFPorts: []int{8000, 80}, RTSPPort: 554, Main: "/h264Preview_01_main", Sub: "/h264Preview_01_sub", Note: "enable-rtsp-onvif"},
	{Name: "tapo", ONVIFPorts: []int{2020}, RTSPPort: 554, Main: "/stream1", Sub: "/stream2", Note: "camera-account"},
	{Name: "axis", ONVIFPorts: []int{80}, RTSPPort: 554, Main: "/axis-media/media.amp"},
	{Name: "uniview", ONVIFPorts: []int{80}, RTSPPort: 554, Main: "/media/video1", Sub: "/media/video2"},
	{Name: "foscam", ONVIFPorts: []int{888, 80}, RTSPPort: 554, Main: "/videoMain", Sub: "/videoSub"},
	// Generic paths when the maker is unknown; tried last.
	{Name: "generic", ONVIFPorts: []int{80, 8080, 8000}, RTSPPort: 554, Main: "/stream1", Sub: "/stream2"},
}

// CandidatesFor lists the addresses to try, the identified maker first
// (fewer wrong passwords), then the others. An empty or unknown maker gives
// all of them in table order. Duplicates (same port and path) appear once.
func CandidatesFor(vendor string) []Candidate {
	order := slices.Clone(Vendors)
	slices.SortStableFunc(order, func(a, b Vendor) int {
		return rank(a, vendor) - rank(b, vendor)
	})
	var list []Candidate
	seen := map[string]bool{}
	add := func(v Vendor, role, path string) {
		if path == "" {
			return
		}
		key := role + "|" + path
		if seen[key] {
			return
		}
		seen[key] = true
		list = append(list, Candidate{Vendor: v.Name, Scheme: "rtsp", Port: v.RTSPPort, Path: path, Role: role})
	}
	for _, v := range order {
		add(v, "main", v.Main)
		add(v, "sub", v.Sub)
	}
	return list
}

func rank(v Vendor, wanted string) int {
	switch {
	case v.Name == wanted && wanted != "":
		return 0
	case v.Name == "generic":
		return 2
	default:
		return 1
	}
}
