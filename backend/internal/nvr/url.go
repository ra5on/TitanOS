package nvr

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// StreamURL builds an RTSP address. User and password are percent-encoded,
// so that a password with @, :, / or # does not break the address (a classic
// reason why "the password is right but the camera says no").
func StreamURL(scheme, host string, port int, path, user, password string) string {
	if scheme == "" {
		scheme = "rtsp"
	}
	u := url.URL{Scheme: scheme, Host: hostPort(host, port)}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	if path != "" {
		if i := strings.IndexByte(path, '?'); i >= 0 {
			u.Path, u.RawQuery = path[:i], path[i+1:]
		} else {
			u.Path = path
		}
		if !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
	}
	return u.String()
}

func hostPort(host string, port int) string {
	if port <= 0 {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Redact hides the password of an address for logs, errors and the
// interface. Text that is no address is returned unchanged.
func Redact(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.User == nil {
		return address
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "REDACTED")
	}
	return u.String()
}
