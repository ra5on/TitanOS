package nvr

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
)

// Request is one camera search. Host is the only address examined.
type Request struct {
	Host     string
	User     string
	Password string
	// StreamURL is an RTSP link given by the owner; then only it is tried.
	StreamURL string
	Vendor    string
	// Attempts caps rejected logins; 0 means DefaultAuthAttempts.
	Attempts int
}

// Stream is a working stream address, without credentials.
type Stream struct {
	Role   string `json:"role"` // "main" or "sub"
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Codec  string `json:"codec,omitempty"`
	Source string `json:"source"` // "onvif", "link" or the vendor name
}

// Result is the outcome of a search.
type Result struct {
	Found   bool     `json:"found"`
	Streams []Stream `json:"streams"`
	// ONVIFPort is the port ONVIF answered on, 0 if it did not.
	ONVIFPort int `json:"onvifPort,omitempty"`
	// Rejected counts logins the camera refused; Locked is set when it
	// reported a locked account. Both stop the search.
	Rejected int  `json:"rejected"`
	Locked   bool `json:"locked"`
	// ClockOffsetSeconds is camera time minus ours, when ONVIF told it.
	ClockOffsetSeconds float64 `json:"clockOffsetSeconds,omitempty"`
	// Tried names what was looked at, for the person to read.
	Tried []string `json:"tried,omitempty"`
}

// ErrNoCredentials is returned for a search without a user name.
var ErrNoCredentials = errors.New("nvr: user name missing")

// Discover finds the streams of one camera: ONVIF first, then the vendor
// table. It never tries default passwords and stops after the allowed number
// of rejected logins or at the first sign of a locked account.
func (p *Prober) Discover(ctx context.Context, req Request) (Result, error) {
	var res Result
	host := req.Host
	if req.StreamURL != "" {
		h, _, _, err := pathOf(req.StreamURL)
		if err != nil {
			return res, err
		}
		host = h
	}
	if host == "" {
		return res, errors.New("nvr: address missing")
	}
	if err := p.resolve(ctx, host); err != nil {
		return res, err
	}
	budget := NewAuthBudget(orDefault(req.Attempts))
	note := func(s string) { res.Tried = append(res.Tried, s) }
	record := func(o Outcome) {
		budget.Record(o)
		res.Rejected, res.Locked = budget.Rejected(), budget.Locked()
	}

	if req.StreamURL != "" {
		h, port, path, _ := pathOf(req.StreamURL)
		r, err := p.ProbeRTSP(ctx, h, port, path, req.User, req.Password)
		if err != nil {
			return res, err
		}
		record(r.Outcome)
		note("link")
		if r.Outcome == OutcomeOK {
			res.Streams = []Stream{{Role: "main", URL: StreamURL("rtsp", h, port, path, "", ""), Codec: r.Video, Source: "link"}}
			res.Found = true
		}
		return res, nil
	}

	// ONVIF on the usual ports, each at most once.
	var ports []int
	for _, v := range orderedVendors(req.Vendor) {
		for _, port := range v.ONVIFPorts {
			if !slices.Contains(ports, port) {
				ports = append(ports, port)
			}
		}
	}
	if p.ONVIFPorts != nil {
		ports = p.ONVIFPorts
	}
	for _, port := range ports {
		if !budget.Allow() || ctx.Err() != nil {
			break
		}
		r, err := p.ProbeONVIF(ctx, host, port, req.User, req.Password)
		if errors.Is(err, ErrTargetNotAllowed) {
			return res, err
		}
		if err != nil {
			continue // nothing listens there
		}
		note("onvif:" + itoa(port))
		record(r.Outcome)
		if r.Outcome != OutcomeOK {
			continue
		}
		res.ONVIFPort = port
		res.ClockOffsetSeconds = r.ClockOffset.Seconds()
		res.Streams = p.verifiedStreams(ctx, host, req, r.Profiles, budget, record)
		if len(res.Streams) > 0 {
			res.Found = true
			return res, nil
		}
		break
	}

	// Vendor table as the fallback; the first reachable path of each role.
	have := map[string]bool{}
	table := CandidatesFor(req.Vendor)
	if p.Table != nil {
		table = p.Table
	}
	for _, c := range table {
		if have[c.Role] {
			continue
		}
		if !budget.Allow() || ctx.Err() != nil {
			break
		}
		r, err := p.ProbeRTSP(ctx, host, c.Port, c.Path, req.User, req.Password)
		if errors.Is(err, ErrTargetNotAllowed) {
			return res, err
		}
		if err != nil {
			continue
		}
		record(r.Outcome)
		note(c.Vendor + ":" + c.Role)
		if r.Outcome == OutcomeOK {
			have[c.Role] = true
			res.Streams = append(res.Streams, Stream{Role: c.Role, URL: StreamURL("rtsp", host, c.Port, c.Path, "", ""), Codec: r.Video, Source: c.Vendor})
		}
	}
	res.Found = len(res.Streams) > 0
	return res, nil
}

// verifiedStreams checks the addresses ONVIF named with a DESCRIBE and picks
// main (largest picture) and sub (smallest). The camera's own host in the
// address is replaced by the one we reached it at.
func (p *Prober) verifiedStreams(ctx context.Context, host string, req Request, profiles []Profile, budget *AuthBudget, record func(Outcome)) []Stream {
	list := slices.Clone(profiles)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Width*list[i].Height > list[j].Width*list[j].Height })
	var out []Stream
	for _, pr := range list {
		if pr.URI == "" || !budget.Allow() {
			continue
		}
		_, port, path, err := pathOf(pr.URI)
		if err != nil {
			continue
		}
		r, err := p.ProbeRTSP(ctx, host, port, path, req.User, req.Password)
		if err != nil {
			continue
		}
		record(r.Outcome)
		if r.Outcome != OutcomeOK {
			continue
		}
		role := "main"
		if len(out) > 0 {
			role = "sub"
		}
		out = append(out, Stream{Role: role, URL: StreamURL("rtsp", host, port, path, "", ""), Width: pr.Width, Height: pr.Height, Codec: orStr(pr.Codec, r.Video), Source: "onvif"})
	}
	// Keep the main stream and the smallest one as sub.
	if len(out) > 2 {
		out = []Stream{out[0], out[len(out)-1]}
		out[1].Role = "sub"
	}
	return out
}

func orDefault(n int) int {
	if n <= 0 {
		return DefaultAuthAttempts
	}
	return n
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func itoa(n int) string { return strconv.Itoa(n) }

func orderedVendors(wanted string) []Vendor {
	order := slices.Clone(Vendors)
	slices.SortStableFunc(order, func(a, b Vendor) int { return rank(a, wanted) - rank(b, wanted) })
	return order
}
