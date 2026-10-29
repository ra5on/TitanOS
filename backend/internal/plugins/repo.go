package plugins

// Official plugins come from their own repository (docs/PLUGIN-REPO.md):
// a fixed release "catalog" holds a signed pointer to the plugin sets, one
// set of kernel-independent plugins and one set per exact Debian kernel.
// A set is found through the exact running (or target) kernel, never as the
// newest release. Without a matching set TitanOS uses the plugins attached
// to its own release, as before.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

// errNoPluginSet means the plugin repository has nothing for this system;
// the plugins of the TitanOS release are used instead. A bad signature is
// never this error.
var errNoPluginSet = errors.New("plugins: no plugin set for this system")

const catalogSerialPath = "plugins/catalog-serial.json"

var setTagRE = regexp.MustCompile(`^(common|kernel-[0-9][0-9A-Za-z._~-]*)-[1-9][0-9]*$`)

// repoPointer is catalog.json of the release "catalog".
type repoPointer struct {
	Serial      uint64       `json:"serial"`
	Debian      string       `json:"debian"`
	SysextLevel string       `json:"sysextLevel"`
	Common      *repoSetRef  `json:"common"`
	Kernels     []repoSetRef `json:"kernels"`
}

type repoSetRef struct {
	Kernel        string `json:"kernel,omitempty"`
	KernelPackage string `json:"kernelPackage,omitempty"`
	Tag           string `json:"tag"`
	// Index is the SHA-256 of the set's index.json.
	Index string `json:"index"`
}

// repoIndex is index.json of one set.
type repoIndex struct {
	Tag           string `json:"tag"`
	Debian        string `json:"debian"`
	SysextLevel   string `json:"sysextLevel"`
	Kernel        string `json:"kernel,omitempty"`
	KernelPackage string `json:"kernelPackage,omitempty"`
	// MinBase is the oldest TitanOS system base the set is meant for.
	MinBase string          `json:"minBase"`
	Plugins []repoIndexItem `json:"plugins"`
}

type repoIndexItem struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Manifest is the SHA-256 of <name>.plugin.json; SHA256 and Size
	// are those of the image.
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// repoTarget is the system a set has to fit.
type repoTarget struct {
	base, kernel, kernelPackage string
}

// repoSet is a verified set: the base URL of its release and what it holds.
type repoSet struct {
	base  string
	index repoIndex
}

func (m *Manager) repoBase(tag string) string {
	return m.pluginSource + "/releases/download/" + tag + "/"
}

// fetchRepoSets returns the verified sets for target: common and, if there
// is one, the set for the exact kernel. fetch reads small metadata files.
func (m *Manager) fetchRepoSets(ctx context.Context, target repoTarget, fetch func(context.Context, string, int64) ([]byte, error)) (common, kernel *repoSet, err error) {
	if m.pluginSource == "" {
		return nil, nil, errNoPluginSet
	}
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("%w: no trusted plugin keys", ErrInvalid)
	}
	signed := func(url string, limit int64) ([]byte, error) {
		data, err := fetch(ctx, url, limit)
		if err != nil {
			return nil, err
		}
		sig, err := fetch(ctx, url+".minisig", 4096)
		if err != nil {
			return nil, err
		}
		if minisign.Verify(keys, data, string(sig)) != nil {
			return nil, fmt.Errorf("%w: signature of %s", ErrInvalid, url[strings.LastIndex(url, "/")+1:])
		}
		return data, nil
	}
	data, err := signed(m.repoBase("catalog")+"catalog.json", 64<<10)
	if errors.Is(err, ErrRelease) {
		return nil, nil, errNoPluginSet
	}
	if err != nil {
		return nil, nil, err
	}
	var pointer repoPointer
	if json.Unmarshal(data, &pointer) != nil || pointer.Serial == 0 || len(pointer.Kernels) > 64 {
		return nil, nil, fmt.Errorf("%w: plugin catalog", ErrInvalid)
	}
	if err := m.checkSerial(pointer.Serial); err != nil {
		return nil, nil, err
	}
	// Another Debian release or sysext level: nothing in the repository fits.
	if pointer.Debian != m.debian || pointer.SysextLevel != m.sysextLevel {
		return nil, nil, errNoPluginSet
	}
	load := func(ref repoSetRef, kind string) (*repoSet, error) {
		if !setTagRE.MatchString(ref.Tag) || !strings.HasPrefix(ref.Tag, kind) || !driverHashRE.MatchString(ref.Index) {
			return nil, fmt.Errorf("%w: plugin catalog entry", ErrInvalid)
		}
		base := m.repoBase(ref.Tag)
		data, err := signed(base+"index.json", 256<<10)
		if err != nil {
			return nil, err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != ref.Index {
			return nil, fmt.Errorf("%w: index of %s does not match the catalog", ErrInvalid, ref.Tag)
		}
		var index repoIndex
		if json.Unmarshal(data, &index) != nil || index.Tag != ref.Tag || index.Debian != pointer.Debian || index.SysextLevel != pointer.SysextLevel ||
			index.Kernel != ref.Kernel || index.KernelPackage != ref.KernelPackage || len(index.Plugins) > 32 || !releaseVersion.MatchString(index.MinBase) {
			return nil, fmt.Errorf("%w: index of %s", ErrInvalid, ref.Tag)
		}
		seen := map[string]bool{}
		for _, item := range index.Plugins {
			if !nameRE.MatchString(item.Name) || seen[item.Name] || item.Version == "" || len(item.Version) > 160 ||
				!driverHashRE.MatchString(item.Manifest) || !driverHashRE.MatchString(item.SHA256) || item.Size <= 0 || item.Size > maxImageBytes {
				return nil, fmt.Errorf("%w: index of %s", ErrInvalid, ref.Tag)
			}
			seen[item.Name] = true
		}
		// A set made for newer systems than the target is not used, and
		// it is no reason to take another one either.
		if newerVersion(index.MinBase, target.base) {
			return nil, errNoPluginSet
		}
		return &repoSet{base: base, index: index}, nil
	}
	if pointer.Common != nil {
		if pointer.Common.Kernel != "" || pointer.Common.KernelPackage != "" {
			return nil, nil, fmt.Errorf("%w: plugin catalog entry", ErrInvalid)
		}
		if common, err = load(*pointer.Common, "common"); err != nil && !errors.Is(err, errNoPluginSet) {
			return nil, nil, err
		}
	}
	for _, ref := range pointer.Kernels {
		if ref.Kernel == "" || ref.KernelPackage == "" {
			return nil, nil, fmt.Errorf("%w: plugin catalog entry", ErrInvalid)
		}
		if ref.Kernel != target.kernel || ref.KernelPackage != target.kernelPackage || target.kernelPackage == "" {
			continue
		}
		if kernel, err = load(ref, "kernel-"); err != nil && !errors.Is(err, errNoPluginSet) {
			return nil, nil, err
		}
		break
	}
	if common == nil && kernel == nil {
		return nil, nil, errNoPluginSet
	}
	return common, kernel, nil
}

// checkSerial refuses a plugin catalog older than one seen before, so that
// an old signed catalog cannot bring back plugins that were replaced. The
// highest serial is written to /config only when it grows.
func (m *Manager) checkSerial(serial uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.serialLoaded {
		var saved struct {
			Serial uint64 `json:"serial"`
		}
		if err := m.store.ReadJSON(catalogSerialPath, &saved); err == nil {
			m.catalogSerial = max(m.catalogSerial, saved.Serial)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		m.serialLoaded = true
	}
	if serial < m.catalogSerial {
		return fmt.Errorf("%w: the plugin catalog went backwards", ErrInvalid)
	}
	if serial > m.catalogSerial {
		if err := m.store.WriteJSON(catalogSerialPath, map[string]uint64{"serial": serial}); err != nil {
			return err
		}
		m.catalogSerial = serial
	}
	return nil
}

// verifySetManifest checks a plugin manifest of a set against its index.
func verifySetManifest(set *repoSet, item repoIndexItem, data []byte) (Manifest, error) {
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != item.Manifest {
		return Manifest{}, fmt.Errorf("%w: plugin %s does not match the index", ErrInvalid, item.Name)
	}
	manifest, err := parseManifest(data, item.Name+manifestSuffix)
	if err != nil {
		return manifest, err
	}
	if manifest.Version != item.Version || strings.ToLower(manifest.SHA256) != item.SHA256 || manifest.Size != item.Size || manifest.Kernel != set.index.Kernel || manifest.LocalBuild {
		return manifest, fmt.Errorf("%w: plugin %s does not match the index", ErrInvalid, item.Name)
	}
	return manifest, nil
}

var kernelPackageRE = regexp.MustCompile(`Debian ([0-9][0-9A-Za-z.+~-]*) \([0-9]{4}-[0-9]{2}-[0-9]{2}\)`)

// kernelPackageOf reads the Debian version of the running kernel package
// from /proc/version (the last "Debian <version> (<date>)").
func kernelPackageOf(procVersion string) string {
	matches := kernelPackageRE.FindAllStringSubmatch(procVersion, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// osReleaseValues reads VERSION_CODENAME and SYSEXT_LEVEL of the image.
func osReleaseValues(data string) (codename, sysextLevel string) {
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "VERSION_CODENAME":
			codename = value
		case "SYSEXT_LEVEL":
			sysextLevel = value
		}
	}
	return codename, sysextLevel
}
