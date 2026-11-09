// Package webapps runs programs that exist as plain browser pages (the
// Craft apps of storytold) directly in the browser: titand installs their
// static files from the signed manifest of its own release onto a data
// volume and serves them under /webapps/<id>/<version>/ (PLAN 66,
// docs/WEBAPPS.md).
package webapps

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// catalogJSON is a copy of webapps/catalog.json at the root of the
// repository (go:embed cannot reach outside the package); a test keeps
// both the same.
//
//go:embed catalog.json
var catalogJSON []byte

// App is one entry of the catalog.
type App struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Repo          string   `json:"repo"`
	Version       string   `json:"version"`
	UpstreamAsset string   `json:"upstreamAsset"`
	SHA256        string   `json:"sha256"`
	Size          int64    `json:"size"`
	License       string   `json:"license"`
	Bridge        string   `json:"bridge"`
	Types         []string `json:"types"`
	Default       bool     `json:"default"`
	// Trusted: our own build (TitanOS-Office: T-Word, T-PDF, T-Exel, PLAN
	// 75-18/75-20). It runs without the sandbox, on the web UI's origin
	// with the account's login: through Cloudflare Access it loads its
	// files with the Cloudflare cookie like every page of TitanOS.
	Trusted bool `json:"trusted,omitempty"`
}

// Asset is the name of the app's archive in a TitanOS release.
func (a App) Asset() string { return AssetName(a.ID, a.Version) }

// AssetName is titanos-webapp-<id>-<version>.zip.
func AssetName(id, version string) string { return "titanos-webapp-" + id + "-" + version + ".zip" }

// Limits of an app archive (docs/WEBAPPS.md). The biggest Craft app
// (LightCraft 0.19.0) unpacks to 96 MiB with its precompressed copies.
const (
	MaxArchive  = 256 << 20
	MaxUnpacked = 256 << 20
	MaxFiles    = 2000
)

var (
	// An id is lower case; one starting with "_" is reserved (/webapps/_host/).
	idRE      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	shaRE     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repoRE    = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	typeRE    = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
)

// ValidID reports whether id can name an app (and a folder).
func ValidID(id string) bool { return idRE.MatchString(id) }

// ValidVersion reports whether version is a plain release number.
func ValidVersion(version string) bool { return versionRE.MatchString(version) }

// ParseCatalog reads and checks a catalog.
func ParseCatalog(data []byte) ([]App, error) {
	var c struct {
		Apps []App `json:"apps"`
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("webapps: catalog: %w", err)
	}
	if len(c.Apps) == 0 {
		return nil, errors.New("webapps: empty catalog")
	}
	seen := map[string]bool{}
	types := map[string]string{}
	for _, a := range c.Apps {
		switch {
		case !ValidID(a.ID) || seen[a.ID]:
			return nil, fmt.Errorf("webapps: catalog: invalid or repeated id %q", a.ID)
		case a.Name == "" || len(a.Name) > 40:
			return nil, fmt.Errorf("webapps: catalog: %s: invalid name", a.ID)
		case !repoRE.MatchString(a.Repo):
			return nil, fmt.Errorf("webapps: catalog: %s: invalid repo", a.ID)
		case !ValidVersion(a.Version):
			return nil, fmt.Errorf("webapps: catalog: %s: invalid version", a.ID)
		case a.UpstreamAsset != a.ID+"-web-"+a.Version+".zip":
			return nil, fmt.Errorf("webapps: catalog: %s: upstream asset must be %s-web-%s.zip", a.ID, a.ID, a.Version)
		case !shaRE.MatchString(a.SHA256):
			return nil, fmt.Errorf("webapps: catalog: %s: invalid sha256", a.ID)
		case a.Size <= 0 || a.Size > MaxArchive:
			return nil, fmt.Errorf("webapps: catalog: %s: invalid size", a.ID)
		case a.License == "":
			return nil, fmt.Errorf("webapps: catalog: %s: no license", a.ID)
		case a.Bridge != "host" && a.Bridge != "adapter":
			return nil, fmt.Errorf("webapps: catalog: %s: bridge must be host or adapter", a.ID)
		}
		for _, t := range a.Types {
			if !typeRE.MatchString(t) {
				return nil, fmt.Errorf("webapps: catalog: %s: invalid type %q", a.ID, t)
			}
			// A type has one default app; two default apps may not share one.
			if a.Default {
				if other, ok := types[t]; ok {
					return nil, fmt.Errorf("webapps: catalog: type %s in default apps %s and %s", t, other, a.ID)
				}
				types[t] = a.ID
			}
		}
		if a.Types == nil {
			return nil, fmt.Errorf("webapps: catalog: %s: types missing (may be empty)", a.ID)
		}
		seen[a.ID] = true
	}
	return c.Apps, nil
}

var catalog = func() []App {
	apps, err := ParseCatalog(catalogJSON)
	if err != nil {
		panic(err)
	}
	return apps
}()

// Catalog returns the apps this titand knows.
func Catalog() []App { return slices.Clone(catalog) }

// Lookup returns the catalog entry of id.
func Lookup(id string) (App, bool) {
	for _, a := range catalog {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// Defaults are the ids of the basic set (Grundausstattung).
func Defaults() []string {
	var ids []string
	for _, a := range catalog {
		if a.Default {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// ReleaseEntry is one entry of "webapps" in a release manifest.
type ReleaseEntry struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// ErrNotInRelease is an app the release of this titand does not bring.
var ErrNotInRelease = errors.New("webapps: the release has no archive of this app")

// ParseRelease reads the "webapps" entries of a manifest whose signature
// was checked (plugins.Manager.ReleaseManifest) for the release of version.
func ParseRelease(manifest []byte, version string) ([]ReleaseEntry, error) {
	var release struct {
		Version string         `json:"version"`
		Tag     string         `json:"tag"`
		Webapps []ReleaseEntry `json:"webapps"`
	}
	if err := json.Unmarshal(manifest, &release); err != nil {
		return nil, fmt.Errorf("webapps: manifest: %w", err)
	}
	if release.Version != version || release.Tag != "v"+version {
		return nil, errors.New("webapps: manifest of another release")
	}
	seen := map[string]bool{}
	for _, e := range release.Webapps {
		if !ValidID(e.ID) || !ValidVersion(e.Version) || seen[e.ID] || e.Asset != AssetName(e.ID, e.Version) ||
			!shaRE.MatchString(e.SHA256) || e.Size <= 0 || e.Size > MaxArchive {
			return nil, fmt.Errorf("webapps: invalid entry %q in the manifest", e.ID)
		}
		seen[e.ID] = true
	}
	return release.Webapps, nil
}
