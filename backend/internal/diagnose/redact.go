package diagnose

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Redactor takes out of a text what should not end up in a public bug
// report: addresses, names of accounts and of the NAS, and anything that
// looks like a password or key. The same address gets the same stand-in
// throughout one bundle, so that a log stays readable.
type Redactor struct {
	// names are replaced wherever they stand as a word: the name of the
	// NAS and the names of the accounts.
	names map[string]string
	seen  map[string]string
	count map[string]int
}

// NewRedactor returns a redactor that also hides the given names.
func NewRedactor(hostname string, accounts []string) *Redactor {
	r := &Redactor{names: map[string]string{}, seen: map[string]string{}, count: map[string]int{}}
	// The name every NAS has at first tells nothing, and it is part of
	// many paths and unit names.
	if len(hostname) >= 3 && hostname != "titanos" {
		r.names[hostname] = "<nas>"
	}
	for _, account := range accounts {
		// Very short names would hit ordinary words.
		if len(account) >= 3 {
			r.names[account] = r.standIn("user", account)
		}
	}
	return r
}

func (r *Redactor) standIn(kind, value string) string {
	key := kind + "\x00" + strings.ToLower(value)
	if known, ok := r.seen[key]; ok {
		return known
	}
	r.count[kind]++
	name := fmt.Sprintf("<%s-%d>", kind, r.count[kind])
	r.seen[key] = name
	return name
}

var (
	// key = value, key: value and "key": "value" where the key speaks of a secret.
	secretPair = regexp.MustCompile(`(?i)("?[a-z0-9_.-]*(?:passw(?:or)?d|passphrase|secret|token|api[_-]?key|apikey|private[_-]?key|credential|auth)[a-z0-9_.-]*"?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`)
	// user:password@ in addresses.
	urlLogin = regexp.MustCompile(`([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	bearer   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]{8,}`)
	pemBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+-----.*?-----END [A-Z ]+-----`)
	sshKey   = regexp.MustCompile(`\b(ssh-(?:rsa|ed25519|dss)|ecdsa-sha2-[a-z0-9-]+) [A-Za-z0-9+/=]{20,}`)
	email    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+`)
	unitName = regexp.MustCompile(`\.(service|socket|mount|automount|target|scope|slice|device|timer|path|swap)$`)
	mac      = regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:-][0-9A-Fa-f]{2}){5}\b`)
	ipv4     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// Candidates only; netip decides whether it is an address.
	ipv6 = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}`)
	// Long runs of letters and digits, as tokens and hashes are.
	longHex = regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`)
)

// Text returns s with everything private replaced.
// Secrets removes only secrets (keys, passwords, tokens, logins in
// addresses) and leaves names and addresses as they are.
func Secrets(s string) string {
	s = pemBlock.ReplaceAllString(s, "<key removed>")
	s = sshKey.ReplaceAllString(s, "$1 <key removed>")
	s = urlLogin.ReplaceAllString(s, "$1<removed>@")
	s = bearer.ReplaceAllString(s, "$1 <removed>")
	return secretPair.ReplaceAllStringFunc(s, func(found string) string {
		m := secretPair.FindStringSubmatch(found)
		// JSON flags such as "secretSet": true or an object are no secret.
		switch value := m[2]; {
		case strings.HasPrefix(value, "{"), strings.HasPrefix(value, "["), value == "true", value == "false", value == "null":
			return found
		}
		return m[1] + "<removed>"
	})
}

func (r *Redactor) Text(s string) string {
	s = Secrets(s)
	s = email.ReplaceAllStringFunc(s, func(found string) string {
		// Units of systemd look alike: getty@tty1.service.
		if unitName.MatchString(found) {
			return found
		}
		return r.standIn("email", found)
	})
	s = mac.ReplaceAllStringFunc(s, func(found string) string {
		if strings.Trim(found, "0:-") == "" || strings.EqualFold(strings.ReplaceAll(found, "-", ":"), "ff:ff:ff:ff:ff:ff") {
			return found
		}
		return r.standIn("mac", strings.ReplaceAll(found, "-", ":"))
	})
	s = ipv4.ReplaceAllStringFunc(s, func(found string) string {
		addr, err := netip.ParseAddr(found)
		// Version numbers look the same; an address has no part above 255.
		if err != nil || addr.IsLoopback() || addr.IsUnspecified() {
			return found
		}
		if bits := addr.As4(); bits[0] == 255 {
			return found // a netmask
		}
		return r.standIn("ip", found)
	})
	s = ipv6.ReplaceAllStringFunc(s, func(found string) string {
		addr, err := netip.ParseAddr(found)
		// Times (12:30:05) and the like are no addresses.
		if err != nil || !addr.Is6() || addr.IsLoopback() || addr.IsUnspecified() || !strings.Contains(found, "::") && strings.Count(found, ":") != 7 {
			return found
		}
		return r.standIn("ip6", addr.String())
	})
	s = longHex.ReplaceAllString(s, "<removed>")
	for name, replacement := range r.names {
		s = replaceWord(s, name, replacement)
	}
	return s
}

// replaceWord replaces name where it is not part of a longer word.
func replaceWord(s, name, replacement string) string {
	var b strings.Builder
	for {
		at := strings.Index(s, name)
		if at < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := at + len(name)
		if (at > 0 && wordByte(s[at-1])) || (end < len(s) && wordByte(s[end])) {
			b.WriteString(s[:end])
		} else {
			b.WriteString(s[:at])
			b.WriteString(replacement)
		}
		s = s[end:]
	}
}

func wordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
