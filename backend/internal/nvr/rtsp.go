package nvr

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The outcomes of asking a camera.
const (
	OutcomeOK = "ok"
	// OutcomeUnauthorized: the camera refused user or password.
	OutcomeUnauthorized = "unauthorized"
	// OutcomeLocked: the camera says the account is locked (too many
	// wrong logins).
	OutcomeLocked = "locked"
	// OutcomeNotFound: the camera answers, but not for this path.
	OutcomeNotFound = "not_found"
	// OutcomeUnreachable: nothing answered at the address.
	OutcomeUnreachable = "unreachable"
	// OutcomeOther: an answer TitanOS does not understand.
	OutcomeOther = "other"
)

// StreamInfo is what a DESCRIBE showed of a stream.
type StreamInfo struct {
	Outcome string `json:"outcome"`
	// Codec is the video codec ("H264", "H265"); Audio the audio codec;
	// both "" if the description names none.
	Codec string `json:"codec"`
	Audio string `json:"audio"`
	// Width and Height are 0 when the description does not say.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ProbeRTSP asks a camera for the description of one stream (RTSP
// DESCRIBE): it shows that the address and the login are right and names
// the codec. Credentials travel in the Authorization header (Digest if
// the camera offers it), never in the request line; the request is made
// once, so a wrong password costs one attempt at the camera. An rtsps
// address is asked over TLS without checking the certificate: cameras
// sign their own.
func (p *Prober) ProbeRTSP(ctx context.Context, address, user, password string) (StreamInfo, error) {
	if _, err := streamURL(address); err != nil {
		return StreamInfo{}, invalid("stream address: %v", err)
	}
	u, _ := url.Parse(address) // parsed by streamURL above
	port := 554
	if u.Scheme == "rtsps" {
		port = 322
	}
	if ps := u.Port(); ps != "" {
		port = atoi(ps)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*p.timeout())
	defer cancel()
	conn, err := p.dial(ctx, u.Hostname(), port)
	if err != nil {
		if errors.Is(err, ErrTargetNotAllowed) {
			return StreamInfo{}, err
		}
		return StreamInfo{Outcome: OutcomeUnreachable}, nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * p.timeout()))
	if u.Scheme == "rtsps" {
		secure := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: u.Hostname()}) //nolint:gosec // cameras present self-signed certificates; the probe only tests address and login
		if err := secure.HandshakeContext(ctx); err != nil {
			return StreamInfo{Outcome: OutcomeUnreachable}, nil
		}
		conn = secure
	}
	target := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}).String()
	reader := bufio.NewReader(conn)
	send := func(auth string, cseq int) (int, map[string]string, string, error) {
		req := fmt.Sprintf("DESCRIBE %s RTSP/1.0\r\nCSeq: %d\r\nAccept: application/sdp\r\nUser-Agent: TitanOS\r\n", target, cseq)
		if auth != "" {
			req += "Authorization: " + auth + "\r\n"
		}
		if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
			return 0, nil, "", err
		}
		return readRTSPResponse(reader)
	}
	status, head, body, err := send("", 1)
	if err != nil {
		return StreamInfo{Outcome: OutcomeUnreachable}, nil
	}
	if status == 401 && user != "" {
		auth := rtspAuth(head["www-authenticate"], user, password, "DESCRIBE", target)
		if auth == "" {
			return StreamInfo{Outcome: OutcomeOther}, nil
		}
		if status, _, body, err = send(auth, 2); err != nil {
			return StreamInfo{Outcome: OutcomeUnreachable}, nil
		}
	}
	switch status {
	case 200:
		info := parseSDP(body)
		info.Outcome = OutcomeOK
		return info, nil
	case 401:
		return StreamInfo{Outcome: OutcomeUnauthorized}, nil
	case 403, 423:
		// Some cameras answer a locked account with 403 rather than 401.
		return StreamInfo{Outcome: OutcomeLocked}, nil
	case 404:
		return StreamInfo{Outcome: OutcomeNotFound}, nil
	}
	return StreamInfo{Outcome: OutcomeOther}, nil
}

func readRTSPResponse(r *bufio.Reader) (int, map[string]string, string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "RTSP/") {
		return 0, nil, "", errors.New("nvr: not an RTSP answer")
	}
	status, _ := strconv.Atoi(fields[1]) // 0 for a status that is no number: handled as "other"
	head := map[string]string{}
	for range 100 {
		l, err := r.ReadString('\n')
		if err != nil {
			return 0, nil, "", err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok {
			k = strings.ToLower(strings.TrimSpace(k))
			// Digest is preferred when a camera offers both.
			if old, dup := head[k]; !dup || (k == "www-authenticate" && !strings.HasPrefix(strings.ToLower(old), "digest")) {
				head[k] = strings.TrimSpace(v)
			}
		}
	}
	var body string
	if n, _ := strconv.Atoi(head["content-length"]); n > 0 && n < 64<<10 {
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, nil, "", err
		}
		body = string(buf)
	}
	return status, head, body, nil
}

// parseSDP reads codecs and, where the camera names it, the picture size
// from a stream description.
func parseSDP(sdp string) StreamInfo {
	var info StreamInfo
	media := ""
	for _, l := range strings.Split(sdp, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "m="):
			media, _, _ = strings.Cut(strings.TrimPrefix(l, "m="), " ")
		case strings.HasPrefix(l, "a=rtpmap:"):
			if _, rest, ok := strings.Cut(l, " "); ok {
				name, _, _ := strings.Cut(rest, "/")
				name = strings.ToUpper(name)
				if media == "video" && info.Codec == "" {
					info.Codec = name
				}
				if media == "audio" && info.Audio == "" {
					info.Audio = name
				}
			}
		case media == "video" && strings.HasPrefix(l, "a=x-dimensions:"):
			// a=x-dimensions:1920,1080
			w, h, _ := strings.Cut(strings.TrimPrefix(l, "a=x-dimensions:"), ",")
			info.Width, info.Height = dimension(w), dimension(h)
		case media == "video" && strings.HasPrefix(l, "a=framesize:"):
			// a=framesize:96 1920-1080
			if _, size, ok := strings.Cut(l, " "); ok {
				w, h, _ := strings.Cut(size, "-")
				info.Width, info.Height = dimension(w), dimension(h)
			}
		}
	}
	if info.Codec == "H265" || info.Codec == "HEVC" {
		info.Codec = "H265"
	}
	return info
}

func dimension(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 16384 {
		return 0
	}
	return n
}

// rtspAuth answers a challenge, Digest preferred.
func rtspAuth(challenge, user, password, method, uri string) string {
	lower := strings.ToLower(challenge)
	if strings.HasPrefix(lower, "digest") {
		params := parseAuthParams(challenge[len("digest"):])
		realm, nonce := params["realm"], params["nonce"]
		ha1 := md5hex(user + ":" + realm + ":" + password)
		ha2 := md5hex(method + ":" + uri)
		return fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`, user, realm, nonce, uri, md5hex(ha1+":"+nonce+":"+ha2))
	}
	if strings.HasPrefix(lower, "basic") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	}
	return ""
}

func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

// md5hex: RTSP Digest authentication is defined over MD5 (RFC 2617).
func md5hex(s string) string { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }

func hostPort(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
