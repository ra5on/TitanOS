package nvr

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Profile is one media profile of a camera, as ONVIF lists it.
type Profile struct {
	Token  string `json:"token"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Codec  string `json:"codec,omitempty"`
	// URI is the stream address the camera named (no credentials).
	URI string `json:"uri,omitempty"`
}

// ONVIFResult is what one ONVIF address showed.
type ONVIFResult struct {
	Outcome  Outcome
	Profiles []Profile
	// ClockOffset is camera time minus ours; used for the clock check.
	ClockOffset time.Duration
}

const (
	nsEnvelope = `xmlns:s="http://www.w3.org/2003/05/soap-envelope"`
	nsWSSE     = `xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"`
)

func (p *Prober) httpClient() *http.Client {
	return &http.Client{
		Timeout: p.timeout() * 2,
		Transport: &http.Transport{
			DialContext:       func(ctx context.Context, _, addr string) (c netConn, err error) { return p.dialAddr(ctx, addr) },
			DisableKeepAlives: true,
		},
		// A camera must not send us elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func securityHeader(user, password string, offset time.Duration) string {
	if user == "" {
		return ""
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	created := time.Now().Add(offset).UTC().Format("2006-01-02T15:04:05.000Z")
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return fmt.Sprintf(`<s:Header><wsse:Security %s><wsse:UsernameToken><wsse:Username>%s</wsse:Username>`+
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>`+
		`<wsse:Nonce>%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>`,
		nsWSSE, xmlEscape(user), base64.StdEncoding.EncodeToString(h.Sum(nil)), base64.StdEncoding.EncodeToString(nonce), created)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// soap sends one request and returns the status and body.
func (p *Prober) soap(ctx context.Context, client *http.Client, endpoint, user, password string, offset time.Duration, body string) (int, []byte, error) {
	env := `<?xml version="1.0" encoding="utf-8"?><s:Envelope ` + nsEnvelope + `>` + securityHeader(user, password, offset) + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(env))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8`)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// soapOutcome reads the rejection out of a failed answer. ONVIF cameras
// answer a wrong login with 400 or 401 and a "NotAuthorized" fault; a locked
// account is told in the fault text by some makers.
func soapOutcome(status int, body []byte) Outcome {
	text := strings.ToLower(string(body))
	switch {
	case strings.Contains(text, "locked") || strings.Contains(text, "lockout") || strings.Contains(text, "too many"):
		return OutcomeLocked
	case status == 401 || strings.Contains(text, "notauthorized") || strings.Contains(text, "sender not authorized"):
		return OutcomeUnauthorized
	case status == 200:
		return OutcomeOK
	}
	return OutcomeOther
}

type xmlEnvelope struct {
	Body struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"Body"`
}

func bodyOf(data []byte) []byte {
	var e xmlEnvelope
	if err := xml.Unmarshal(data, &e); err != nil {
		return nil
	}
	return e.Body.Inner
}

// ProbeONVIF lists the profiles and their stream addresses of the camera at
// host:port. A wrong login is reported as OutcomeUnauthorized so the caller
// can count it against its budget.
func (p *Prober) ProbeONVIF(ctx context.Context, host string, port int, user, password string) (ONVIFResult, error) {
	if err := p.resolve(ctx, host); err != nil {
		return ONVIFResult{}, err
	}
	client := p.httpClient()
	device := "http://" + hostPort(host, port) + "/onvif/device_service"
	var res ONVIFResult

	// The clock needs no login; the digest is computed with the camera's time.
	if st, data, err := p.soap(ctx, client, device, "", "", 0, `<GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/>`); err == nil && st == 200 {
		res.ClockOffset = parseCameraClock(data)
	} else if err != nil {
		return res, err
	}

	mediaURL := "http://" + hostPort(host, port) + "/onvif/media_service"
	st, data, err := p.soap(ctx, client, device, user, password, res.ClockOffset,
		`<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>Media</Category></GetCapabilities>`)
	if err != nil {
		return res, err
	}
	if out := soapOutcome(st, data); out != OutcomeOK {
		res.Outcome = out
		return res, nil
	}
	if x := xaddrOf(data); x != "" {
		mediaURL = onSameHost(x, host, port)
	}

	st, data, err = p.soap(ctx, client, mediaURL, user, password, res.ClockOffset, `<GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/>`)
	if err != nil {
		return res, err
	}
	if out := soapOutcome(st, data); out != OutcomeOK {
		res.Outcome = out
		return res, nil
	}
	res.Profiles = parseProfiles(data)
	for i := range res.Profiles {
		pr := &res.Profiles[i]
		st, data, err := p.soap(ctx, client, mediaURL, user, password, res.ClockOffset,
			`<GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl"><StreamSetup><Stream xmlns="http://www.onvif.org/ver10/schema">RTP-Unicast</Stream><Transport xmlns="http://www.onvif.org/ver10/schema"><Protocol>RTSP</Protocol></Transport></StreamSetup><ProfileToken>`+xmlEscape(pr.Token)+`</ProfileToken></GetStreamUri>`)
		if err != nil || st != 200 {
			continue
		}
		pr.URI = streamURIOf(data)
	}
	res.Outcome = OutcomeOK
	return res, nil
}

// onSameHost keeps the address a camera names for a service, but on the
// address we reached it at: a camera must not point us at another host.
func onSameHost(raw, host string, port int) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "http://" + hostPort(host, port) + "/onvif/media_service"
	}
	return "http://" + hostPort(host, port) + u.EscapedPath()
}

func xaddrOf(data []byte) string {
	var v struct {
		XAddr string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	_ = xml.Unmarshal(data, &v)
	return strings.TrimSpace(v.XAddr)
}

func streamURIOf(data []byte) string {
	var v struct {
		URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
	}
	_ = xml.Unmarshal(data, &v)
	return strings.TrimSpace(v.URI)
}

func parseProfiles(data []byte) []Profile {
	var v struct {
		Profiles []struct {
			Token string `xml:"token,attr"`
			Name  string `xml:"Name"`
			Video struct {
				Encoding   string `xml:"Encoding"`
				Resolution struct {
					Width  int `xml:"Width"`
					Height int `xml:"Height"`
				} `xml:"Resolution"`
			} `xml:"VideoEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	_ = xml.Unmarshal(data, &v)
	var out []Profile
	for _, pr := range v.Profiles {
		out = append(out, Profile{
			Token: pr.Token, Name: pr.Name,
			Width: pr.Video.Resolution.Width, Height: pr.Video.Resolution.Height,
			Codec: strings.ToUpper(pr.Video.Encoding),
		})
	}
	return out
}

func parseCameraClock(data []byte) time.Duration {
	var x struct {
		Year   int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Year"`
		Month  int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Month"`
		Day    int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Date>Day"`
		Hour   int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Hour"`
		Minute int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Minute"`
		Second int `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime>Time>Second"`
	}
	if xml.Unmarshal(data, &x) != nil || x.Year == 0 {
		return 0
	}
	cam := time.Date(x.Year, time.Month(x.Month), x.Day, x.Hour, x.Minute, x.Second, 0, time.UTC)
	return time.Until(cam)
}
