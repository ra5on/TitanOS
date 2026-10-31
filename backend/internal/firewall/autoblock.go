package firewall

import (
	"encoding/json"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	// maxBlocks bounds the list (and the nft set) an attacker can grow.
	maxBlocks = 4096
	// maxTracked bounds the senders with failed logins being counted.
	maxTracked = 10000
	// blockedFlush is how often permanent blocks are written to the stick
	// at most.
	blockedFlush = time.Minute
)

// Sources of failed logins.
const (
	SourceWeb = "web"
	SourceSSH = "ssh"
	SourceSMB = "smb"
)

// Block is an address (IPv6: its /64) locked out after failed logins.
type Block struct {
	Address string    `json:"address"`
	Since   time.Time `json:"since"`
	// Until is zero for a permanent block.
	Until    time.Time `json:"until,omitzero"`
	Source   string    `json:"source"`
	Attempts int       `json:"attempts"`
}

func (b Block) prefix() (netip.Prefix, bool) {
	p, err := parseSource(b.Address)
	return p, err == nil
}

// blockKey is what failed logins are counted for: the address, for IPv6
// the /64 (one device or home network gets a whole /64 and can pick any
// address in it).
func blockKey(addr netip.Addr) netip.Prefix {
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32)
	}
	return netip.PrefixFrom(addr, 64).Masked()
}

// exempt tells whether an address must never be blocked.
func exempt(a AutoBlock, addr netip.Addr, home []netip.Prefix) bool {
	addr = addr.Unmap().WithZone("")
	if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() {
		return true
	}
	if a.ExemptLocal && classify(addr, home) != Internet {
		return true
	}
	for _, entry := range a.Exempt {
		if p, err := parseSource(entry); err == nil && p.Contains(addr) {
			return true
		}
	}
	return false
}

// failed records a failed login and returns the new block, if the sender
// is now blocked. The caller holds mu.
func (m *Manager) countFailure(addr netip.Addr, source string, now time.Time) (Block, bool) {
	cfg := m.active.AutoBlock
	if !cfg.Enabled || exempt(cfg, addr, m.home) {
		return Block{}, false
	}
	key := blockKey(addr)
	if m.blockedLocked(key.Addr()) {
		return Block{}, false
	}
	window := time.Duration(cfg.Within) * time.Minute
	times := m.attempts[key]
	times = slices.DeleteFunc(times, func(t time.Time) bool { return now.Sub(t) >= window })
	times = append(times, now)
	if len(times) < cfg.Attempts {
		if _, known := m.attempts[key]; !known && len(m.attempts) >= maxTracked {
			m.forgetOldest(now, window)
		}
		m.attempts[key] = times
		return Block{}, false
	}
	delete(m.attempts, key)
	block := Block{Address: prefixText(key), Since: now, Source: source, Attempts: len(times)}
	if !cfg.Permanent {
		block.Until = now.Add(time.Duration(cfg.Minutes) * time.Minute)
	}
	if len(m.blocks) >= maxBlocks {
		// Make room by the temporary block that ends first; permanent
		// blocks are only ever lifted by hand.
		oldest := -1
		for i, b := range m.blocks {
			if !b.Until.IsZero() && (oldest < 0 || b.Until.Before(m.blocks[oldest].Until)) {
				oldest = i
			}
		}
		if oldest < 0 {
			slog.Warn("firewall: block list is full, not blocking", "address", block.Address)
			return Block{}, false
		}
		m.blocks = slices.Delete(m.blocks, oldest, oldest+1)
	}
	m.blocks = append(m.blocks, block)
	if block.Until.IsZero() {
		m.blocksDirty = true
	}
	return block, true
}

// forgetOldest drops expired counts, or the one counted longest ago.
func (m *Manager) forgetOldest(now time.Time, window time.Duration) {
	var oldest netip.Prefix
	var oldestAt time.Time
	for key, times := range m.attempts {
		last := times[len(times)-1]
		if now.Sub(last) >= window {
			delete(m.attempts, key)
			continue
		}
		if oldestAt.IsZero() || last.Before(oldestAt) {
			oldest, oldestAt = key, last
		}
	}
	if len(m.attempts) >= maxTracked {
		delete(m.attempts, oldest)
	}
}

func (m *Manager) blockedLocked(addr netip.Addr) bool {
	for _, b := range m.blocks {
		if p, ok := b.prefix(); ok && p.Contains(addr) {
			return true
		}
	}
	return false
}

// expire removes temporary blocks that have run out; true if any did.
func (m *Manager) expire(now time.Time) bool {
	before := len(m.blocks)
	m.blocks = slices.DeleteFunc(m.blocks, func(b Block) bool { return !b.Until.IsZero() && !now.Before(b.Until) })
	return len(m.blocks) != before
}

// blockedPrefixes are the blocks as they go into the rules: exempt
// networks never, even if they were blocked before being exempted.
func (m *Manager) blockedPrefixes() []netip.Prefix {
	out := []netip.Prefix{}
	for _, b := range m.blocks {
		p, ok := b.prefix()
		if ok && !exempt(m.active.AutoBlock, p.Addr(), m.home) {
			out = append(out, p)
		}
	}
	return out
}

// loadBlocks reads the permanent blocks from the stick and what the last
// titand left in /run. Called once at the start.
func (m *Manager) loadBlocks(now time.Time) {
	var permanent []Block
	if err := m.store.ReadJSON(blockedFile, &permanent); err != nil && !os.IsNotExist(err) {
		slog.Warn("firewall: permanent blocks not readable", "err", err)
	}
	var running []Block
	if m.runFile != "" {
		if data, err := os.ReadFile(m.runFile); err == nil {
			_ = json.Unmarshal(data, &running) // a value that cannot be read leaves the zero value, which the code handles
		}
	}
	seen := map[string]bool{}
	for _, b := range append(permanent, running...) {
		if _, ok := b.prefix(); !ok || seen[b.Address] {
			continue
		}
		if !b.Until.IsZero() && !now.Before(b.Until) {
			continue
		}
		seen[b.Address] = true
		m.blocks = append(m.blocks, b)
		if len(m.blocks) >= maxBlocks {
			break
		}
	}
}

// saveRun keeps all blocks in RAM across a restart of titand.
func (m *Manager) saveRun() {
	if m.runFile == "" {
		return
	}
	data, err := json.Marshal(m.blocks)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(m.runFile), 0o700) // a runtime or state file only; without it the default applies
	tmp := m.runFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, m.runFile) // a runtime or state file only; without it the default applies
	}
}

// flushPermanent writes the permanent blocks to the stick, at most once a
// minute. The caller holds mu.
func (m *Manager) flushPermanent(now time.Time, force bool) {
	if !m.blocksDirty || (!force && now.Sub(m.blocksWritten) < blockedFlush) {
		return
	}
	permanent := []Block{}
	for _, b := range m.blocks {
		if b.Until.IsZero() {
			permanent = append(permanent, b)
		}
	}
	if err := m.store.WriteJSON(blockedFile, permanent); err != nil {
		slog.Warn("firewall: permanent blocks not saved", "err", err)
		return
	}
	m.blocksDirty, m.blocksWritten = false, now
}
