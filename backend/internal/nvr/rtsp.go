package nvr

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// RTSPResult is what a DESCRIBE showed.
type RTSPResult struct {
	Outcome Outcome
	Status  int
	// Video is the codec named in the description ("H264", "H265"), empty if none.
	Video string
}

// ProbeRTSP asks the camera for the description of one stream. Credentials
// travel in the Authorization header only; the request line has none.
func (p *Prober) ProbeRTSP(ctx context.Context, host string, port int, path, user, password string) (RTSPResult, error) {
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	target := "rtsp://" + hostPort(host, port) + path
	conn, err := p.dial(ctx, host, port)
	if err != nil {
		return RTSPResult{}, err
	}
	defer conn.Close()
	deadline := p.timeout() * 2
	_ = conn.SetDeadline(nowPlus(deadline))
	r := bufio.NewReader(conn)

	send := func(auth string, cseq int) (int, map[string]string, string, error) {
		req := fmt.Sprintf("DESCRIBE %s RTSP/1.0\r\nCSeq: %d\r\nAccept: application/sdp\r\nUser-Agent: TitanOS\r\n", target, cseq)
		if auth != "" {
			req += "Authorization: " + auth + "\r\n"
		}
		if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
			return 0, nil, "", err
		}
		return readRTSPResponse(r)
	}
	status, head, body, err := send("", 1)
	if err != nil {
		return RTSPResult{}, err
	}
	if status == 401 && user != "" {
		auth := rtspAuth(head["www-authenticate"], user, password, "DESCRIBE", target)
		if auth == "" {
			return RTSPResult{Outcome: OutcomeOther, Status: status}, nil
		}
		status, _, body, err = send(auth, 2)
		if err != nil {
			return RTSPResult{}, err
		}
	}
	res := RTSPResult{Status: status}
	switch {
	case status == 200:
		res.Outcome = OutcomeOK
		res.Video = sdpVideo(body)
	case status == 401:
		res.Outcome = OutcomeUnauthorized
	case status == 403 || status == 423:
		// Some cameras answer a locked account with 403 rather than 401.
		res.Outcome = OutcomeLocked
	}
	return res, nil
}

func readRTSPResponse(r *bufio.Reader) (int, map[string]string, string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}
	f := strings.Fields(line)
	if len(f) < 2 || !strings.HasPrefix(f[0], "RTSP/") {
		return 0, nil, "", fmt.Errorf("nvr: not an RTSP answer")
	}
	status, _ := strconv.Atoi(f[1])
	head := map[string]string{}
	for {
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
			if _, dup := head[k]; !dup {
				head[k] = strings.TrimSpace(v)
			}
		}
	}
	var body string
	if n, _ := strconv.Atoi(head["content-length"]); n > 0 && n < 64<<10 {
		buf := make([]byte, n)
		if _, err := readFull(r, buf); err != nil {
			return 0, nil, "", err
		}
		body = string(buf)
	}
	return status, head, body, nil
}

func sdpVideo(sdp string) string {
	inVideo := false
	for _, l := range strings.Split(sdp, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "m=") {
			inVideo = strings.HasPrefix(l, "m=video")
		}
		if inVideo && strings.HasPrefix(l, "a=rtpmap:") {
			if _, rest, ok := strings.Cut(l, " "); ok {
				name, _, _ := strings.Cut(rest, "/")
				return strings.ToUpper(name)
			}
		}
	}
	return ""
}

// rtspAuth answers a challenge, Digest preferred.
func rtspAuth(challenge, user, password, method, uri string) string {
	lower := strings.ToLower(challenge)
	if strings.HasPrefix(lower, "digest") {
		params := parseAuthParams(challenge[len("digest"):])
		realm, nonce := params["realm"], params["nonce"]
		ha1 := md5hex(user + ":" + realm + ":" + password)
		ha2 := md5hex(method + ":" + uri)
		resp := md5hex(ha1 + ":" + nonce + ":" + ha2)
		return fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`, user, realm, nonce, uri, resp)
	}
	if strings.HasPrefix(lower, "basic") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	}
	return ""
}

func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

func md5hex(s string) string { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }

// pathOf is the path and query of an RTSP address.
func pathOf(address string) (host string, port int, path string, err error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return "", 0, "", fmt.Errorf("nvr: not a stream address")
	}
	port = 554
	if ps := u.Port(); ps != "" {
		port, _ = strconv.Atoi(ps)
	}
	path = u.EscapedPath()
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return u.Hostname(), port, path, nil
}

var _ = context.Background

// AddressOf is the host, port and path of a stream address.
func AddressOf(address string) (host string, port int, path string, err error) {
	return pathOf(address)
}
