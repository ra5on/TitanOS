package appproxy

// Cookies of apps on their ports (PLAN 62). Browsers keep cookies apart by
// host, not by port: every app on https://192.168.10.5:200xx and the web
// UI on https://192.168.10.5 share one cookie jar. So that one app does
// not read or overwrite the cookies of another, titand renames the cookies
// an app sets on its port: "sid" becomes "tp20001_sid", "__Host-sid"
// becomes "__Host-tp20001_sid". On the way in, only the cookies of the
// request's own port reach the app, under their original names.
//
// Limit: cookies an app's JavaScript sets or reads through document.cookie
// are not rewritten. Such a cookie set on the port never reaches the app,
// and a cookie the server set is seen by the script under its prefixed
// name. Apps that need this work on their names (immich.nas.local).

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// portKey marks a request that came in on an app port.
type portKey struct{}

// withPort marks ctx as a request on an app port.
func withPort(ctx context.Context, port int) context.Context {
	return context.WithValue(ctx, portKey{}, port)
}

// portOf is the app port a request came in on, or 0 (an app's name).
func portOf(ctx context.Context) int {
	port, _ := ctx.Value(portKey{}).(int)
	return port
}

// portCookieTag is the part a port puts in front of the names of its
// app's cookies.
func portCookieTag(port int) string {
	return "tp" + strconv.Itoa(port) + "_"
}

// cookieNamePrefixes must stay at the very start of a cookie's name: the
// browser checks them there.
var cookieNamePrefixes = []string{"__Host-", "__Secure-"}

// splitNamePrefix splits a cookie name into __Host-/__Secure- (in its own
// spelling; browsers compare without case) and the rest.
func splitNamePrefix(name string) (prefix, rest string) {
	for _, p := range cookieNamePrefixes {
		if len(name) >= len(p) && strings.EqualFold(name[:len(p)], p) {
			return name[:len(p)], name[len(p):]
		}
	}
	return "", name
}

// prefixPortSetCookies renames every cookie an app sets on its port to
// the port's name; the attributes stay as they are. A cookie without a
// name cannot be kept apart and is dropped.
func prefixPortSetCookies(h http.Header, port int) {
	lines := h.Values("Set-Cookie")
	if len(lines) == 0 {
		return
	}
	h.Del("Set-Cookie")
	tag := portCookieTag(port)
	for _, line := range lines {
		name, rest, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		lead := name[:len(name)-len(strings.TrimLeft(name, " \t"))]
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		prefix, base := splitNamePrefix(name)
		h.Add("Set-Cookie", lead+prefix+tag+base+"="+rest)
	}
}

// unprefixPortCookies keeps only the cookies of a port's app in a
// request and gives them their original names; cookies of other ports,
// of the app's names and of the web UI are dropped.
func unprefixPortCookies(h http.Header, port int) {
	lines := h.Values("Cookie")
	if len(lines) == 0 {
		return
	}
	h.Del("Cookie")
	tag := portCookieTag(port)
	var kept []string
	for _, line := range lines {
		for part := range strings.SplitSeq(line, ";") {
			part = strings.TrimSpace(part)
			name, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			prefix, base := splitNamePrefix(strings.TrimSpace(name))
			base, ours := strings.CutPrefix(base, tag)
			if !ours || base == "" {
				continue
			}
			kept = append(kept, prefix+base+"="+value)
		}
	}
	if len(kept) > 0 {
		h.Set("Cookie", strings.Join(kept, "; "))
	}
}
