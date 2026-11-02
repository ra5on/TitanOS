package nvr

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCamera speaks just enough RTSP and ONVIF to be searched.
type fakeCamera struct {
	user, password string
	paths          map[string]bool // RTSP paths that exist
	locked         atomic.Bool
	rejected       atomic.Int32
	rtspAddr       string
	onvifAddr      string
	lockAfter      int32
	ntp, timeType  string
	setEnc         string
}

func (c *fakeCamera) reject() bool {
	n := c.rejected.Add(1)
	if c.lockAfter > 0 && n >= c.lockAfter {
		c.locked.Store(true)
	}
	return c.locked.Load()
}

func startFakeCamera(t *testing.T, c *fakeCamera) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	c.rtspAddr = l.Addr().String()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go c.serveRTSP(conn)
		}
	}()
	_, port, _ := net.SplitHostPort(c.rtspAddr)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.serveONVIF(w, r, port)
	}))
	t.Cleanup(srv.Close)
	c.onvifAddr = strings.TrimPrefix(srv.URL, "http://")
}

func (c *fakeCamera) serveRTSP(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return
		}
		uri := f[1]
		auth := ""
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			l = strings.TrimRight(l, "\r\n")
			if l == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(l), "authorization:") {
				auth = l
			}
		}
		path := strings.TrimPrefix(uri, "rtsp://"+c.rtspAddr)
		switch {
		case c.locked.Load():
			fmt.Fprint(conn, "RTSP/1.0 403 Forbidden\r\nCSeq: 1\r\n\r\n")
		case auth == "":
			fmt.Fprint(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: 1\r\nWWW-Authenticate: Digest realm=\"cam\", nonce=\"abc\"\r\n\r\n")
		case !strings.Contains(auth, `response="`+md5hex(md5hex(c.user+":cam:"+c.password)+":abc:"+md5hex("DESCRIBE:"+uri))+`"`):
			if c.reject() {
				fmt.Fprint(conn, "RTSP/1.0 403 Forbidden\r\nCSeq: 2\r\n\r\n")
			} else {
				fmt.Fprint(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: 2\r\nWWW-Authenticate: Digest realm=\"cam\", nonce=\"abc\"\r\n\r\n")
			}
		case !c.paths[path]:
			fmt.Fprint(conn, "RTSP/1.0 404 Not Found\r\nCSeq: 2\r\n\r\n")
		default:
			sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n"
			fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: 2\r\nContent-Length: %d\r\n\r\n%s", len(sdp), sdp)
		}
	}
}

func (c *fakeCamera) serveONVIF(w http.ResponseWriter, r *http.Request, rtspPort string) {
	buf := make([]byte, 1<<16)
	n, _ := r.Body.Read(buf)
	body := string(buf[:n])
	reply := func(s string) {
		fmt.Fprint(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>`+s+`</s:Body></s:Envelope>`)
	}
	if strings.Contains(body, "GetSystemDateAndTime") {
		now := time.Now().UTC()
		reply(fmt.Sprintf(`<GetSystemDateAndTimeResponse><SystemDateAndTime><UTCDateTime><Time><Hour>%d</Hour><Minute>%d</Minute><Second>%d</Second></Time><Date><Year>%d</Year><Month>%d</Month><Day>%d</Day></Date></UTCDateTime></SystemDateAndTime></GetSystemDateAndTimeResponse>`,
			now.Hour(), now.Minute(), now.Second(), now.Year(), int(now.Month()), now.Day()))
		return
	}
	if c.locked.Load() {
		w.WriteHeader(400)
		fmt.Fprint(w, "account locked")
		return
	}
	if !c.validDigest(body) {
		if c.reject() {
			w.WriteHeader(400)
			fmt.Fprint(w, "too many failed logins, account locked")
			return
		}
		w.WriteHeader(400)
		fmt.Fprint(w, `<s:Fault><s:Reason>Sender not Authorized</s:Reason></s:Fault>`)
		return
	}
	switch {
	case strings.Contains(body, "GetVideoEncoderConfigurationOptions"):
		reply(`<GetVideoEncoderConfigurationOptionsResponse><Options><JPEG><ResolutionsAvailable><Width>640</Width><Height>480</Height></ResolutionsAvailable><FrameRateRange><Min>1</Min><Max>5</Max></FrameRateRange></JPEG><H264><ResolutionsAvailable><Width>2560</Width><Height>1440</Height></ResolutionsAvailable><ResolutionsAvailable><Width>1280</Width><Height>720</Height></ResolutionsAvailable><FrameRateRange><Min>1</Min><Max>25</Max></FrameRateRange></H264><Extension><H264><BitrateRange><Min>256</Min><Max>8192</Max></BitrateRange></H264></Extension></Options></GetVideoEncoderConfigurationOptionsResponse>`)
	case strings.Contains(body, "SetVideoEncoderConfiguration"):
		c.setEnc = body
		reply(`<SetVideoEncoderConfigurationResponse/>`)
	case strings.Contains(body, "GetVideoEncoderConfigurations"):
		reply(`<GetVideoEncoderConfigurationsResponse><Configurations token="enc_main"><Name>main</Name><UseCount>1</UseCount><Encoding>H264</Encoding><Resolution><Width>2560</Width><Height>1440</Height></Resolution><Quality>5</Quality><RateControl><FrameRateLimit>25</FrameRateLimit><EncodingInterval>1</EncodingInterval><BitrateLimit>6144</BitrateLimit></RateControl><H264><GovLength>50</GovLength><H264Profile>High</H264Profile></H264><Multicast><Address><Type>IPv4</Type><IPv4Address>239.0.0.1</IPv4Address></Address><Port>5000</Port><TTL>2</TTL><AutoStart>false</AutoStart></Multicast><SessionTimeout>PT30S</SessionTimeout></Configurations></GetVideoEncoderConfigurationsResponse>`)
	case strings.Contains(body, "<SetNTP"):
		c.ntp = body
		reply(`<SetNTPResponse/>`)
	case strings.Contains(body, "SetSystemDateAndTime"):
		c.timeType = body
		reply(`<SetSystemDateAndTimeResponse/>`)
	case strings.Contains(body, "GetCapabilities"):
		reply(`<GetCapabilitiesResponse><Capabilities><Media><XAddr>http://10.9.9.9/onvif/media_service</XAddr></Media></Capabilities></GetCapabilitiesResponse>`)
	case strings.Contains(body, "GetProfiles"):
		reply(`<GetProfilesResponse>` +
			`<Profiles token="p_sub"><Name>sub</Name><VideoEncoderConfiguration><Encoding>H264</Encoding><Resolution><Width>640</Width><Height>360</Height></Resolution></VideoEncoderConfiguration></Profiles>` +
			`<Profiles token="p_main"><Name>main</Name><VideoEncoderConfiguration><Encoding>H265</Encoding><Resolution><Width>2560</Width><Height>1440</Height></Resolution></VideoEncoderConfiguration></Profiles>` +
			`</GetProfilesResponse>`)
	case strings.Contains(body, "GetStreamUri"):
		path := "/main"
		if strings.Contains(body, "p_sub") {
			path = "/sub"
		}
		reply(`<GetStreamUriResponse><MediaUri><Uri>rtsp://10.9.9.9:` + rtspPort + path + `</Uri></MediaUri></GetStreamUriResponse>`)
	}
}

func (c *fakeCamera) validDigest(body string) bool {
	pick := func(tag string) string {
		i := strings.Index(body, "<"+tag+">")
		if i < 0 {
			i = strings.Index(body, "<"+tag+" ")
		}
		if i < 0 {
			return ""
		}
		j := strings.Index(body[i:], ">")
		k := strings.Index(body[i+j:], "</")
		return body[i+j+1 : i+j+k]
	}
	nonce, _ := base64.StdEncoding.DecodeString(pick("wsse:Nonce"))
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(pick("wsu:Created")))
	h.Write([]byte(c.password))
	return pick("wsse:Username") == c.user && pick("wsse:Password") == base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func loopbackOnly(a netip.Addr) bool { return a.IsLoopback() }

func loopbackProber(c *fakeCamera) *Prober {
	_, rtspPort, _ := net.SplitHostPort(c.rtspAddr)
	_, onvifPort, _ := net.SplitHostPort(c.onvifAddr)
	var op, rp int
	fmt.Sscan(onvifPort, &op)
	fmt.Sscan(rtspPort, &rp)
	return &Prober{
		Allow:      func(a netip.Addr) bool { return a.IsLoopback() },
		Timeout:    time.Second,
		ONVIFPorts: []int{op},
		Table:      []Candidate{{Vendor: "test", Port: rp, Path: "/main", Role: "main"}, {Vendor: "test", Port: rp, Path: "/sub", Role: "sub"}},
	}
}

func TestDiscoverONVIFFindsMainAndSub(t *testing.T) {
	c := &fakeCamera{user: "view", password: "pa ss/word", paths: map[string]bool{"/main": true, "/sub": true}}
	startFakeCamera(t, c)
	res, err := loopbackProber(c).Discover(context.Background(), Request{Host: "127.0.0.1", User: "view", Password: "pa ss/word"})
	if err != nil || !res.Found {
		t.Fatalf("not found: %+v err=%v", res, err)
	}
	if len(res.Streams) != 2 || res.Streams[0].Role != "main" || res.Streams[0].Width != 2560 || res.Streams[1].Role != "sub" || res.Streams[1].Height != 360 {
		t.Fatalf("streams: %+v", res.Streams)
	}
	for _, s := range res.Streams {
		if strings.Contains(s.URL, "@") || strings.Contains(s.URL, "10.9.9.9") {
			t.Fatalf("stream address must carry no credentials and the host we reached: %s", s.URL)
		}
	}
	if res.Rejected != 0 || res.ONVIFPort == 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestDiscoverWrongPasswordStopsAfterBudget(t *testing.T) {
	c := &fakeCamera{user: "view", password: "right", paths: map[string]bool{"/main": true}}
	startFakeCamera(t, c)
	res, err := loopbackProber(c).Discover(context.Background(), Request{Host: "127.0.0.1", User: "view", Password: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Found || res.Rejected > DefaultAuthAttempts {
		t.Fatalf("must give up within %d rejected logins: %+v", DefaultAuthAttempts, res)
	}
	if got := int(c.rejected.Load()); got > DefaultAuthAttempts {
		t.Fatalf("camera saw %d rejected logins", got)
	}
}

func TestDiscoverStopsAtLockedAccount(t *testing.T) {
	c := &fakeCamera{user: "view", password: "right", paths: map[string]bool{"/main": true}, lockAfter: 1}
	startFakeCamera(t, c)
	res, _ := loopbackProber(c).Discover(context.Background(), Request{Host: "127.0.0.1", User: "view", Password: "wrong", Attempts: 10})
	if !res.Locked || res.Found {
		t.Fatalf("lock must be reported: %+v", res)
	}
	if got := int(c.rejected.Load()); got != 1 {
		t.Fatalf("search went on after the lock: %d rejected", got)
	}
}

func TestDiscoverRTSPFallbackWithoutONVIF(t *testing.T) {
	c := &fakeCamera{user: "view", password: "pw", paths: map[string]bool{"/main": true, "/sub": true}}
	startFakeCamera(t, c)
	p := loopbackProber(c)
	p.ONVIFPorts = []int{1} // nothing listens
	res, err := p.Discover(context.Background(), Request{Host: "127.0.0.1", User: "view", Password: "pw"})
	if err != nil || len(res.Streams) != 2 || res.Streams[0].Source != "test" {
		t.Fatalf("fallback: %+v err=%v", res, err)
	}
}

func TestDiscoverGivenLinkTriesOnlyThatLink(t *testing.T) {
	c := &fakeCamera{user: "view", password: "pw", paths: map[string]bool{"/odd": true}}
	startFakeCamera(t, c)
	link := "rtsp://" + c.rtspAddr + "/odd"
	res, err := loopbackProber(c).Discover(context.Background(), Request{StreamURL: link, User: "view", Password: "pw"})
	if err != nil || !res.Found || res.Streams[0].URL != link || res.Streams[0].Source != "link" {
		t.Fatalf("link: %+v err=%v", res, err)
	}
}

func TestDiscoverRefusesOutsideHomeNetwork(t *testing.T) {
	p := &Prober{}
	for _, host := range []string{"8.8.8.8", "127.0.0.1", "169.254.169.254"} {
		if _, err := p.Discover(context.Background(), Request{Host: host, User: "u", Password: "p"}); err == nil {
			t.Fatalf("%s must be refused", host)
		}
	}
	if _, err := p.Discover(context.Background(), Request{StreamURL: "rtsp://8.8.8.8/x", User: "u"}); err == nil {
		t.Fatal("link outside the home network must be refused")
	}
}

func TestRTSPAuthPrefersDigestOverBasic(t *testing.T) {
	got := rtspAuth("Basic realm=\"cam\"\nDigest realm=\"cam\", nonce=\"n\"", "u", "p", "DESCRIBE", "rtsp://x/y")
	if !strings.HasPrefix(got, "Digest ") {
		t.Fatalf("password sent as %q", got)
	}
	if got := rtspAuth("Basic realm=\"cam\"", "u", "p", "DESCRIBE", "rtsp://x/y"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("basic only: %q", got)
	}
	if got := httpDigest(`Digest realm="r", nonce="n", qop="auth", algorithm=SHA-256`, "u", "p", "GET", "/"); !strings.Contains(got, "algorithm=SHA-256") {
		t.Fatalf("sha-256: %q", got)
	}
}
