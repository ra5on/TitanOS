package nvr

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Quality is the video setting of one stream of a camera.
type Quality struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Codec     string `json:"codec"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FPS       int    `json:"fps"`
	BitrateKb int    `json:"bitrateKb"`
	// Options are what the camera offers; empty lists mean it did not say.
	Resolutions []Resolution `json:"resolutions,omitempty"`
	FPSMin      int          `json:"fpsMin,omitempty"`
	FPSMax      int          `json:"fpsMax,omitempty"`
	BitrateMin  int          `json:"bitrateMin,omitempty"`
	BitrateMax  int          `json:"bitrateMax,omitempty"`

	// raw is what is needed to write the setting back unchanged elsewhere.
	raw encoderConfig
}

// Resolution is a picture size.
type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type encoderConfig struct {
	// Multicast and SessionTimeout are required by the schema and written
	// back as the camera sent them.
	Multicast               *node
	SessionTimeout          string
	Name, Encoding, Quality string
	UseCount                int
	Interval                int
	GovLength               int
	Profile                 string
}

// ErrQualityUnsupported is returned for a camera without ONVIF media settings.
var ErrQualityUnsupported = errors.New("nvr: the camera does not offer video settings over ONVIF")

// ReadQuality lists the video settings of every stream of the camera, with
// the choices it offers.
func (p *Prober) ReadQuality(ctx context.Context, host string, port int, user, password string) ([]Quality, Outcome, error) {
	if port <= 0 {
		return nil, OutcomeOther, ErrQualityUnsupported
	}
	if err := p.resolve(ctx, host); err != nil {
		return nil, OutcomeOther, err
	}
	s, err := p.openSession(ctx, host, port, user, password)
	if err != nil || s.outcome != OutcomeOK {
		return nil, s.outcome, err
	}
	st, data, err := s.call(ctx, `<GetVideoEncoderConfigurations xmlns="http://www.onvif.org/ver10/media/wsdl"/>`)
	if err != nil {
		return nil, OutcomeOther, err
	}
	if o := soapOutcome(st, data); o != OutcomeOK {
		return nil, o, ErrQualityUnsupported
	}
	var out []Quality
	for _, c := range parseTree(data).find("GetVideoEncoderConfigurationsResponse", "Configurations") {
		q := qualityOf(c)
		// The options need the token of the configuration.
		if st, data, err := s.call(ctx, `<GetVideoEncoderConfigurationOptions xmlns="http://www.onvif.org/ver10/media/wsdl"><ConfigurationToken>`+xmlEscape(q.Token)+`</ConfigurationToken></GetVideoEncoderConfigurationOptions>`); err == nil && st == 200 {
			applyOptions(&q, parseTree(data))
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil, OutcomeOK, ErrQualityUnsupported
	}
	return out, OutcomeOK, nil
}

func qualityOf(c *node) Quality {
	q := Quality{Token: c.Attrs["token"], Name: c.child("Name").text(), Codec: strings.ToUpper(c.child("Encoding").text())}
	if r := c.child("Resolution"); r != nil {
		q.Width, q.Height = r.num("Width"), r.num("Height")
	}
	if rc := c.child("RateControl"); rc != nil {
		q.FPS, q.BitrateKb = rc.num("FrameRateLimit"), rc.num("BitrateLimit")
		q.raw.Interval = rc.num("EncodingInterval")
	}
	q.raw.Name, q.raw.Encoding, q.raw.Quality = q.Name, c.child("Encoding").text(), c.child("Quality").text()
	q.raw.UseCount = c.num("UseCount")
	q.raw.Multicast = c.child("Multicast")
	q.raw.SessionTimeout = c.child("SessionTimeout").text()
	for _, codec := range []string{"H264", "H265", "MPEG4"} {
		if e := c.child(codec); e != nil {
			q.raw.GovLength = e.num("GovLength")
			q.raw.Profile = e.child(codec + "Profile").text()
			if q.raw.Profile == "" {
				q.raw.Profile = e.child("Profile").text()
			}
		}
	}
	return q
}

// applyOptions takes the choices of the stream's own codec only: the
// answer lists JPEG, MPEG4 and H264 side by side, usually JPEG first.
func applyOptions(q *Quality, doc *node) {
	codec := strings.ToUpper(q.raw.Encoding)
	for _, opts := range doc.find("GetVideoEncoderConfigurationOptionsResponse", "Options") {
		for _, block := range codecBlocks(opts, codec) {
			for _, r := range block.find("ResolutionsAvailable") {
				q.Resolutions = append(q.Resolutions, Resolution{Width: r.num("Width"), Height: r.num("Height")})
			}
			if fr := block.child("FrameRateRange"); fr != nil {
				q.FPSMin, q.FPSMax = fr.num("Min"), fr.num("Max")
			}
			if br := block.child("BitrateRange"); br != nil {
				q.BitrateMin, q.BitrateMax = br.num("Min"), br.num("Max")
			}
		}
	}
	seen := map[Resolution]bool{}
	var uniq []Resolution
	for _, r := range q.Resolutions {
		if r.Width > 0 && !seen[r] {
			seen[r] = true
			uniq = append(uniq, r)
		}
	}
	q.Resolutions = uniq
}

// codecBlocks are the option blocks of one codec: the direct child
// (Options/H264) and the one under Extension (Options/Extension/H264,
// where Profile T cameras put the bit rate).
func codecBlocks(opts *node, codec string) []*node {
	var out []*node
	if b := opts.child(codec); b != nil {
		out = append(out, b)
	}
	if ext := opts.child("Extension"); ext != nil {
		if b := ext.child(codec); b != nil {
			out = append(out, b)
		}
	}
	return out
}

// QualityChange is a new setting for one stream; zero fields stay as they are.
type QualityChange struct {
	Token     string `json:"token"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FPS       int    `json:"fps"`
	BitrateKb int    `json:"bitrateKb"`
}

// ErrOutsideOptions is returned when a value is not among those the camera offers.
var ErrOutsideOptions = errors.New("nvr: the camera does not offer that setting")

// WriteQuality applies a change after checking it against what the camera
// offers. The camera may restart the stream, so recordings show a short gap.
func (p *Prober) WriteQuality(ctx context.Context, host string, port int, user, password string, change QualityChange) (Quality, Outcome, error) {
	list, o, err := p.ReadQuality(ctx, host, port, user, password)
	if err != nil {
		return Quality{}, o, err
	}
	var cur *Quality
	for i := range list {
		if list[i].Token == change.Token {
			cur = &list[i]
		}
	}
	if cur == nil {
		return Quality{}, OutcomeOK, fmt.Errorf("%w: no such stream", ErrOutsideOptions)
	}
	next := *cur
	if change.Width > 0 && change.Height > 0 {
		next.Width, next.Height = change.Width, change.Height
		if len(cur.Resolutions) > 0 && !containsRes(cur.Resolutions, Resolution{change.Width, change.Height}) {
			return Quality{}, OutcomeOK, fmt.Errorf("%w: resolution", ErrOutsideOptions)
		}
	}
	if change.FPS > 0 {
		next.FPS = change.FPS
		if cur.FPSMax > 0 && (change.FPS < cur.FPSMin || change.FPS > cur.FPSMax) {
			return Quality{}, OutcomeOK, fmt.Errorf("%w: frame rate", ErrOutsideOptions)
		}
	}
	if change.BitrateKb > 0 {
		next.BitrateKb = change.BitrateKb
		if cur.BitrateMax > 0 && (change.BitrateKb < cur.BitrateMin || change.BitrateKb > cur.BitrateMax) {
			return Quality{}, OutcomeOK, fmt.Errorf("%w: bit rate", ErrOutsideOptions)
		}
	}
	s, err := p.openSession(ctx, host, port, user, password)
	if err != nil || s.outcome != OutcomeOK {
		return Quality{}, s.outcome, err
	}
	st, data, err := s.call(ctx, setEncoderBody(next))
	if err != nil {
		return Quality{}, OutcomeOther, err
	}
	if o := soapOutcome(st, data); o != OutcomeOK {
		return Quality{}, o, errors.New("nvr: the camera refused the setting")
	}
	return next, OutcomeOK, nil
}

func containsRes(list []Resolution, r Resolution) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}

func setEncoderBody(q Quality) string {
	r := q.raw
	codec := strings.ToUpper(r.Encoding)
	var codecXML string
	switch codec {
	case "H264", "H265":
		codecXML = "<" + codec + ` xmlns="http://www.onvif.org/ver10/schema"><GovLength>` + strconv.Itoa(max(r.GovLength, 1)) + `</GovLength><` + codec + `Profile>` + xmlEscape(orStr(r.Profile, map[string]string{"H264": "Main", "H265": "Main"}[codec])) + `</` + codec + `Profile></` + codec + `>`
	}
	return `<SetVideoEncoderConfiguration xmlns="http://www.onvif.org/ver10/media/wsdl"><Configuration token="` + xmlEscape(q.Token) + `" xmlns:t="http://www.onvif.org/ver10/schema">` +
		`<Name xmlns="http://www.onvif.org/ver10/schema">` + xmlEscape(r.Name) + `</Name>` +
		`<UseCount xmlns="http://www.onvif.org/ver10/schema">` + strconv.Itoa(r.UseCount) + `</UseCount>` +
		`<Encoding xmlns="http://www.onvif.org/ver10/schema">` + xmlEscape(r.Encoding) + `</Encoding>` +
		`<Resolution xmlns="http://www.onvif.org/ver10/schema"><Width>` + strconv.Itoa(q.Width) + `</Width><Height>` + strconv.Itoa(q.Height) + `</Height></Resolution>` +
		`<Quality xmlns="http://www.onvif.org/ver10/schema">` + xmlEscape(orStr(r.Quality, "5")) + `</Quality>` +
		`<RateControl xmlns="http://www.onvif.org/ver10/schema"><FrameRateLimit>` + strconv.Itoa(q.FPS) + `</FrameRateLimit><EncodingInterval>` + strconv.Itoa(max(r.Interval, 1)) + `</EncodingInterval><BitrateLimit>` + strconv.Itoa(q.BitrateKb) + `</BitrateLimit></RateControl>` +
		codecXML + multicastXML(r) + `</Configuration><ForcePersistence>true</ForcePersistence></SetVideoEncoderConfiguration>`
}

// multicastXML writes Multicast and SessionTimeout back unchanged (with
// harmless defaults when the camera left them out).
func multicastXML(r encoderConfig) string {
	const ns = ` xmlns="http://www.onvif.org/ver10/schema"`
	m := r.Multicast
	addrType, addr, port, ttl, auto := "IPv4", "0.0.0.0", "0", "1", "false"
	if m != nil {
		if a := m.child("Address"); a != nil {
			addrType = orStr(a.child("Type").text(), addrType)
			addr = orStr(a.child("IPv4Address").text(), orStr(a.child("IPv6Address").text(), addr))
		}
		port = orStr(m.child("Port").text(), port)
		ttl = orStr(m.child("TTL").text(), ttl)
		auto = orStr(m.child("AutoStart").text(), auto)
	}
	field := "IPv4Address"
	if addrType == "IPv6" {
		field = "IPv6Address"
	}
	return `<Multicast` + ns + `><Address><Type>` + xmlEscape(addrType) + `</Type><` + field + `>` + xmlEscape(addr) + `</` + field + `></Address>` +
		`<Port>` + xmlEscape(port) + `</Port><TTL>` + xmlEscape(ttl) + `</TTL><AutoStart>` + xmlEscape(auto) + `</AutoStart></Multicast>` +
		`<SessionTimeout` + ns + `>` + xmlEscape(orStr(r.SessionTimeout, "PT60S")) + `</SessionTimeout>`
}
