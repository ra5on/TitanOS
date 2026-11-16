// Package version holds the build version of titand.
package version

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version is set at build time via
// -ldflags "-X github.com/ra5on/TitanOS/backend/internal/version.Version=1.2.3".
var Version = "dev"

// Display is a TitanOS version as people read it (PLAN 75-20): releases
// since 0.72 count in two parts ("0.72"); tags, manifests and file names
// keep "0.72.0", because titand up to 0.71.1 only takes three parts as an
// update. Versions with a third number other than 0 stay as they are.
//
// From 1.0 on the second part always has two digits (owner 16 Nov 2026:
// "Versionen sollen künftig so aussehen x.xx"): the tag v1.0.0 shows as
// "1.00", v1.1.0 as "1.01", v1.12.0 as "1.12". The 0.x releases keep how
// they were shown (0.72 … 0.99, 0.100).
func Display(v string) string {
	m := twoPart.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	major, _ := strconv.Atoi(m[1])
	if major == 0 {
		return m[1] + "." + m[2]
	}
	minor, _ := strconv.Atoi(m[2])
	return fmt.Sprintf("%d.%02d", major, minor)
}

var twoPart = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.0$`)
