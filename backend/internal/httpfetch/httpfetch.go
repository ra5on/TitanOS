// Package httpfetch is the HTTP(S) download core shared by VM media imports
// and the file manager's "download from link": a client that checks every
// address it connects to and every redirect, and a resumable GET that
// continues a partial file with Range/If-Range and refuses a source that
// changed in between. Callers own the target file, its location on a volume
// and the bookkeeping; nothing here buffers more than the copy buffer.
package httpfetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"
)

var (
	// ErrBlocked means an address or redirect target is not allowed.
	ErrBlocked = errors.New("httpfetch: address not allowed")
	// ErrRedirect means a redirect was refused (count, scheme or downgrade).
	ErrRedirect = errors.New("httpfetch: redirect not allowed")
	// ErrRange means the server answered a resume with an unusable range.
	ErrRange = errors.New("invalid HTTP resume range")
	// ErrChanged means the source is no longer the one a resume started from.
	ErrChanged = errors.New("source changed during resume")
)

// StatusError is an HTTP answer other than 200 or 206.
type StatusError struct{ Code int }

func (e StatusError) Error() string { return fmt.Sprintf("download returned HTTP %d", e.Code) }

// Policy says where a client may connect.
type Policy struct {
	// Allow decides about each address right before connecting, after name
	// resolution and for every redirect hop. Nil allows any address.
	Allow func(netip.AddrPort) bool
	// MaxRedirects caps redirects (default 10).
	MaxRedirects int
	// TLS replaces the TLS settings; tests trust their own servers with it.
	TLS *tls.Config
}

// Redirects follows at most max redirects, only to http(s) without user
// info, and never from https back to plain http anywhere in the chain.
func Redirects(max int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= max {
			return fmt.Errorf("%w: too many redirects", ErrRedirect)
		}
		if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return fmt.Errorf("%w: %s", ErrRedirect, req.URL.Scheme)
		}
		if req.URL.Scheme != "https" {
			for _, previous := range via {
				if previous.URL.Scheme == "https" {
					return fmt.Errorf("%w: https to http", ErrRedirect)
				}
			}
		}
		return nil
	}
}

// NewClient returns a client without proxy whose dialer checks the actual
// address of every connection. Checking at connect time also covers DNS
// answers that change between a check and the connection.
func NewClient(p Policy) *http.Client {
	if p.MaxRedirects <= 0 {
		p.MaxRedirects = 10
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	if p.Allow != nil {
		allow := p.Allow
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			addr, err := netip.ParseAddrPort(address)
			if err != nil || !allow(netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())) {
				return ErrBlocked
			}
			return nil
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = dialer.DialContext
	tr.ResponseHeaderTimeout = 30 * time.Second
	tr.TLSHandshakeTimeout = 15 * time.Second
	tr.MaxIdleConnsPerHost = 2
	if p.TLS != nil {
		tr.TLSClientConfig = p.TLS
	}
	return &http.Client{Transport: tr, CheckRedirect: Redirects(p.MaxRedirects)}
}

// ValidURL checks a link a person entered: http or https with a host, no
// user info, at most 8 KiB.
func ValidURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if len(raw) > 8192 || err != nil || u.Host == "" || u.User != nil || !(u.Scheme == "http" || u.Scheme == "https") || u.Opaque != "" {
		return nil, false
	}
	return u, true
}

// Resume is what a partial file already holds and how its source looked.
type Resume struct {
	Offset             int64
	ETag, LastModified string
}

// Response is an answered GET. Body starts at Offset.
type Response struct {
	*http.Response
	// Offset is where Body starts: the resume offset for 206, 0 for 200.
	Offset int64
	// Total is the size of the whole resource, -1 if unknown.
	Total              int64
	ETag, LastModified string
}

// Filename returns the file name the server suggests (Content-Disposition)
// or else the last element of the final URL path; "" if neither has one.
func (r *Response) Filename() string {
	if _, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition")); err == nil {
		if name := path.Base(strings.ReplaceAll(params["filename"], "\\", "/")); name != "" && name != "." && name != "/" {
			return name
		}
	}
	return URLFilename(r.Request.URL)
}

// URLFilename is the decoded last path element of a URL, "" if there is none.
func URLFilename(u *url.URL) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" {
		return ""
	}
	return name
}

// Open sends the GET, with Range and If-Range when a partial file can be
// continued, and checks the answer. A resume needs a strong ETag or a
// Last-Modified date; without one it starts over. The caller closes Body.
func Open(ctx context.Context, client *http.Client, rawURL string, header http.Header, resume Resume) (*Response, error) {
	offset := resume.Offset
	validator := resume.ETag
	if strings.HasPrefix(validator, "W/") {
		validator = ""
	}
	if validator == "" {
		validator = resume.LastModified
	}
	if offset > 0 && validator == "" {
		offset = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", validator)
	}
	res, err := client.Do(req)
	if err != nil {
		if u, ok := err.(*url.Error); ok {
			return nil, fmt.Errorf("download failed: %w", u.Err)
		}
		return nil, fmt.Errorf("download failed")
	}
	out := &Response{Response: res, Total: res.ContentLength, ETag: res.Header.Get("ETag"), LastModified: res.Header.Get("Last-Modified")}
	switch res.StatusCode {
	case http.StatusOK:
		out.Offset = 0
	case http.StatusPartialContent:
		var start, end, total int64
		count, err := fmt.Sscanf(res.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total)
		if offset == 0 || err != nil || count != 3 || start != offset || end < start || total <= end || (res.ContentLength >= 0 && res.ContentLength != end-start+1) {
			res.Body.Close()
			return nil, ErrRange
		}
		if (resume.ETag != "" && out.ETag != resume.ETag) || (resume.ETag == "" && resume.LastModified != "" && out.LastModified != resume.LastModified) {
			res.Body.Close()
			return nil, ErrChanged
		}
		out.Offset, out.Total = offset, total
	default:
		res.Body.Close()
		return nil, StatusError{res.StatusCode}
	}
	return out, nil
}
