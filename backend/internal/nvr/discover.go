package nvr

import (
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Found is a camera that answered the search.
type Found struct {
	// Address is where the answer came from.
	Address string `json:"address"`
	// ONVIFPort is the port of its ONVIF service.
	ONVIFPort int `json:"onvifPort"`
	// Name and Hardware are what the camera says of itself (unverified
	// text of a device in the network; "" if it says nothing).
	Name     string `json:"name"`
	Hardware string `json:"hardware"`
	// ID is the camera's own identifier (a UUID); it tells cameras with
	// several addresses apart.
	ID string `json:"id"`
}

// DiscoverTime is how long the search listens for answers.
const DiscoverTime = 3 * time.Second

var wsDiscovery = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}

// probeMessage is the WS-Discovery Probe for ONVIF devices.
func probeMessage() []byte {
	id := make([]byte, 16)
	_, _ = rand.Read(id) // crypto/rand does not fail on Linux
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
	return []byte(`<?xml version="1.0" encoding="utf-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header><w:MessageID>uuid:` + uuid + `</w:MessageID><w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>` +
		`<w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>` +
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`)
}

// Discover searches the home network for ONVIF cameras: one WS-Discovery
// probe (UDP multicast) on every interface of the NAS with a home network
// address, then DiscoverTime of listening. It runs only when an
// administrator asks, never by itself, and sends no password. Answers
// from outside the home network are dropped.
func Discover(ctx context.Context) ([]Found, error) {
	var conns []*net.UDPConn
	for _, address := range lanAddresses() {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(address)})
		if err != nil {
			continue
		}
		conns = append(conns, conn)
	}
	if len(conns) == 0 {
		return []Found{}, nil
	}
	return discoverOn(ctx, conns, wsDiscovery, DiscoverTime, HomeAddr)
}

// discoverOn sends the probe from every connection to target and
// collects the answers for wait.
func discoverOn(ctx context.Context, conns []*net.UDPConn, target *net.UDPAddr, wait time.Duration, allow func(netip.Addr) bool) ([]Found, error) {
	deadline := time.Now().Add(wait)
	stop := context.AfterFunc(ctx, func() {
		for _, conn := range conns {
			_ = conn.SetReadDeadline(time.Unix(1, 0)) // ends the read; a closed connection is no error here
		}
	})
	defer stop()
	message := probeMessage()
	answers := make(chan Found, 64)
	done := make(chan struct{})
	for _, conn := range conns {
		go func() {
			defer func() { done <- struct{}{} }()
			defer func() { _ = conn.Close() }() // a connection that is closed already is no error here
			_ = conn.SetDeadline(deadline)      // a connection that cannot take a deadline is closed by the caller's context
			// Twice: UDP may lose one.
			for range 2 {
				if _, err := conn.WriteToUDP(message, target); err != nil {
					return
				}
			}
			buf := make([]byte, 64<<10)
			for {
				n, from, err := conn.ReadFromUDPAddrPort(buf)
				if err != nil {
					return
				}
				source := from.Addr().Unmap()
				if !allow(source) {
					continue
				}
				for _, found := range parseProbeMatches(buf[:n], source) {
					select {
					case answers <- found:
					default: // more answers than any home has cameras
					}
				}
			}
		}()
	}
	for range conns {
		<-done
	}
	close(answers)
	out := []Found{}
	for found := range answers {
		// A camera answers every probe, and on every interface it hears.
		if !slices.ContainsFunc(out, func(f Found) bool {
			return f.Address == found.Address || (f.ID != "" && f.ID == found.ID)
		}) {
			out = append(out, found)
		}
	}
	slices.SortFunc(out, func(a, b Found) int {
		x, _ := netip.ParseAddr(a.Address) // addresses made by netip above
		y, _ := netip.ParseAddr(b.Address)
		return x.Compare(y)
	})
	return out, ctx.Err()
}

// parseProbeMatches reads the cameras out of one answer. The address is
// the one the answer came from: what the device writes into its answer
// only gives the port and the texts.
func parseProbeMatches(data []byte, source netip.Addr) []Found {
	var envelope struct {
		Matches []struct {
			Endpoint string `xml:"EndpointReference>Address"`
			Types    string `xml:"Types"`
			Scopes   string `xml:"Scopes"`
			XAddrs   string `xml:"XAddrs"`
		} `xml:"Body>ProbeMatches>ProbeMatch"`
	}
	if xml.Unmarshal(data, &envelope) != nil {
		return nil
	}
	var out []Found
	for _, match := range envelope.Matches {
		if !strings.Contains(match.Types, "NetworkVideoTransmitter") && !strings.Contains(match.Scopes, "onvif://") {
			continue
		}
		found := Found{Address: source.String(), ONVIFPort: 80, ID: clip(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(match.Endpoint), "urn:"), "uuid:"), 64)}
		// The service address on the source address, else the first.
		ports := []int{}
		for _, raw := range strings.Fields(match.XAddrs) {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "http" {
				continue
			}
			port := 80
			if p := u.Port(); p != "" {
				port = atoi(p)
			}
			if !validPort(port) {
				continue
			}
			if addr, err := netip.ParseAddr(u.Hostname()); err == nil && addr.Unmap() == source {
				ports = append([]int{port}, ports...)
			} else {
				ports = append(ports, port)
			}
		}
		if len(ports) > 0 {
			found.ONVIFPort = ports[0]
		}
		for _, scope := range strings.Fields(match.Scopes) {
			if value, ok := strings.CutPrefix(scope, "onvif://www.onvif.org/name/"); ok && found.Name == "" {
				found.Name = scopeText(value)
			}
			if value, ok := strings.CutPrefix(scope, "onvif://www.onvif.org/hardware/"); ok && found.Hardware == "" {
				found.Hardware = scopeText(value)
			}
		}
		out = append(out, found)
	}
	return out
}

// scopeText is the text of a scope: URL-decoded, without control
// characters, bounded.
func scopeText(value string) string {
	if decoded, err := url.PathUnescape(value); err == nil {
		value = decoded
	}
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	return clip(strings.TrimSpace(value), 80)
}
