package nvr

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Profile is one media profile of a camera, as ONVIF lists it.
type Profile struct {
	Token  string `json:"token"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Codec  string `json:"codec"`
	// URL is the stream address the camera named, on the address the
	// camera was reached at, without credentials.
	URL string `json:"url"`
	// PTZ: the profile has a pan/tilt configuration.
	PTZ bool `json:"ptz"`
}

// ONVIFInfo is what a camera told over ONVIF.
type ONVIFInfo struct {
	Outcome      string    `json:"outcome"`
	Manufacturer string    `json:"manufacturer"`
	Model        string    `json:"model"`
	Firmware     string    `json:"firmware"`
	Profiles     []Profile `json:"profiles"`
	// PTZ: at least one profile pans and tilts.
	PTZ bool `json:"ptz"`
	// ClockOffsetSeconds is camera time minus ours; cameras refuse logins
	// when their clock is far off, and recordings carry the wrong time.
	ClockOffsetSeconds float64 `json:"clockOffsetSeconds"`
}

const (
	nsEnvelope = `xmlns:s="http://www.w3.org/2003/05/soap-envelope"`
	nsWSSE     = `xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"`
)

func (p *Prober) httpClient() *http.Client {
	return &http.Client{
		Timeout: 2 * p.timeout(),
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				return p.dial(ctx, host, atoi(port))
			},
			DisableKeepAlives: true,
		},
		// A camera must not send titand elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// securityHeader is the WS-Security UsernameToken with a password digest
// (SHA-1 over nonce, time and password, as ONVIF defines it): the
// password itself is never sent.
func securityHeader(user, password string, offset time.Duration) string {
	if user == "" {
		return ""
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce) // crypto/rand does not fail on Linux
	created := time.Now().Add(offset).UTC().Format("2006-01-02T15:04:05.000Z")
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return fmt.Sprintf(`<s:Header><wsse:Security %s><wsse:UsernameToken><wsse:Username>%s</wsse:Username>`+
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>`+
		`<wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>`,
		nsWSSE, xmlEscape(user), base64.StdEncoding.EncodeToString(h.Sum(nil)), base64.StdEncoding.EncodeToString(nonce), created)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s)) // a bytes.Buffer does not fail
	return b.String()
}

// soap sends one request and returns the status and body.
func soap(ctx context.Context, client *http.Client, endpoint, user, password string, offset time.Duration, body string) (int, []byte, error) {
	envelope := `<?xml version="1.0" encoding="utf-8"?><s:Envelope ` + nsEnvelope + `>` + securityHeader(user, password, offset) + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8`)
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, data, err
}

// soapOutcome reads the rejection out of an answer. ONVIF cameras answer
// a wrong login with 400 or 401 and a "NotAuthorized" fault; a locked
// account is told in the fault text by some makers.
func soapOutcome(status int, body []byte) string {
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

// ProbeONVIF asks the camera at host:port who it is and lists its
// profiles with their stream addresses. A refused login is the outcome
// "unauthorized", not an error; the login is tried once.
func (p *Prober) ProbeONVIF(ctx context.Context, host string, port int, user, password string) (ONVIFInfo, error) {
	info := ONVIFInfo{Profiles: []Profile{}}
	if !validHost(host) || !validPort(port) {
		return info, invalid("ONVIF address")
	}
	client := p.httpClient()
	device := "http://" + hostPort(host, port) + "/onvif/device_service"
	// The clock needs no login; the digest is made with the camera's time.
	status, data, err := soap(ctx, client, device, "", "", 0, `<GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/>`)
	if err != nil {
		if errors.Is(err, ErrTargetNotAllowed) {
			return info, fmt.Errorf("%w: %s", ErrTargetNotAllowed, host)
		}
		info.Outcome = OutcomeUnreachable
		return info, nil
	}
	var offset time.Duration
	if status == 200 {
		offset = parseCameraClock(data)
		info.ClockOffsetSeconds = offset.Round(time.Second).Seconds()
	}
	media := "http://" + hostPort(host, port) + "/onvif/media_service"
	status, data, err = soap(ctx, client, device, user, password, offset,
		`<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>Media</Category></GetCapabilities>`)
	if err != nil {
		info.Outcome = OutcomeUnreachable
		return info, nil
	}
	if info.Outcome = soapOutcome(status, data); info.Outcome != OutcomeOK {
		return info, nil
	}
	if x := xaddrOf(data); x != "" {
		media = onSameHost(x, host, port)
	}
	if status, data, err = soap(ctx, client, device, user, password, offset, `<GetDeviceInformation xmlns="http://www.onvif.org/ver10/device/wsdl"/>`); err == nil && status == 200 {
		info.Manufacturer, info.Model, info.Firmware = parseDeviceInformation(data)
	}
	status, data, err = soap(ctx, client, media, user, password, offset, `<GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/>`)
	if err != nil {
		info.Outcome = OutcomeUnreachable
		return info, nil
	}
	if info.Outcome = soapOutcome(status, data); info.Outcome != OutcomeOK {
		return info, nil
	}
	info.Profiles = parseProfiles(data)
	for i := range info.Profiles {
		pr := &info.Profiles[i]
		status, data, err := soap(ctx, client, media, user, password, offset,
			`<GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl"><StreamSetup><Stream xmlns="http://www.onvif.org/ver10/schema">RTP-Unicast</Stream><Transport xmlns="http://www.onvif.org/ver10/schema"><Protocol>RTSP</Protocol></Transport></StreamSetup><ProfileToken>`+xmlEscape(pr.Token)+`</ProfileToken></GetStreamUri>`)
		if err != nil || status != 200 {
			continue
		}
		pr.URL = streamOnHost(streamURIOf(data), host)
		info.PTZ = info.PTZ || pr.PTZ
	}
	// Profiles without a usable stream address are of no use.
	info.Profiles = slices.DeleteFunc(info.Profiles, func(pr Profile) bool { return pr.URL == "" })
	return info, nil
}

// onSameHost keeps the path a camera names for a service, but on the
// address it was reached at: a camera must not point titand at another
// host.
func onSameHost(raw, host string, port int) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "http://" + hostPort(host, port) + "/onvif/media_service"
	}
	return "http://" + hostPort(host, port) + u.EscapedPath()
}

// streamOnHost is the stream address a camera named, on the host it was
// reached at (cameras behind NAT name their inner address), without
// credentials; "" if it is no address TitanOS takes.
func streamOnHost(raw, host string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") {
		return ""
	}
	u.User, u.Fragment = nil, ""
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		u.Host = host
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		}
	}
	out := u.String()
	if _, err := streamURL(out); err != nil {
		return ""
	}
	return out
}

func xaddrOf(data []byte) string {
	var v struct {
		XAddr string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	_ = xml.Unmarshal(data, &v) // an answer that cannot be read leaves "": the usual path is used
	return strings.TrimSpace(v.XAddr)
}

func streamURIOf(data []byte) string {
	var v struct {
		URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
	}
	_ = xml.Unmarshal(data, &v) // an answer that cannot be read leaves "": the profile is dropped
	return strings.TrimSpace(v.URI)
}

func parseDeviceInformation(data []byte) (manufacturer, model, firmware string) {
	var v struct {
		Manufacturer string `xml:"Body>GetDeviceInformationResponse>Manufacturer"`
		Model        string `xml:"Body>GetDeviceInformationResponse>Model"`
		Firmware     string `xml:"Body>GetDeviceInformationResponse>FirmwareVersion"`
	}
	_ = xml.Unmarshal(data, &v) // an answer that cannot be read leaves the fields empty
	return clip(strings.TrimSpace(v.Manufacturer), 80), clip(strings.TrimSpace(v.Model), 80), clip(strings.TrimSpace(v.Firmware), 80)
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
			PTZ *struct {
				Token string `xml:"token,attr"`
			} `xml:"PTZConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	_ = xml.Unmarshal(data, &v) // an answer that cannot be read leaves no profiles
	out := []Profile{}
	for _, pr := range v.Profiles {
		if pr.Token == "" || len(out) >= 16 {
			continue
		}
		out = append(out, Profile{
			Token: clip(pr.Token, 80), Name: clip(pr.Name, 80),
			Width: dimension(fmt.Sprint(pr.Video.Resolution.Width)), Height: dimension(fmt.Sprint(pr.Video.Resolution.Height)),
			Codec: strings.ToUpper(clip(pr.Video.Encoding, 16)), PTZ: pr.PTZ != nil,
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
	return time.Until(time.Date(x.Year, time.Month(x.Month), x.Day, x.Hour, x.Minute, x.Second, 0, time.UTC))
}

// PickStreams chooses main (largest picture) and sub (smallest, if it is
// smaller) from the profiles of a camera.
func PickStreams(profiles []Profile) (mainURL, subURL string) {
	if len(profiles) == 0 {
		return "", ""
	}
	list := slices.Clone(profiles)
	slices.SortStableFunc(list, func(a, b Profile) int { return b.Width*b.Height - a.Width*a.Height })
	mainURL = list[0].URL
	if last := list[len(list)-1]; last.URL != mainURL && last.Width*last.Height < list[0].Width*list[0].Height {
		subURL = last.URL
	}
	return mainURL, subURL
}
