package catalog

import (
	"net/url"
	"strings"
)

// Repository spelling and the official catalog's main/HEAD archives identify
// the same source. Other repositories' branches, tags and files stay distinct.
func sourceIdentity(address string) string {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return address
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.EqualFold(u.Host, "github.com") && u.RawQuery == "" {
		path = strings.ToLower(path)
		if strings.HasPrefix(path, "/ra5on/titanos-apps/") {
			for _, suffix := range []string{"/archive/refs/heads/main.zip", "/archive/refs/heads/main.tar.gz", "/archive/main.zip", "/archive/main.tar.gz", "/archive/head.zip", "/archive/head.tar.gz"} {
				path = strings.TrimSuffix(path, suffix)
			}
		}
		path = strings.TrimSuffix(path, ".git")
		if len(strings.Split(strings.Trim(path, "/"), "/")) == 2 {
			return "github.com" + path
		}
	}
	u.Path = path
	u.Fragment = ""
	return u.String()
}
func sameSource(a, b string) bool { return sourceIdentity(a) == sourceIdentity(b) }
