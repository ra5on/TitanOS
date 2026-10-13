package update

import (
	"strconv"
	"strings"
)

// version is a release number: 1.4.0, or 1.4.0-beta.2 for a pre-release.
type version struct {
	numbers [3]int
	// pre is what follows the dash; empty for a final release.
	pre string
}

// parseVersion reads a version as releases carry it, with or without a
// leading "v". What a development build calls itself (a commit, "-dirty")
// is not a release number.
func parseVersion(text string) (version, bool) {
	var v version
	text = strings.TrimPrefix(strings.TrimSpace(text), "v")
	core, pre, hasPre := strings.Cut(text, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 || hasPre && pre == "" {
		return v, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || part != strconv.Itoa(number) {
			return v, false
		}
		v.numbers[i] = number
	}
	for _, r := range pre {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.') {
			return v, false
		}
	}
	v.pre = pre
	return v, true
}

// compare returns -1, 0 or 1 as a is older than, the same as or newer than b.
func (a version) compare(b version) int {
	for i := range a.numbers {
		if a.numbers[i] != b.numbers[i] {
			if a.numbers[i] < b.numbers[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "": // a final release is newer than its pre-releases
		return 1
	case b.pre == "":
		return -1
	}
	// beta.2 against beta.10: parts that are numbers compare as numbers.
	x, y := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		if errM == nil && errN == nil {
			if m < n {
				return -1
			}
			return 1
		}
		if x[i] < y[i] {
			return -1
		}
		return 1
	}
	if len(x) < len(y) {
		return -1
	}
	if len(x) > len(y) {
		return 1
	}
	return 0
}
