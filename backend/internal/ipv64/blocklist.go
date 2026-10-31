package ipv64

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// The v64 blocklist of IPv64 (Honeypot64): GET /honeypot/list-token gives
// a personal token and the base address of the list files ("Abruf dann über
// base + Dateiname (z. B. ipv64_blocklist_v64.txt)"). Without a live
// permission the address delivers the daily state. TitanOS fetches the file
// at most once a day, keeps it in RAM only and hands it to the firewall,
// which drops these senders in the internet zone while it is on.

const (
	// BlocklistFile is the main list named in the description.
	BlocklistFile = "ipv64_blocklist_v64.txt"
	// MaxBlocklist bounds the entries taken; MaxBlocklistBytes the file.
	MaxBlocklist      = 200_000
	MaxBlocklistBytes = 32 << 20
	// BlocklistEvery is how often the list is fetched.
	BlocklistEvery = 24 * time.Hour
)

// BlocklistStatus tells about the last fetch.
type BlocklistStatus struct {
	Entries   int        `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
	Live      bool       `json:"live"`
	Fetched   *time.Time `json:"fetched,omitempty"`
	Next      *time.Time `json:"next,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type listState struct {
	fetched   time.Time
	tried     time.Time
	entries   int
	truncated bool
	live      bool
	err       string
	client    *http.Client
}

func (m *Manager) listStatus() BlocklistStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := BlocklistStatus{Entries: m.list.entries, Truncated: m.list.truncated, Live: m.list.live, Error: m.list.err}
	if !m.list.fetched.IsZero() {
		at := m.list.fetched
		st.Fetched = &at
	}
	if m.current.Blocklist.Enabled {
		next := m.nextFetch()
		st.Next = &next
	}
	return st
}

// nextFetch is a day after the last fetch, an hour after a failed try.
func (m *Manager) nextFetch() time.Time {
	next := m.list.fetched.Add(BlocklistEvery)
	if !m.list.tried.IsZero() && m.list.tried.After(m.list.fetched) {
		next = m.list.tried.Add(time.Hour)
	}
	return next
}

func (m *Manager) dropList(ctx context.Context) {
	m.mu.Lock()
	m.list = listState{client: m.list.client}
	m.mu.Unlock()
	if m.Firewall != nil {
		_ = m.Firewall.SetBlocklist(ctx, nil) // cleanup, best effort: nothing more can be done if it fails
	}
}

// RefreshBlocklist fetches the list now (button on the page): when none
// was fetched within a day or the last try failed.
func (m *Manager) RefreshBlocklist(ctx context.Context) error {
	m.mu.Lock()
	if !m.current.Blocklist.Enabled {
		m.mu.Unlock()
		return errors.New("the blocklist is switched off")
	}
	// At most once a day; after a failure again a minute later.
	if m.now().Sub(m.list.tried) < time.Minute || (!m.list.fetched.IsZero() && m.list.err == "" && m.now().Sub(m.list.fetched) < BlocklistEvery) {
		m.mu.Unlock()
		return errors.New("the list was fetched within the last day")
	}
	m.mu.Unlock()
	return m.fetchList(ctx)
}

func (m *Manager) refreshList(ctx context.Context) {
	m.mu.Lock()
	due := m.loaded && m.current.Blocklist.Enabled && !m.now().Before(m.nextFetch())
	m.mu.Unlock()
	if due {
		if err := m.fetchList(ctx); err != nil {
			slog.Warn("IPv64 blocklist not fetched", "err", err)
		}
	}
}

func (m *Manager) fetchList(ctx context.Context) error {
	m.mu.Lock()
	m.list.tried = m.now()
	httpClient := m.list.client
	m.mu.Unlock()
	fail := func(err error) error {
		m.mu.Lock()
		m.list.err = err.Error()
		m.mu.Unlock()
		return err
	}
	client, err := m.Client()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	token, err := client.ListToken(ctx)
	if err != nil {
		return fail(err)
	}
	address, err := listAddress(token.Base, m.Options.BaseURL)
	if err != nil {
		return fail(err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if m.Options.Client != nil {
			httpClient = m.Options.Client
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fail(errors.New("blocklist address not usable"))
	}
	req.Header.Set("User-Agent", "TitanOS")
	res, err := httpClient.Do(req)
	if err != nil {
		// The address carries the token: never in a message.
		return fail(errors.New("the blocklist could not be fetched"))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("the blocklist answered HTTP %d", res.StatusCode))
	}
	list, truncated, err := ParseBlocklist(io.LimitReader(res.Body, MaxBlocklistBytes))
	if err != nil {
		return fail(err)
	}
	if m.Firewall != nil {
		if err := m.Firewall.SetBlocklist(ctx, list); err != nil {
			return fail(err)
		}
	}
	m.mu.Lock()
	m.list.fetched, m.list.entries, m.list.truncated, m.list.err = m.now(), len(list), truncated, ""
	m.list.live = token.Live && res.Header.Get("X-IPv64-List") != "daily"
	m.mu.Unlock()
	slog.Info("IPv64 blocklist loaded", "entries", len(list), "truncated", truncated)
	return nil
}

// listAddress joins base and file name; the base must be HTTPS at
// ipv64.net (or at the test server).
func listAddress(base, testBase string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.User != nil {
		return "", errors.New("the blocklist address is not usable")
	}
	host := strings.ToLower(u.Hostname())
	ok := u.Scheme == "https" && (host == "ipv64.net" || strings.HasSuffix(host, ".ipv64.net"))
	if testBase != "" {
		if t, err := url.Parse(testBase); err == nil && t.Host == u.Host {
			ok = true
		}
	}
	if !ok {
		return "", errors.New("the blocklist address is not at ipv64.net")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + BlocklistFile, nil
}

// ParseBlocklist reads one address or network per line; comments (# ;)
// and empty lines are skipped. Private, loopback, link-local and other
// special addresses are never taken: they would hit the home network.
func ParseBlocklist(r io.Reader) ([]netip.Prefix, bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 4096)
	var out []netip.Prefix
	seen := map[netip.Prefix]bool{}
	truncated := false
	lines := 0
	for scanner.Scan() {
		lines++
		line := strings.TrimSpace(scanner.Text())
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		field := strings.Fields(line)[0]
		prefix, ok := parseEntry(field)
		if !ok || seen[prefix] {
			continue
		}
		if len(out) >= MaxBlocklist {
			truncated = true
			break
		}
		seen[prefix] = true
		out = append(out, prefix)
	}
	if err := scanner.Err(); err != nil {
		return nil, false, errors.New("the blocklist could not be read")
	}
	if lines > 0 && len(out) == 0 {
		return nil, false, errors.New("the blocklist holds no addresses")
	}
	return out, truncated, nil
}

func parseEntry(text string) (netip.Prefix, bool) {
	var p netip.Prefix
	if strings.Contains(text, "/") {
		parsed, err := netip.ParsePrefix(text)
		if err != nil {
			return p, false
		}
		p = parsed.Masked()
	} else {
		addr, err := netip.ParseAddr(text)
		if err != nil {
			return p, false
		}
		addr = addr.Unmap()
		p = netip.PrefixFrom(addr, addr.BitLen())
	}
	a := p.Addr()
	if a.Is4In6() || a.Zone() != "" || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() {
		return p, false
	}
	// Too wide a network would shut out a large part of the internet.
	if (a.Is4() && p.Bits() < 16) || (a.Is6() && p.Bits() < 32) {
		return p, false
	}
	// 100.64.0.0/10 is the address range of Tailscale and NetBird.
	if netip.MustParsePrefix("100.64.0.0/10").Overlaps(p) {
		return p, false
	}
	return p, true
}
