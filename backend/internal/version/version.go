// Package version holds the build version of titand.
package version

import "regexp"

// Version is set at build time via
// -ldflags "-X github.com/ra5on/TitanOS/backend/internal/version.Version=1.2.3".
var Version = "dev"

// Display is a TitanOS version as people read it (PLAN 75-20): releases
// since 0.72 count in two parts ("0.72"); tags, manifests and file names
// keep "0.72.0", because titand up to 0.71.1 only takes three parts as an
// update. Versions with a third number other than 0 stay as they are.
func Display(v string) string {
	if m := twoPart.FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return v
}

var twoPart = regexp.MustCompile(`^([0-9]+\.[0-9]+)\.0$`)
