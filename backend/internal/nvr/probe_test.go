package nvr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func loopbackProber(t *testing.T) *Prober {
	loopbackIsCamera = true
	t.Cleanup(func() { loopbackIsCamera = false })
	return &Prober{Allow: func(netip.Addr) bool { return true }, Timeout: 2 * time.Second}
}

const testSDP = "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=Media\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=x-dimensions:2560,1440\r\nm=audio 0 RTP/AVP 97\r\na=rtpmap:97 MPEG4-GENERIC/16000\r\n"

// rtspServer answers DESCRIBE like a camera: Digest (or Basic) login for
// user "viewer" with password "geheim". It returns its address and a
// counter of requests that carried a login.
func rtspServer(t *testing.T, scheme string) (address string, logins *int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	count := 0
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					fields := strings.Fields(line)
					auth, cseq := "", "0"
					for {
						l, err := r.ReadString('\n')
						if err != nil {
							return
						}
						l = strings.TrimSpace(l)
						if l == "" {
							break
						}
						if v, ok := strings.CutPrefix(l, "Authorization: "); ok {
							auth = v
						}
						if v, ok := strings.CutPrefix(l, "CSeq: "); ok {
							cseq = v
						}
					}
					if len(fields) < 2 || fields[0] != "DESCRIBE" {
						return
					}
					target := fields[1]
					// The request line never carries the login.
					if strings.Contains(target, "@") {
						fmt.Fprintf(conn, "RTSP/1.0 400 Bad Request\r\nCSeq: %s\r\n\r\n", cseq)
						continue
					}
					switch {
					case strings.Contains(target, "/missing"):
						fmt.Fprintf(conn, "RTSP/1.0 404 Not Found\r\nCSeq: %s\r\n\r\n", cseq)
					case auth == "":
						challenge := `Digest realm="cam", nonce="abc123"`
						if scheme == "basic" {
							challenge = `Basic realm="cam"`
						}
						fmt.Fprintf(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: %s\r\n\r\n", cseq, challenge)
					default:
						count++
						want := rtspAuth(`Digest realm="cam", nonce="abc123"`, "viewer", "geheim", "DESCRIBE", target)
						if scheme == "basic" {
							want = rtspAuth("Basic", "viewer", "geheim", "DESCRIBE", target)
						}
						if auth != want {
							fmt.Fprintf(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\n\r\n", cseq)
							continue
						}
						fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, len(testSDP), testSDP)
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), &count
}

func TestProbeRTSP(t *testing.T) {
	for _, scheme := range []string{"digest", "basic"} {
		address, logins := rtspServer(t, scheme)
		p := loopbackProber(t)
		info, err := p.ProbeRTSP(context.Background(), "rtsp://"+address+"/live?channel=1", "viewer", "geheim")
		if err != nil || info.Outcome != OutcomeOK || info.Codec != "H265" || info.Audio != "MPEG4-GENERIC" || info.Width != 2560 || info.Height != 1440 {
			t.Errorf("%s: %+v %v", scheme, info, err)
		}
		// A wrong password costs exactly one attempt at the camera.
		before := *logins
		info, err = p.ProbeRTSP(context.Background(), "rtsp://"+address+"/live", "viewer", "falsch")
		if err != nil || info.Outcome != OutcomeUnauthorized || *logins != before+1 {
			t.Errorf("%s wrong password: %+v %v, %d attempts", scheme, info, err, *logins-before)
		}
		if info, _ := p.ProbeRTSP(context.Background(), "rtsp://"+address+"/live", "", ""); info.Outcome != OutcomeUnauthorized {
			t.Errorf("%s without login: %+v", scheme, info)
		}
		if info, _ := p.ProbeRTSP(context.Background(), "rtsp://"+address+"/missing", "viewer", "geheim"); info.Outcome != OutcomeNotFound {
			t.Errorf("%s missing path: %+v", scheme, info)
		}
	}
	// Nothing listens.
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := listener.Addr().String()
	listener.Close()
	if info, err := loopbackProber(t).ProbeRTSP(context.Background(), "rtsp://"+dead+"/x", "", ""); err != nil || info.Outcome != OutcomeUnreachable {
		t.Errorf("dead port: %+v %v", info, err)
	}
	if _, err := loopbackProber(t).ProbeRTSP(context.Background(), "http://192.168.1.2/x", "", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("http address: %v", err)
	}
}

func TestProbeStaysInTheHomeNetwork(t *testing.T) {
	address, _ := rtspServer(t, "digest")
	// The rule of a NAS: this host is never a camera …
	p := &Prober{Timeout: time.Second}
	if _, err := p.ProbeRTSP(context.Background(), "rtsp://"+address+"/live", "viewer", "geheim"); err == nil {
		t.Error("loopback must be refused by address validation")
	}
	if _, err := p.dial(context.Background(), "127.0.0.1", 554); !errors.Is(err, ErrTargetNotAllowed) {
		t.Errorf("dial loopback: %v", err)
	}
	// … a public address needs the administrator's confirmation …
	if _, err := p.ProbeRTSP(context.Background(), "rtsp://203.0.113.7/live", "viewer", "geheim"); !errors.Is(err, ErrTargetNotAllowed) {
		t.Errorf("public address: %v", err)
	}
	// … and even then never loopback or link-local.
	p.AllowPublic = true
	for _, host := range []string{"127.0.0.1", "169.254.1.1", "::1"} {
		if _, err := p.dial(context.Background(), host, 554); !errors.Is(err, ErrTargetNotAllowed) {
			t.Errorf("AllowPublic dial %s: %v", host, err)
		}
	}
	for addr, want := range map[string]bool{
		"192.168.1.5": true, "10.0.0.9": true, "100.100.2.3": true, "fd00::1": true,
		"127.0.0.1": false, "8.8.8.8": false, "169.254.0.5": false, "224.0.0.1": false, "0.0.0.0": false, "::1": false,
	} {
		if got := HomeAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("HomeAddr(%s) = %v", addr, got)
		}
	}
}

func TestParseSDP(t *testing.T) {
	info := parseSDP("m=video 0 RTP/AVP 96\na=rtpmap:96 H264/90000\na=framesize:96 1920-1080\nm=audio 0 RTP/AVP 0\na=rtpmap:0 PCMU/8000\n")
	if info.Codec != "H264" || info.Audio != "PCMU" || info.Width != 1920 || info.Height != 1080 {
		t.Errorf("%+v", info)
	}
	if info := parseSDP("m=video 0 RTP/AVP 96\na=rtpmap:96 HEVC/90000\na=x-dimensions:99999999,-4\n"); info.Codec != "H265" || info.Width != 0 || info.Height != 0 {
		t.Errorf("%+v", info)
	}
}

func recorded(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseProbeMatches(t *testing.T) {
	source := netip.MustParseAddr("192.168.1.64")
	found := parseProbeMatches(recorded(t, "probematch-hikvision.xml"), source)
	want := Found{Address: "192.168.1.64", ONVIFPort: 80, Name: "HIKVISION DS-2CD2143G2-I", Hardware: "DS-2CD2143G2-I", ID: "8a4e0c00-7c3d-11b2-8080-bcbac2f1a7e5"}
	if len(found) != 1 || found[0] != want {
		t.Errorf("hikvision: %+v", found)
	}
	// The port comes from the address that matches the sender; what the
	// device claims as its address is never used.
	found = parseProbeMatches(recorded(t, "probematch-reolink.xml"), netip.MustParseAddr("192.168.1.70"))
	if len(found) != 1 || found[0].Address != "192.168.1.70" || found[0].ONVIFPort != 8000 || found[0].Name != "Reolink RLC-810A" {
		t.Errorf("reolink: %+v", found)
	}
	// A printer answers WS-Discovery too.
	if found := parseProbeMatches(recorded(t, "probematch-printer.xml"), source); len(found) != 0 {
		t.Errorf("printer: %+v", found)
	}
	if found := parseProbeMatches([]byte("not xml <"), source); len(found) != 0 {
		t.Errorf("garbage: %+v", found)
	}
}

func TestDiscoverOn(t *testing.T) {
	// A "camera" on loopback: answers every probe with a recorded match.
	camera, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer camera.Close()
	answer := recorded(t, "probematch-reolink.xml")
	probes := make(chan string, 4)
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, from, err := camera.ReadFromUDP(buf)
			if err != nil {
				return
			}
			probes <- string(buf[:n])
			_, _ = camera.WriteToUDP(answer, from)
		}
	}()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	allowAll := func(netip.Addr) bool { return true }
	start := time.Now()
	found, err := discoverOn(context.Background(), []*net.UDPConn{conn}, camera.LocalAddr().(*net.UDPAddr), 400*time.Millisecond, allowAll)
	if err != nil || len(found) != 1 || found[0].Address != "127.0.0.1" || found[0].ONVIFPort != 8000 {
		t.Fatalf("%+v %v", found, err)
	}
	if took := time.Since(start); took < 300*time.Millisecond || took > 3*time.Second {
		t.Errorf("the search took %v", took)
	}
	probe := <-probes
	if !strings.Contains(probe, "NetworkVideoTransmitter") || !strings.Contains(probe, "discovery/Probe") {
		t.Errorf("probe = %s", probe)
	}
	// The probe carries no login.
	if strings.Contains(strings.ToLower(probe), "password") {
		t.Error("the search must not send a password")
	}
	// Answers from outside the home network are dropped.
	conn, _ = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	found, _ = discoverOn(context.Background(), []*net.UDPConn{conn}, camera.LocalAddr().(*net.UDPAddr), 300*time.Millisecond, HomeAddr)
	if len(found) != 0 {
		t.Errorf("answer from loopback kept: %+v", found)
	}
	// A cancelled request ends the search early.
	conn, _ = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	_, _ = discoverOn(ctx, []*net.UDPConn{conn}, camera.LocalAddr().(*net.UDPAddr), 10*time.Second, allowAll)
	if time.Since(start) > 2*time.Second {
		t.Error("the search did not end with its request")
	}
}

// onvifCamera answers the SOAP calls of ProbeONVIF with recorded answers.
func onvifCamera(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		authorized := strings.Contains(text, "<wsse:Username>admin</wsse:Username>") && strings.Contains(text, "PasswordDigest")
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(text, "GetSystemDateAndTime"):
			calls = append(calls, "time")
			_, _ = w.Write(recorded(t, "onvif-datetime.xml"))
		case !authorized:
			calls = append(calls, "refused")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(recorded(t, "onvif-notauthorized.xml"))
		case strings.Contains(text, "GetCapabilities"):
			calls = append(calls, "capabilities")
			_, _ = w.Write(recorded(t, "onvif-capabilities.xml"))
		case strings.Contains(text, "GetDeviceInformation"):
			calls = append(calls, "device")
			_, _ = w.Write(recorded(t, "onvif-deviceinfo.xml"))
		case strings.Contains(text, "GetProfiles"):
			calls = append(calls, "profiles "+r.URL.Path)
			_, _ = w.Write(recorded(t, "onvif-profiles.xml"))
		case strings.Contains(text, "GetStreamUri"):
			token := "sub"
			if strings.Contains(text, "<ProfileToken>Profile_1</ProfileToken>") {
				token = "main"
			}
			calls = append(calls, "uri "+token)
			_, _ = w.Write([]byte(strings.ReplaceAll(string(recorded(t, "onvif-streamuri.xml")), "STREAM", token)))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestProbeONVIF(t *testing.T) {
	srv, calls := onvifCamera(t)
	u, _ := url.Parse(srv.URL)
	port := atoi(u.Port())
	p := loopbackProber(t)
	info, err := p.ProbeONVIF(context.Background(), "127.0.0.1", port, "admin", "geheim")
	if err != nil || info.Outcome != OutcomeOK {
		t.Fatalf("%+v %v", info, err)
	}
	if info.Manufacturer != "HIKVISION" || info.Model != "DS-2CD2143G2-I" || info.Firmware != "V5.7.3" || !info.PTZ {
		t.Errorf("%+v", info)
	}
	if len(info.Profiles) != 2 {
		t.Fatalf("profiles: %+v", info.Profiles)
	}
	main := info.Profiles[0]
	// The camera named 10.0.0.99 (its inner address) with a login in the
	// address: the stream is on the host that was asked, without login.
	if main.Token != "Profile_1" || main.Width != 2688 || main.Height != 1520 || main.Codec != "H265" || !main.PTZ || main.URL != "rtsp://127.0.0.1:554/Streaming/Channels/main?transportmode=unicast" {
		t.Errorf("main: %+v", main)
	}
	if mainURL, subURL := PickStreams(info.Profiles); mainURL != main.URL || subURL != "rtsp://127.0.0.1:554/Streaming/Channels/sub?transportmode=unicast" {
		t.Errorf("PickStreams: %q %q", mainURL, subURL)
	}
	// The media service is asked on the camera's path but our address.
	if !strings.Contains(strings.Join(*calls, ","), "profiles /onvif/Media") {
		t.Errorf("calls: %v", *calls)
	}
	// The clock of the recorded camera is from 2024.
	if info.ClockOffsetSeconds > -3600 {
		t.Errorf("clock offset = %v", info.ClockOffsetSeconds)
	}
	// A wrong user: one refused login, then nothing more is asked.
	*calls = nil
	info, err = p.ProbeONVIF(context.Background(), "127.0.0.1", port, "guest", "x")
	if err != nil || info.Outcome != OutcomeUnauthorized || strings.Join(*calls, ",") != "time,refused" {
		t.Errorf("%+v %v %v", info, err, *calls)
	}
	// Nothing listens; the rule of the home network holds here too.
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if info, err := p.ProbeONVIF(context.Background(), "127.0.0.1", dead, "admin", "geheim"); err != nil || info.Outcome != OutcomeUnreachable {
		t.Errorf("dead: %+v %v", info, err)
	}
	if _, err := (&Prober{}).ProbeONVIF(context.Background(), "203.0.113.5", 80, "admin", "geheim"); !errors.Is(err, ErrTargetNotAllowed) {
		t.Errorf("public: %v", err)
	}
}

func TestStreamOnHost(t *testing.T) {
	for raw, want := range map[string]string{
		"rtsp://admin:pw@10.0.0.99:8554/live#frag": "rtsp://192.168.1.5:8554/live",
		"rtsp://10.0.0.99/live":                    "rtsp://192.168.1.5/live",
		"http://10.0.0.99/live":                    "",
		"rtsp://10.0.0.99/{x}":                     "rtsp://192.168.1.5/%7Bx%7D",
	} {
		if got := streamOnHost(raw, "192.168.1.5"); got != want {
			t.Errorf("streamOnHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPickStreams(t *testing.T) {
	if m, s := PickStreams([]Profile{{URL: "a", Width: 1920, Height: 1080}}); m != "a" || s != "" {
		t.Errorf("%q %q", m, s)
	}
	if m, s := PickStreams([]Profile{{URL: "small", Width: 640, Height: 360}, {URL: "big", Width: 3840, Height: 2160}, {URL: "mid", Width: 1280, Height: 720}}); m != "big" || s != "small" {
		t.Errorf("%q %q", m, s)
	}
	if m, s := PickStreams(nil); m != "" || s != "" {
		t.Errorf("%q %q", m, s)
	}
}
