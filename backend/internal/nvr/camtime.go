package nvr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TimeResult says how a camera was pointed at the NAS's time server.
type TimeResult struct {
	// Method is "onvif", "hikvision" or "dahua".
	Method string `json:"method"`
	// Rejected and Locked are counted as in a search.
	Rejected int  `json:"rejected"`
	Locked   bool `json:"locked"`
}

// ErrNoTimeMethod is returned when no way of setting the time worked.
var ErrNoTimeMethod = errors.New("nvr: the camera accepts none of the known ways to set its time server")

// SetTimeServer tells the camera to ask server (an address of this NAS) for
// the time. ONVIF first, then the vendor's own interface. Each way is tried
// with the same credentials and counts against the same budget of rejected
// logins as a search.
func (p *Prober) SetTimeServer(ctx context.Context, host string, onvifPort int, vendor, user, password, server string) (TimeResult, error) {
	var res TimeResult
	if err := p.resolve(ctx, host); err != nil {
		return res, err
	}
	budget := NewAuthBudget(DefaultAuthAttempts)
	note := func(o Outcome) {
		budget.Record(o)
		res.Rejected, res.Locked = budget.Rejected(), budget.Locked()
	}
	if onvifPort > 0 && budget.Allow() {
		o, err := p.onvifSetNTP(ctx, host, onvifPort, user, password, server)
		note(o)
		if err == nil && o == OutcomeOK {
			res.Method = "onvif"
			return res, nil
		}
	}
	for _, v := range []string{"hikvision", "dahua"} {
		if vendor != "" && vendor != v || !budget.Allow() {
			continue
		}
		var o Outcome
		var err error
		if v == "hikvision" {
			o, err = p.hikvisionNTP(ctx, host, user, password, server)
		} else {
			o, err = p.dahuaNTP(ctx, host, user, password, server)
		}
		note(o)
		if err == nil && o == OutcomeOK {
			res.Method = v
			return res, nil
		}
	}
	return res, ErrNoTimeMethod
}

func (p *Prober) onvifSetNTP(ctx context.Context, host string, port int, user, password, server string) (Outcome, error) {
	client := p.httpClient()
	device := "http://" + hostPort(host, port) + "/onvif/device_service"
	var offset time.Duration
	if st, data, err := p.soap(ctx, client, device, "", "", 0, `<GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/>`); err == nil && st == 200 {
		offset = parseCameraClock(data)
	}
	kind := "IPv4"
	field := "IPv4Address"
	if strings.Contains(server, ":") {
		kind, field = "IPv6", "IPv6Address"
	}
	body := `<SetNTP xmlns="http://www.onvif.org/ver10/device/wsdl"><FromDHCP>false</FromDHCP><NTPManual xmlns="http://www.onvif.org/ver10/device/wsdl"><Type xmlns="http://www.onvif.org/ver10/schema">` + kind + `</Type><` + field + ` xmlns="http://www.onvif.org/ver10/schema">` + xmlEscape(server) + `</` + field + `></NTPManual></SetNTP>`
	st, data, err := p.soap(ctx, client, device, user, password, offset, body)
	if err != nil {
		return OutcomeOther, err
	}
	if o := soapOutcome(st, data); o != OutcomeOK {
		return o, fmt.Errorf("nvr: ONVIF refused")
	}
	// The camera takes its time from the server only once the type is NTP.
	st, data, err = p.soap(ctx, client, device, user, password, offset,
		`<SetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"><DateTimeType>NTP</DateTimeType><DaylightSavings>false</DaylightSavings></SetSystemDateAndTime>`)
	if err != nil {
		return OutcomeOther, err
	}
	return soapOutcome(st, data), nil
}

func (p *Prober) hikvisionNTP(ctx context.Context, host, user, password, server string) (Outcome, error) {
	addrType := "ipaddress"
	if strings.Contains(server, ":") {
		addrType = "ipv6address"
	}
	ntp := `<?xml version="1.0" encoding="UTF-8"?><NTPServer><id>1</id><addressingFormatType>` + addrType + `</addressingFormatType><ipAddress>` + xmlEscape(server) + `</ipAddress><portNo>123</portNo><synchronizeInterval>60</synchronizeInterval></NTPServer>`
	o, err := p.digestRequest(ctx, host, http.MethodPut, "/ISAPI/System/time/ntpServers/1", ntp, "application/xml", user, password)
	if err != nil || o != OutcomeOK {
		return o, err
	}
	return p.digestRequest(ctx, host, http.MethodPut, "/ISAPI/System/time", `<?xml version="1.0" encoding="UTF-8"?><Time><timeMode>NTP</timeMode></Time>`, "application/xml", user, password)
}

func (p *Prober) dahuaNTP(ctx context.Context, host, user, password, server string) (Outcome, error) {
	q := url.Values{"action": {"setConfig"}, "NTP.Enable": {"true"}, "NTP.Address": {server}, "NTP.Port": {"123"}, "NTP.UpdatePeriod": {"60"}}
	return p.digestRequest(ctx, host, http.MethodGet, "/cgi-bin/configManager.cgi?"+q.Encode(), "", "", user, password)
}

// digestRequest sends a request to port 80 with HTTP digest login.
func (p *Prober) digestRequest(ctx context.Context, host, method, pathQuery, body, contentType, user, password string) (Outcome, error) {
	client := p.httpClient()
	port := 80
	if p.HTTPPort > 0 {
		port = p.HTTPPort
	}
	target := "http://" + hostPort(host, port) + pathQuery
	do := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return client.Do(req)
	}
	resp, err := do("")
	if err != nil {
		return OutcomeOther, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()
		auth := httpDigest(challenge, user, password, method, pathQuery)
		if auth == "" {
			return OutcomeOther, errors.New("nvr: login method not supported")
		}
		if resp, err = do(auth); err != nil {
			return OutcomeOther, err
		}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		if strings.Contains(strings.ToLower(string(data)), "lock") {
			return OutcomeLocked, errors.New("nvr: account locked")
		}
		return OutcomeUnauthorized, errors.New("nvr: login refused")
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if strings.Contains(string(data), "Error") && !strings.Contains(string(data), "<statusCode>1</statusCode>") && !strings.Contains(string(data), "<statusString>OK") {
			return OutcomeOther, errors.New("nvr: camera reported an error")
		}
		return OutcomeOK, nil
	}
	return OutcomeOther, fmt.Errorf("nvr: camera answered %d", resp.StatusCode)
}

// httpDigest answers a Digest challenge (qop auth, MD5).
func httpDigest(challenge, user, password, method, uri string) string {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest") {
		return ""
	}
	params := parseAuthParams(challenge[len("digest"):])
	realm, nonce, opaque := params["realm"], params["nonce"], params["opaque"]
	cnonceRaw := make([]byte, 8)
	_, _ = rand.Read(cnonceRaw)
	cnonce := hex.EncodeToString(cnonceRaw)
	ha1 := md5hex(user + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	var resp, qop string
	if strings.Contains(params["qop"], "auth") {
		qop = "auth"
		resp = md5hex(ha1 + ":" + nonce + ":00000001:" + cnonce + ":auth:" + ha2)
	} else {
		resp = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	out := fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`, user, realm, nonce, uri, resp)
	if qop != "" {
		out += fmt.Sprintf(`, qop=auth, nc=00000001, cnonce=%q`, cnonce)
	}
	if opaque != "" {
		out += fmt.Sprintf(`, opaque=%q`, opaque)
	}
	return out
}

// LocalAddrFor is the address of this machine that the camera sees: the one
// the system would use to reach it. Nothing is sent.
func LocalAddrFor(host string) (string, error) { return netDialUDP(host) }
